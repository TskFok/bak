package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"BaiduYunPanBak/global"
	"BaiduYunPanBak/internal/backup"
	"BaiduYunPanBak/internal/cloudacct"
	"BaiduYunPanBak/internal/provider"
	"BaiduYunPanBak/internal/provider/aliyun"
	"BaiduYunPanBak/internal/provider/baidu"
	"BaiduYunPanBak/internal/provider/webdav"
	"BaiduYunPanBak/internal/store"
	"BaiduYunPanBak/model"
	"BaiduYunPanBak/tool/openapi"
	"BaiduYunPanBak/utils/cache"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"
)

const (
	aliyunBase       = "https://openapi.alipan.com"
	refreshThreshold = 5 * time.Minute
	maxBodyBytes     = 1 << 20
)

// providers 是固定的网盘列表，顺序即概览和列表的输出顺序。
var providers = []string{"baidu", "aliyun", "webdav"}

var displayNames = map[string]string{
	"baidu":  "百度网盘",
	"aliyun": "阿里云盘",
	"webdav": "WebDAV",
}

var scheduleTimeRe = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

// APIStore 是 HTTP 层需要的存储能力，store.GormStore 实现了它。
type APIStore interface {
	backup.Store
	UpsertCloud(ctx context.Context, acct *store.CloudAccount) error
	ListClouds(ctx context.Context) ([]store.CloudAccount, error)
	GetCloud(ctx context.Context, id uint) (store.CloudAccount, error)
	CreateTask(ctx context.Context, task *backup.Task) error
	UpdateTask(ctx context.Context, task *backup.Task) error
	ListTasks(ctx context.Context) ([]backup.Task, error)
	DeleteTask(ctx context.Context, id uint) error
	GetRun(ctx context.Context, id uint) (backup.Run, error)
	ListRuns(ctx context.Context, limit int) ([]backup.Run, error)
}

var _ APIStore = (*store.GormStore)(nil)

// ProviderFactory 按网盘类型和已解密的凭据构造 provider。
type ProviderFactory func(name string, s cloudacct.Secrets) (provider.Provider, error)

// TokenService 负责 OAuth 授权码换令牌和令牌刷新。
type TokenService interface {
	Exchange(ctx context.Context, provider string, s cloudacct.Secrets, code string) (cloudacct.Secrets, error)
	Refresh(ctx context.Context, provider string, s cloudacct.Secrets) (cloudacct.Secrets, error)
}

// Deps 是 NewMux 需要的全部依赖。Providers、Tokens、Now 为空时使用真实实现。
type Deps struct {
	AuthDeps
	Store     APIStore
	JWTSecret string
	ChunkDir  string
	Providers ProviderFactory
	Tokens    TokenService
	Now       func() time.Time
}

// NewMux 返回完整的 HTTP 处理器：公开的 setup/login 路由和需要会话的业务路由。
func NewMux(deps Deps) http.Handler {
	if deps.Providers == nil {
		deps.Providers = defaultProviders(deps.ChunkDir)
	}
	if deps.Tokens == nil {
		deps.Tokens = liveTokens{}
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	a := &api{deps: deps, running: map[uint]bool{}}

	r := chi.NewRouter()
	registerAuthRoutes(r, deps.AuthDeps)
	r.Group(func(pr chi.Router) {
		pr.Use(requireSession(deps.AuthDeps))
		pr.Get("/api/overview", a.overview)
		pr.Get("/api/clouds", a.listClouds)
		pr.Put("/api/clouds/{provider}", a.putCloud)
		pr.Post("/api/clouds/{provider}/test", a.testCloud)
		pr.Post("/api/clouds/{provider}/authorize", a.authorizeCloud)
		pr.Get("/api/tasks", a.listTasks)
		pr.Post("/api/tasks", a.createTask)
		pr.Put("/api/tasks/{id}", a.updateTask)
		pr.Delete("/api/tasks/{id}", a.deleteTask)
		pr.Post("/api/tasks/{id}/run", a.runTask)
		pr.Get("/api/runs", a.listRuns)
		pr.Get("/api/runs/{id}", a.getRun)
	})
	return r
}

type api struct {
	deps Deps

	mu      sync.Mutex
	running map[uint]bool // 本进程内正在手动运行的任务，用来挡住并发请求
}

func (a *api) tryLock(id uint) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.running[id] {
		return false
	}
	a.running[id] = true
	return true
}

func (a *api) unlock(id uint) {
	a.mu.Lock()
	delete(a.running, id)
	a.mu.Unlock()
}

// ---- 通用辅助 ----

func serverError(w http.ResponseWriter) {
	http.Error(w, "服务器错误", http.StatusInternalServerError)
}

func badRequest(w http.ResponseWriter) { http.Error(w, "请求无效", http.StatusBadRequest) }

func fieldErrors(w http.ResponseWriter, fields map[string]string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{"fields": fields})
}

func decodeBody(w http.ResponseWriter, req *http.Request, v any) bool {
	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	if err := json.NewDecoder(req.Body).Decode(v); err != nil {
		badRequest(w)
		return false
	}
	return true
}

func pathID(req *http.Request) (uint, bool) {
	n, err := strconv.ParseUint(chi.URLParam(req, "id"), 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint(n), true
}

func validProvider(name string) bool {
	for _, p := range providers {
		if p == name {
			return true
		}
	}
	return false
}

// shortError 取出可展示的错误文本，限制长度。
func shortError(err error) string {
	msg := err.Error()
	if utf8.RuneCountInString(msg) > 200 {
		msg = string([]rune(msg)[:200])
	}
	return msg
}

// ---- 网盘 ----

func (a *api) findCloud(ctx context.Context, name string) (store.CloudAccount, bool, error) {
	clouds, err := a.deps.Store.ListClouds(ctx)
	if err != nil {
		return store.CloudAccount{}, false, err
	}
	for _, c := range clouds {
		if c.Provider == name {
			return c, true, nil
		}
	}
	return store.CloudAccount{}, false, nil
}

func (a *api) saveCloud(ctx context.Context, acct store.CloudAccount) error {
	acct.ID = 0 // 按 provider 唯一键 upsert，避免主键冲突
	return a.deps.Store.UpsertCloud(ctx, &acct)
}

func (a *api) openSecrets(acct store.CloudAccount) (cloudacct.Secrets, error) {
	if acct.SecretBlob == "" {
		return cloudacct.Secrets{}, nil
	}
	return cloudacct.Open(a.deps.JWTSecret, acct.SecretBlob)
}

// cloudView 返回对外展示的网盘信息，只含非敏感配置。
func (a *api) cloudView(name string, acct store.CloudAccount, found bool) map[string]any {
	status, lastErr := "disconnected", ""
	if found {
		status, lastErr = acct.Status, acct.LastError
		if status == "" {
			status = "disconnected"
		}
	}
	pv := cloudacct.PublicView(name, status, lastErr)
	view := map[string]any{
		"provider":    pv["provider"],
		"status":      pv["status"],
		"lastError":   pv["lastError"],
		"displayName": displayNames[name],
		"id":          acct.ID,
	}
	cfg := map[string]string{}
	if found {
		if s, err := a.openSecrets(acct); err == nil {
			switch name {
			case "webdav":
				cfg["url"], cfg["username"] = s.URL, s.Username
			default:
				cfg["clientId"], cfg["deviceId"] = s.ClientID, s.DeviceID
			}
		}
	}
	view["config"] = cfg
	return view
}

func (a *api) overview(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	clouds, err := a.deps.Store.ListClouds(ctx)
	if err != nil {
		serverError(w)
		return
	}
	byName := map[string]store.CloudAccount{}
	for _, c := range clouds {
		byName[c.Provider] = c
	}
	list := make([]map[string]string, 0, len(providers))
	for _, p := range providers {
		c, ok := byName[p]
		status, lastErr := "disconnected", ""
		if ok && c.Status != "" {
			status, lastErr = c.Status, c.LastError
		}
		list = append(list, cloudacct.PublicView(p, status, lastErr))
	}
	tasks, err := a.deps.Store.ListTasks(ctx)
	if err != nil {
		serverError(w)
		return
	}
	runs, err := a.deps.Store.ListRuns(ctx, 5)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"clouds":     list,
		"taskCount":  len(tasks),
		"recentRuns": runViews(runs),
	})
}

func (a *api) listClouds(w http.ResponseWriter, req *http.Request) {
	clouds, err := a.deps.Store.ListClouds(req.Context())
	if err != nil {
		serverError(w)
		return
	}
	byName := map[string]store.CloudAccount{}
	for _, c := range clouds {
		byName[c.Provider] = c
	}
	out := make([]map[string]any, 0, len(providers))
	for _, p := range providers {
		c, ok := byName[p]
		out = append(out, a.cloudView(p, c, ok))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *api) putCloud(w http.ResponseWriter, req *http.Request) {
	name := chi.URLParam(req, "provider")
	if !validProvider(name) {
		http.Error(w, "不支持的网盘", http.StatusNotFound)
		return
	}
	var body struct {
		URL          string `json:"url"`
		Username     string `json:"username"`
		Password     string `json:"password"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
		DeviceID     string `json:"deviceId"`
	}
	if !decodeBody(w, req, &body) {
		return
	}
	ctx := req.Context()
	old, found, err := a.findCloud(ctx, name)
	if err != nil {
		serverError(w)
		return
	}
	var prev cloudacct.Secrets
	if found {
		if prev, err = a.openSecrets(old); err != nil {
			prev = cloudacct.Secrets{} // 旧密文无法解密时视为未配置，要求重新填写
		}
	}

	fields := map[string]string{}
	var next cloudacct.Secrets
	status := "disconnected"
	if name == "webdav" {
		u := strings.TrimSpace(body.URL)
		if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
			fields["url"] = "请填写以 http:// 或 https:// 开头的地址"
		}
		if strings.TrimSpace(body.Username) == "" {
			fields["username"] = "请填写用户名"
		}
		pw := body.Password
		if pw == "" {
			pw = prev.Password // 留空表示沿用已保存的密码
		}
		next = cloudacct.Secrets{URL: u, Username: strings.TrimSpace(body.Username), Password: pw}
		status = "configured"
	} else {
		id := strings.TrimSpace(body.ClientID)
		secret := body.ClientSecret
		if id == "" {
			fields["clientId"] = "请填写应用 Key"
		}
		if secret == "" {
			secret = prev.ClientSecret
		}
		if secret == "" {
			fields["clientSecret"] = "请填写应用密钥"
		}
		next = cloudacct.Secrets{ClientID: id, ClientSecret: secret, DeviceID: strings.TrimSpace(body.DeviceID)}
		if next.DeviceID == "" {
			next.DeviceID = prev.DeviceID
		}
		if id == prev.ClientID && secret == prev.ClientSecret && prev.AccessToken != "" {
			// 应用凭据没变，保留已授权的令牌。
			next.AccessToken, next.RefreshToken, next.ExpiresAt = prev.AccessToken, prev.RefreshToken, prev.ExpiresAt
			if found {
				status = old.Status
			}
		}
	}
	if len(fields) > 0 {
		fieldErrors(w, fields)
		return
	}
	blob, err := cloudacct.Seal(a.deps.JWTSecret, next)
	if err != nil {
		serverError(w)
		return
	}
	acct := store.CloudAccount{Provider: name, DisplayName: displayNames[name], SecretBlob: blob, Status: status}
	if err := a.saveCloud(ctx, acct); err != nil {
		serverError(w)
		return
	}
	saved, ok, err := a.findCloud(ctx, name)
	if err != nil || !ok {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, a.cloudView(name, saved, true))
}

// loadConfiguredCloud 取出已配置的网盘及其凭据，不存在时写 404。
func (a *api) loadConfiguredCloud(w http.ResponseWriter, req *http.Request) (string, store.CloudAccount, cloudacct.Secrets, bool) {
	name := chi.URLParam(req, "provider")
	if !validProvider(name) {
		http.Error(w, "不支持的网盘", http.StatusNotFound)
		return "", store.CloudAccount{}, cloudacct.Secrets{}, false
	}
	acct, found, err := a.findCloud(req.Context(), name)
	if err != nil {
		serverError(w)
		return "", store.CloudAccount{}, cloudacct.Secrets{}, false
	}
	if !found {
		http.Error(w, "请先保存网盘配置", http.StatusNotFound)
		return "", store.CloudAccount{}, cloudacct.Secrets{}, false
	}
	secrets, err := a.openSecrets(acct)
	if err != nil {
		http.Error(w, "网盘配置无法读取，请重新保存", http.StatusConflict)
		return "", store.CloudAccount{}, cloudacct.Secrets{}, false
	}
	return name, acct, secrets, true
}

func (a *api) testCloud(w http.ResponseWriter, req *http.Request) {
	name, acct, secrets, ok := a.loadConfiguredCloud(w, req)
	if !ok {
		return
	}
	ctx := req.Context()
	var testErr error
	secrets, testErr = a.freshSecrets(ctx, name, secrets, &acct)
	if testErr == nil {
		var p provider.Provider
		if p, testErr = a.deps.Providers(name, secrets); testErr == nil {
			testErr = p.Test(ctx)
		}
	}
	if testErr != nil {
		acct.Status, acct.LastError = "error", shortError(testErr)
	} else {
		acct.Status, acct.LastError = "connected", ""
	}
	if err := a.saveCloud(ctx, acct); err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, a.cloudView(name, acct, true))
}

func (a *api) authorizeCloud(w http.ResponseWriter, req *http.Request) {
	name, acct, secrets, ok := a.loadConfiguredCloud(w, req)
	if !ok {
		return
	}
	if name == "webdav" {
		http.Error(w, "WebDAV 不需要授权", http.StatusBadRequest)
		return
	}
	var body struct {
		Code string `json:"code"`
	}
	if !decodeBody(w, req, &body) {
		return
	}
	code := strings.TrimSpace(body.Code)
	if code == "" {
		u := cloudacct.AliyunAuthorizeURL(secrets.ClientID)
		if name == "baidu" {
			u = cloudacct.BaiduAuthorizeURL(secrets.ClientID, secrets.DeviceID)
		}
		writeJSON(w, http.StatusOK, map[string]string{"authorizeUrl": u})
		return
	}
	ctx := req.Context()
	next, err := a.deps.Tokens.Exchange(ctx, name, secrets, code)
	if err != nil {
		http.Error(w, "授权失败，请检查授权码", http.StatusBadRequest)
		return
	}
	blob, err := cloudacct.Seal(a.deps.JWTSecret, next)
	if err != nil {
		serverError(w)
		return
	}
	acct.SecretBlob, acct.Status, acct.LastError = blob, "connected", ""
	if err := a.saveCloud(ctx, acct); err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, a.cloudView(name, acct, true))
}

// freshSecrets 在令牌距到期不足 5 分钟时刷新并保存新密钥，acct 同步更新。
func (a *api) freshSecrets(ctx context.Context, name string, s cloudacct.Secrets, acct *store.CloudAccount) (cloudacct.Secrets, error) {
	if name == "webdav" || s.RefreshToken == "" || s.ExpiresAt.IsZero() {
		return s, nil
	}
	if s.ExpiresAt.Sub(a.deps.Now()) >= refreshThreshold {
		return s, nil
	}
	next, err := a.deps.Tokens.Refresh(ctx, name, s)
	if err != nil {
		return s, errTokenRefresh
	}
	blob, err := cloudacct.Seal(a.deps.JWTSecret, next)
	if err != nil {
		return s, errTokenRefresh
	}
	acct.SecretBlob, acct.Status, acct.LastError = blob, "connected", ""
	if err := a.saveCloud(ctx, *acct); err != nil {
		return s, errTokenRefresh
	}
	return next, nil
}

var errTokenRefresh = errors.New("令牌刷新失败，请重新授权")

// ---- 任务 ----

type taskBody struct {
	Name           string `json:"name"`
	LocalDir       string `json:"localDir"`
	Provider       string `json:"provider"`
	CloudAccountID uint   `json:"cloudAccountId"`
	RemoteDir      string `json:"remoteDir"`
	ScheduleTime   string `json:"scheduleTime"`
	Enabled        *bool  `json:"enabled"`
}

func localDirReadable(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	f, err := os.Open(dir)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// parseTask 校验请求并生成任务，校验失败时已写出 400。
func (a *api) parseTask(w http.ResponseWriter, req *http.Request) (backup.Task, bool) {
	var body taskBody
	if !decodeBody(w, req, &body) {
		return backup.Task{}, false
	}
	fields := map[string]string{}
	localDir := strings.TrimSpace(body.LocalDir)
	if localDir == "" || !localDirReadable(localDir) {
		fields["localDir"] = "本地目录不存在或不可读"
	}
	remoteDir := strings.TrimSpace(body.RemoteDir)
	if !strings.HasPrefix(remoteDir, "/") {
		fields["remoteDir"] = "网盘目录必须以 / 开头"
	}
	schedule := strings.TrimSpace(body.ScheduleTime)
	if schedule == "" {
		schedule = "02:00"
	}
	if !scheduleTimeRe.MatchString(schedule) {
		fields["scheduleTime"] = "请填写 HH:MM 格式的时间"
	}
	name := strings.TrimSpace(body.Name)
	if name == "" && localDir != "" {
		name = filepath.Base(localDir)
	}
	if name == "" || utf8.RuneCountInString(name) > 100 {
		fields["name"] = "请填写 1 到 100 个字符的任务名称"
	}
	if len(fields) > 0 {
		fieldErrors(w, fields)
		return backup.Task{}, false
	}

	clouds, err := a.deps.Store.ListClouds(req.Context())
	if err != nil {
		serverError(w)
		return backup.Task{}, false
	}
	var accountID uint
	for _, c := range clouds {
		if (body.Provider != "" && c.Provider == body.Provider) || (body.Provider == "" && body.CloudAccountID != 0 && c.ID == body.CloudAccountID) {
			accountID = c.ID
		}
	}
	if accountID == 0 {
		fieldErrors(w, map[string]string{"provider": "请先配置并选择网盘"})
		return backup.Task{}, false
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	return backup.Task{
		Name: name, LocalDir: localDir, CloudAccountID: accountID,
		RemoteDir: path.Clean(remoteDir), ScheduleTime: schedule, Enabled: enabled,
	}, true
}

func (a *api) taskViews(ctx context.Context, tasks []backup.Task) ([]map[string]any, error) {
	clouds, err := a.deps.Store.ListClouds(ctx)
	if err != nil {
		return nil, err
	}
	names := map[uint]string{}
	for _, c := range clouds {
		names[c.ID] = c.Provider
	}
	out := make([]map[string]any, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, taskView(t, names[t.CloudAccountID]))
	}
	return out, nil
}

func taskView(t backup.Task, providerName string) map[string]any {
	return map[string]any{
		"id":             t.ID,
		"name":           t.Name,
		"localDir":       t.LocalDir,
		"provider":       providerName,
		"cloudAccountId": t.CloudAccountID,
		"remoteDir":      t.RemoteDir,
		"scheduleTime":   t.ScheduleTime,
		"enabled":        t.Enabled,
	}
}

func (a *api) findTask(ctx context.Context, id uint) (backup.Task, bool, error) {
	tasks, err := a.deps.Store.ListTasks(ctx)
	if err != nil {
		return backup.Task{}, false, err
	}
	for _, t := range tasks {
		if t.ID == id {
			return t, true, nil
		}
	}
	return backup.Task{}, false, nil
}

func (a *api) listTasks(w http.ResponseWriter, req *http.Request) {
	tasks, err := a.deps.Store.ListTasks(req.Context())
	if err != nil {
		serverError(w)
		return
	}
	views, err := a.taskViews(req.Context(), tasks)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (a *api) createTask(w http.ResponseWriter, req *http.Request) {
	task, ok := a.parseTask(w, req)
	if !ok {
		return
	}
	if err := a.deps.Store.CreateTask(req.Context(), &task); err != nil {
		serverError(w)
		return
	}
	views, err := a.taskViews(req.Context(), []backup.Task{task})
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, views[0])
}

func (a *api) updateTask(w http.ResponseWriter, req *http.Request) {
	id, ok := pathID(req)
	if !ok {
		http.NotFound(w, req)
		return
	}
	old, found, err := a.findTask(req.Context(), id)
	if err != nil {
		serverError(w)
		return
	}
	if !found {
		http.Error(w, "任务不存在", http.StatusNotFound)
		return
	}
	task, ok := a.parseTask(w, req)
	if !ok {
		return
	}
	task.ID, task.LastScheduledOn = old.ID, old.LastScheduledOn
	if err := a.deps.Store.UpdateTask(req.Context(), &task); err != nil {
		serverError(w)
		return
	}
	views, err := a.taskViews(req.Context(), []backup.Task{task})
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, views[0])
}

func (a *api) deleteTask(w http.ResponseWriter, req *http.Request) {
	id, ok := pathID(req)
	if !ok {
		http.NotFound(w, req)
		return
	}
	_, found, err := a.findTask(req.Context(), id)
	if err != nil {
		serverError(w)
		return
	}
	if !found {
		http.Error(w, "任务不存在", http.StatusNotFound)
		return
	}
	if err := a.deps.Store.DeleteTask(req.Context(), id); err != nil {
		serverError(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *api) runTask(w http.ResponseWriter, req *http.Request) {
	id, ok := pathID(req)
	if !ok {
		http.NotFound(w, req)
		return
	}
	task, found, err := a.findTask(req.Context(), id)
	if err != nil {
		serverError(w)
		return
	}
	if !found {
		http.Error(w, "任务不存在", http.StatusNotFound)
		return
	}
	if !a.tryLock(id) {
		http.Error(w, backup.ErrBusy.Error(), http.StatusConflict)
		return
	}
	defer a.unlock(id)

	// 备份可能持续很久，不跟随请求取消。
	ctx := context.Background()
	acct, err := a.deps.Store.GetCloud(ctx, task.CloudAccountID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			http.Error(w, "任务关联的网盘不存在", http.StatusBadRequest)
			return
		}
		serverError(w)
		return
	}
	secrets, err := a.openSecrets(acct)
	if err != nil {
		a.failRun(ctx, w, task, "网盘配置无法读取，请重新保存")
		return
	}
	secrets, err = a.freshSecrets(ctx, acct.Provider, secrets, &acct)
	if err != nil {
		a.failRun(ctx, w, task, err.Error())
		return
	}
	p, err := a.deps.Providers(acct.Provider, secrets)
	if err != nil {
		a.failRun(ctx, w, task, "无法连接网盘")
		return
	}
	run, err := backup.RunTask(ctx, a.deps.Store, task, p, "manual", a.deps.Now())
	if errors.Is(err, backup.ErrBusy) {
		http.Error(w, backup.ErrBusy.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, runView(run))
}

// failRun 把无法开始的手动运行记为 failed，响应仍是 200。
func (a *api) failRun(ctx context.Context, w http.ResponseWriter, task backup.Task, summary string) {
	busy, err := a.deps.Store.HasRunning(ctx, task.ID)
	if err != nil {
		serverError(w)
		return
	}
	if busy {
		http.Error(w, backup.ErrBusy.Error(), http.StatusConflict)
		return
	}
	now := a.deps.Now()
	run := backup.Run{TaskID: task.ID, Trigger: "manual", Status: "running", StartedAt: now}
	if err := a.deps.Store.CreateRun(ctx, &run); err != nil {
		serverError(w)
		return
	}
	done := a.deps.Now()
	run.Status, run.ErrorSummary, run.FinishedAt = "failed", summary, &done
	if err := a.deps.Store.FinishRun(ctx, run); err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, runView(run))
}

// ---- 运行记录 ----

func runView(r backup.Run) map[string]any {
	return map[string]any{
		"id":           r.ID,
		"taskId":       r.TaskID,
		"trigger":      r.Trigger,
		"status":       r.Status,
		"errorSummary": r.ErrorSummary,
		"startedAt":    r.StartedAt,
		"finishedAt":   r.FinishedAt,
		"successCount": r.SuccessCount,
		"skipCount":    r.SkipCount,
		"failCount":    r.FailCount,
	}
}

func runViews(runs []backup.Run) []map[string]any {
	out := make([]map[string]any, 0, len(runs))
	for _, r := range runs {
		out = append(out, runView(r))
	}
	return out
}

func (a *api) listRuns(w http.ResponseWriter, req *http.Request) {
	limit := 50
	if v := req.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 200 {
			badRequest(w)
			return
		}
		limit = n
	}
	runs, err := a.deps.Store.ListRuns(req.Context(), limit)
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, runViews(runs))
}

func (a *api) getRun(w http.ResponseWriter, req *http.Request) {
	id, ok := pathID(req)
	if !ok {
		http.NotFound(w, req)
		return
	}
	run, err := a.deps.Store.GetRun(req.Context(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		http.Error(w, "运行记录不存在", http.StatusNotFound)
		return
	}
	if err != nil {
		serverError(w)
		return
	}
	writeJSON(w, http.StatusOK, runView(run))
}

// ---- 默认实现 ----

func defaultProviders(chunkDir string) ProviderFactory {
	if chunkDir == "" {
		chunkDir = filepath.Join(os.TempDir(), "baktobaidu-chunks")
	}
	return func(name string, s cloudacct.Secrets) (provider.Provider, error) {
		switch name {
		case "webdav":
			return webdav.New(s.URL, s.Username, s.Password), nil
		case "baidu":
			return baidu.New(s.AccessToken, baidu.NewOpenAPIUploader(), chunkDir, 0), nil
		case "aliyun":
			return aliyun.New(aliyunBase, s.AccessToken, nil), nil
		}
		return nil, errors.New("不支持的网盘")
	}
}

type liveTokens struct{}

func (liveTokens) Exchange(ctx context.Context, name string, s cloudacct.Secrets, code string) (cloudacct.Secrets, error) {
	switch name {
	case "aliyun":
		tok, err := aliyun.ExchangeCode(ctx, aliyunBase, s.ClientID, s.ClientSecret, code)
		if err != nil {
			return s, err
		}
		s.AccessToken, s.RefreshToken, s.ExpiresAt = tok.AccessToken, tok.RefreshToken, tok.ExpiresAt
		return s, nil
	case "baidu":
		client := openapi.NewAPIClient(openapi.NewConfiguration())
		resp, _, err := client.AuthApi.OauthTokenCode2token(ctx).Code(code).ClientId(s.ClientID).ClientSecret(s.ClientSecret).RedirectUri("oob").Execute()
		if err != nil || resp.GetAccessToken() == "" {
			return s, errors.New("授权失败")
		}
		s.AccessToken, s.RefreshToken = resp.GetAccessToken(), resp.GetRefreshToken()
		s.ExpiresAt = time.Now().Add(time.Duration(resp.GetExpiresIn()) * time.Second)
		syncLegacyBaiduToken(s)
		return s, nil
	}
	return s, errors.New("不支持的网盘")
}

func (liveTokens) Refresh(ctx context.Context, name string, s cloudacct.Secrets) (cloudacct.Secrets, error) {
	switch name {
	case "aliyun":
		tok, err := aliyun.Refresh(ctx, aliyunBase, s.ClientID, s.ClientSecret, s.RefreshToken)
		if err != nil {
			return s, err
		}
		s.AccessToken, s.ExpiresAt = tok.AccessToken, tok.ExpiresAt
		if tok.RefreshToken != "" {
			s.RefreshToken = tok.RefreshToken
		}
		return s, nil
	case "baidu":
		client := openapi.NewAPIClient(openapi.NewConfiguration())
		resp, _, err := client.AuthApi.OauthTokenRefreshToken(ctx).RefreshToken(s.RefreshToken).ClientId(s.ClientID).ClientSecret(s.ClientSecret).Execute()
		if err != nil || resp.GetAccessToken() == "" {
			return s, errors.New("刷新失败")
		}
		s.AccessToken = resp.GetAccessToken()
		if rt := resp.GetRefreshToken(); rt != "" {
			s.RefreshToken = rt
		}
		s.ExpiresAt = time.Now().Add(time.Duration(resp.GetExpiresIn()) * time.Second)
		syncLegacyBaiduToken(s)
		return s, nil
	}
	return s, errors.New("不支持的网盘")
}

// syncLegacyBaiduToken 让旧的 upload 命令继续可用：写 tokens 表和 Redis access_token。
// 数据库或 Redis 未初始化时直接跳过。
func syncLegacyBaiduToken(s cloudacct.Secrets) {
	expiresIn := int32(time.Until(s.ExpiresAt) / time.Second)
	if global.DataBase != nil {
		cur := model.Token{}.Find()
		if cur.Id != 0 {
			model.Token{}.Update(
				map[string]interface{}{"access_token": s.AccessToken, "refresh_token": s.RefreshToken, "expires_in": expiresIn},
				map[string]interface{}{"id": cur.Id},
			)
		} else {
			tk := &model.Token{AccessToken: s.AccessToken, RefreshToken: s.RefreshToken, ExpiresIn: expiresIn}
			tk.Create(tk)
		}
	}
	if global.RedisClient != nil {
		cache.Set("access_token", s.AccessToken, int(expiresIn))
	}
}
