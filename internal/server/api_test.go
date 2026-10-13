package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"bak/internal/backup"
	"bak/internal/cloudacct"
	"bak/internal/provider"
	"bak/internal/store"

	"gorm.io/gorm"
)

const testJWTSecret = "test-jwt-secret"

// memAPIStore 是 APIStore 的内存实现。
type memAPIStore struct {
	mu       sync.Mutex
	clouds   []store.CloudAccount
	tasks    []backup.Task
	runs     []backup.Run
	files    []backup.FileResult
	fileRuns []uint // 与 files 平行：每条结果所属的 run id
}

func (m *memAPIStore) HasRunning(ctx context.Context, taskID uint) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.TaskID == taskID && r.Status == "running" {
			return true, nil
		}
	}
	return false, nil
}
func (m *memAPIStore) CreateRun(ctx context.Context, run *backup.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	run.ID = uint(len(m.runs) + 1)
	m.runs = append(m.runs, *run)
	return nil
}
func (m *memAPIStore) FinishRun(ctx context.Context, run backup.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.runs {
		if m.runs[i].ID == run.ID {
			m.runs[i] = run
		}
	}
	return nil
}
func (m *memAPIStore) LatestSuccess(ctx context.Context, taskID uint, rel, fp string) (backup.FileResult, bool, error) {
	return backup.FileResult{}, false, nil
}
func (m *memAPIStore) SaveFileResult(ctx context.Context, runID, taskID uint, r backup.FileResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files = append(m.files, r)
	m.fileRuns = append(m.fileRuns, runID)
	return nil
}
func (m *memAPIStore) UpdateRunProgress(ctx context.Context, run backup.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.runs {
		if m.runs[i].ID == run.ID && m.runs[i].Status == "running" {
			m.runs[i].SuccessCount, m.runs[i].SkipCount, m.runs[i].FailCount = run.SuccessCount, run.SkipCount, run.FailCount
		}
	}
	return nil
}
func (m *memAPIStore) ListFileResults(ctx context.Context, runID uint) ([]backup.FileResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []backup.FileResult{}
	for i, r := range m.files {
		if m.fileRuns[i] == runID {
			out = append(out, r)
		}
	}
	return out, nil
}
func (m *memAPIStore) UpsertCloud(ctx context.Context, a *store.CloudAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.clouds {
		if m.clouds[i].Provider == a.Provider {
			a.ID = m.clouds[i].ID
			m.clouds[i] = *a
			return nil
		}
	}
	a.ID = uint(len(m.clouds) + 1)
	m.clouds = append(m.clouds, *a)
	return nil
}
func (m *memAPIStore) ListClouds(ctx context.Context) ([]store.CloudAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]store.CloudAccount(nil), m.clouds...), nil
}
func (m *memAPIStore) GetCloud(ctx context.Context, id uint) (store.CloudAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.clouds {
		if c.ID == id {
			return c, nil
		}
	}
	return store.CloudAccount{}, gorm.ErrRecordNotFound
}
func (m *memAPIStore) CreateTask(ctx context.Context, t *backup.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t.ID = uint(len(m.tasks) + 1)
	m.tasks = append(m.tasks, *t)
	return nil
}
func (m *memAPIStore) UpdateTask(ctx context.Context, t *backup.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == t.ID {
			m.tasks[i] = *t
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}
func (m *memAPIStore) ListTasks(ctx context.Context) ([]backup.Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]backup.Task(nil), m.tasks...), nil
}
func (m *memAPIStore) DeleteTask(ctx context.Context, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.tasks {
		if m.tasks[i].ID == id {
			m.tasks = append(m.tasks[:i], m.tasks[i+1:]...)
			return nil
		}
	}
	return nil
}
func (m *memAPIStore) GetRun(ctx context.Context, id uint) (backup.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.ID == id {
			return r, nil
		}
	}
	return backup.Run{}, gorm.ErrRecordNotFound
}
func (m *memAPIStore) ListRuns(ctx context.Context, limit int) ([]backup.Run, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []backup.Run{}
	for i := len(m.runs) - 1; i >= 0; i-- {
		out = append(out, m.runs[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

type fakeProvider struct {
	mu       sync.Mutex
	testErr  error
	uploaded []string
}

func (f *fakeProvider) Test(ctx context.Context) error { return f.testErr }
func (f *fakeProvider) EnsureDir(ctx context.Context, remoteDir string) error {
	return nil
}
func (f *fakeProvider) Upload(ctx context.Context, localPath, remotePath string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploaded = append(f.uploaded, remotePath)
	return nil
}

type fakeTokens struct {
	exchangeErr error
	refreshErr  error
	refreshed   int
}

func (f *fakeTokens) Exchange(ctx context.Context, prov string, s cloudacct.Secrets, code string) (cloudacct.Secrets, error) {
	if f.exchangeErr != nil {
		return s, f.exchangeErr
	}
	s.AccessToken = "access-" + code
	s.RefreshToken = "refresh-" + code
	s.ExpiresAt = time.Now().Add(time.Hour)
	return s, nil
}
func (f *fakeTokens) Refresh(ctx context.Context, prov string, s cloudacct.Secrets) (cloudacct.Secrets, error) {
	f.refreshed++
	if f.refreshErr != nil {
		return s, f.refreshErr
	}
	s.AccessToken = "access-new"
	s.ExpiresAt = time.Now().Add(time.Hour)
	return s, nil
}

type apiEnv struct {
	h      http.Handler
	store  *memAPIStore
	prov   *fakeProvider
	tokens *fakeTokens
	cookie *http.Cookie
}

func newAPIEnv(t *testing.T) *apiEnv {
	t.Helper()
	env := &apiEnv{store: &memAPIStore{}, prov: &fakeProvider{}, tokens: &fakeTokens{}}
	env.h = NewMux(Deps{
		AuthDeps:  AuthDeps{Users: &memAuthUsers{}, Sessions: &memSessions{}, SessionTTL: time.Hour},
		Store:     env.store,
		JWTSecret: testJWTSecret,
		Providers: func(name string, s cloudacct.Secrets) (provider.Provider, error) { return env.prov, nil },
		Tokens:    env.tokens,
	})
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup",
		strings.NewReader(`{"username":"me","password":"password1","confirm":"password1"}`)))
	if rec.Code != 200 {
		t.Fatalf("setup %d %s", rec.Code, rec.Body.String())
	}
	env.cookie = rec.Result().Cookies()[0]
	return env
}

func (e *apiEnv) do(method, path, body string) *httptest.ResponseRecorder {
	var rd *bytes.Reader
	if body == "" {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, rd)
	req.AddCookie(e.cookie)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

// startRun 发起手动备份并要求 202；锁可能还被上一次备份的 goroutine 持有几毫秒，所以 409 时短暂重试。
func (e *apiEnv) startRun(t *testing.T, runURL string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rec := e.do(http.MethodPost, runURL, "")
		if rec.Code == http.StatusConflict && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		if rec.Code != http.StatusAccepted {
			t.Fatalf("run %d %s", rec.Code, rec.Body.String())
		}
		var v map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// waitRun 轮询 GET /api/runs/{id} 直到状态不再是 running，返回最终详情。
func (e *apiEnv) waitRun(t *testing.T, id uint) map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		rec := e.do(http.MethodGet, "/api/runs/"+strconv.Itoa(int(id)), "")
		if rec.Code != 200 {
			t.Fatalf("run detail %d %s", rec.Code, rec.Body.String())
		}
		var v map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if v["status"] != "running" {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("run %d still running: %s", id, rec.Body.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *apiEnv) saveWebDAV(t *testing.T) {
	t.Helper()
	rec := e.do(http.MethodPut, "/api/clouds/webdav", `{"url":"https://dav.example.com/dav","username":"alice","password":"s3cret-pw"}`)
	if rec.Code != 200 {
		t.Fatalf("put webdav %d %s", rec.Code, rec.Body.String())
	}
}

func TestCloudTaskAPI(t *testing.T) {
	env := newAPIEnv(t)

	// 未登录访问受保护接口返回 401。
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	if rec.Code != 401 {
		t.Fatalf("unauthenticated tasks %d", rec.Code)
	}

	// 概览始终包含三个 provider，缺失的为 disconnected。
	rec = env.do(http.MethodGet, "/api/overview", "")
	if rec.Code != 200 {
		t.Fatalf("overview %d", rec.Code)
	}
	var ov struct {
		Clouds []struct {
			Provider string `json:"provider"`
			Status   string `json:"status"`
		} `json:"clouds"`
		TaskCount  int               `json:"taskCount"`
		RecentRuns []json.RawMessage `json:"recentRuns"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil {
		t.Fatal(err)
	}
	if len(ov.Clouds) != 3 || ov.RecentRuns == nil {
		t.Fatalf("overview %s", rec.Body.String())
	}
	for i, p := range []string{"baidu", "aliyun", "webdav"} {
		if ov.Clouds[i].Provider != p || ov.Clouds[i].Status != "disconnected" {
			t.Fatalf("overview cloud %d: %+v", i, ov.Clouds[i])
		}
	}

	// 保存 WebDAV，列表不泄露密码和密钥。
	env.saveWebDAV(t)
	rec = env.do(http.MethodGet, "/api/clouds", "")
	if rec.Code != 200 {
		t.Fatalf("clouds %d", rec.Code)
	}
	for _, bad := range []string{"s3cret-pw", "password", "secret_blob", "secretBlob", env.store.clouds[0].SecretBlob} {
		if strings.Contains(rec.Body.String(), bad) {
			t.Fatalf("clouds leak %q: %s", bad, rec.Body.String())
		}
	}
	got, err := cloudacct.Open(testJWTSecret, env.store.clouds[0].SecretBlob)
	if err != nil || got.Password != "s3cret-pw" || got.Username != "alice" {
		t.Fatalf("sealed secrets %+v %v", got, err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 本地目录不存在返回 400 和字段错误。
	missing := filepath.Join(dir, "nope")
	rec = env.do(http.MethodPost, "/api/tasks", `{"name":"n","localDir":"`+missing+`","provider":"webdav","remoteDir":"/bak","scheduleTime":"02:00"}`)
	if rec.Code != 400 {
		t.Fatalf("missing dir %d", rec.Code)
	}
	var fe map[string]map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &fe); err != nil || len(fe) != 1 || len(fe["fields"]) != 1 || fe["fields"]["localDir"] != "本地目录不存在或不可读" {
		t.Fatalf("field error body %q", rec.Body.String())
	}

	// remoteDir 必须以 / 开头。
	rec = env.do(http.MethodPost, "/api/tasks", `{"name":"n","localDir":"`+dir+`","provider":"webdav","remoteDir":"bak","scheduleTime":"02:00"}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "remoteDir") {
		t.Fatalf("remote dir %d %s", rec.Code, rec.Body.String())
	}

	// 创建任务。
	rec = env.do(http.MethodPost, "/api/tasks", `{"name":"docs","localDir":"`+dir+`","provider":"webdav","remoteDir":"/bak","scheduleTime":"02:00","enabled":true}`)
	if rec.Code != 200 && rec.Code != 201 {
		t.Fatalf("create task %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID       uint   `json:"id"`
		Provider string `json:"provider"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 || created.Provider != "webdav" {
		t.Fatalf("created %s %v", rec.Body.String(), err)
	}
	runURL := "/api/tasks/" + strconv.Itoa(int(created.ID)) + "/run"

	rec = env.do(http.MethodGet, "/api/tasks", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"docs"`) {
		t.Fatalf("list tasks %d %s", rec.Code, rec.Body.String())
	}

	// 立即运行：202 返回 running 记录，轮询到成功，触发值为 manual。
	started := env.startRun(t, runURL)
	if started["status"] != "running" || started["trigger"] != "manual" || started["id"] == nil || started["id"].(float64) == 0 {
		t.Fatalf("started %v", started)
	}
	var run struct {
		ID uint
	}
	run.ID = uint(started["id"].(float64))
	final := env.waitRun(t, run.ID)
	if final["status"] != "success" || final["trigger"] != "manual" || final["successCount"].(float64) != 1 {
		t.Fatalf("run body %v", final)
	}
	if len(env.prov.uploaded) != 1 || env.prov.uploaded[0] != "/bak/a.txt" {
		t.Fatalf("uploaded %v", env.prov.uploaded)
	}

	// 运行记录接口。
	rec = env.do(http.MethodGet, "/api/runs", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"manual"`) {
		t.Fatalf("runs %d %s", rec.Code, rec.Body.String())
	}
	rec = env.do(http.MethodGet, "/api/runs/"+strconv.Itoa(int(run.ID)), "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"success"`) {
		t.Fatalf("run detail %d %s", rec.Code, rec.Body.String())
	}
	if rec = env.do(http.MethodGet, "/api/runs/999", ""); rec.Code != 404 {
		t.Fatalf("missing run %d", rec.Code)
	}

	// 该任务已有 running 记录时再次运行返回 409。
	env.store.runs = append(env.store.runs, backup.Run{ID: 100, TaskID: created.ID, Trigger: "schedule", Status: "running", StartedAt: time.Now()})
	rec = env.do(http.MethodPost, runURL, "")
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "该任务正在备份") {
		t.Fatalf("busy %d %s", rec.Code, rec.Body.String())
	}

	// 概览统计任务数和最近运行。
	rec = env.do(http.MethodGet, "/api/overview", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &ov); err != nil || ov.TaskCount != 1 || len(ov.RecentRuns) == 0 {
		t.Fatalf("overview after %s", rec.Body.String())
	}
}

func TestTaskUpdateAndDelete(t *testing.T) {
	env := newAPIEnv(t)
	env.saveWebDAV(t)
	dir := t.TempDir()
	rec := env.do(http.MethodPost, "/api/tasks", `{"name":"docs","localDir":"`+dir+`","provider":"webdav","remoteDir":"/bak","scheduleTime":"02:00"}`)
	if rec.Code != 200 && rec.Code != 201 {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}

	rec = env.do(http.MethodPut, "/api/tasks/1", `{"name":"docs2","localDir":"`+dir+`","provider":"webdav","remoteDir":"/bak2","scheduleTime":"03:30","enabled":false}`)
	if rec.Code != 200 {
		t.Fatalf("update %d %s", rec.Code, rec.Body.String())
	}
	if tk := env.store.tasks[0]; tk.Name != "docs2" || tk.RemoteDir != "/bak2" || tk.ScheduleTime != "03:30" || tk.Enabled {
		t.Fatalf("updated task %+v", tk)
	}
	rec = env.do(http.MethodPut, "/api/tasks/1", `{"name":"x","localDir":"/definitely/not/here","provider":"webdav","remoteDir":"/bak"}`)
	if rec.Code != 400 {
		t.Fatalf("update bad dir %d", rec.Code)
	}
	if rec = env.do(http.MethodPut, "/api/tasks/77", `{"name":"x","localDir":"`+dir+`","provider":"webdav","remoteDir":"/bak"}`); rec.Code != 404 {
		t.Fatalf("update missing %d", rec.Code)
	}
	if rec = env.do(http.MethodPost, "/api/tasks/77/run", ""); rec.Code != 404 {
		t.Fatalf("run missing %d", rec.Code)
	}

	if rec = env.do(http.MethodDelete, "/api/tasks/1", ""); rec.Code != 204 {
		t.Fatalf("delete %d", rec.Code)
	}
	if len(env.store.tasks) != 0 {
		t.Fatal("task not deleted")
	}
}

func TestCloudTestAndAuthorize(t *testing.T) {
	env := newAPIEnv(t)
	env.saveWebDAV(t)

	// 测试连接成功。
	rec := env.do(http.MethodPost, "/api/clouds/webdav/test", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"connected"`) {
		t.Fatalf("test ok %d %s", rec.Code, rec.Body.String())
	}
	// 测试连接失败时把错误写入 last_error。
	env.prov.testErr = errors.New("认证失败")
	rec = env.do(http.MethodPost, "/api/clouds/webdav/test", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "认证失败") || !strings.Contains(rec.Body.String(), `"error"`) {
		t.Fatalf("test fail %d %s", rec.Code, rec.Body.String())
	}
	if env.store.clouds[0].LastError != "认证失败" || env.store.clouds[0].Status != "error" {
		t.Fatalf("stored %+v", env.store.clouds[0])
	}
	if rec = env.do(http.MethodPost, "/api/clouds/aliyun/test", ""); rec.Code != 404 {
		t.Fatalf("test unconfigured %d", rec.Code)
	}
	if rec = env.do(http.MethodPut, "/api/clouds/nope", `{}`); rec.Code != 404 {
		t.Fatalf("unknown provider %d", rec.Code)
	}
	if rec = env.do(http.MethodPut, "/api/clouds/webdav", `{"url":"ftp://x","username":""}`); rec.Code != 400 {
		t.Fatalf("bad webdav %d", rec.Code)
	}

	// 保存百度配置后，授权先返回授权链接，再用授权码换令牌。
	rec = env.do(http.MethodPut, "/api/clouds/baidu", `{"clientId":"cid","clientSecret":"csecret-value","deviceId":"dev"}`)
	if rec.Code != 200 || strings.Contains(rec.Body.String(), "csecret-value") {
		t.Fatalf("put baidu %d %s", rec.Code, rec.Body.String())
	}
	rec = env.do(http.MethodPost, "/api/clouds/baidu/authorize", `{}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "openapi.baidu.com/oauth/2.0/authorize") || !strings.Contains(rec.Body.String(), "client_id=cid") {
		t.Fatalf("authorize url %d %s", rec.Code, rec.Body.String())
	}
	rec = env.do(http.MethodPost, "/api/clouds/baidu/authorize", `{"code":"abc"}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"connected"`) {
		t.Fatalf("authorize code %d %s", rec.Code, rec.Body.String())
	}
	for _, bad := range []string{"access-abc", "refresh-abc", "csecret-value"} {
		if strings.Contains(rec.Body.String(), bad) {
			t.Fatalf("authorize leaks %q", bad)
		}
	}
	var baidu store.CloudAccount
	for _, c := range env.store.clouds {
		if c.Provider == "baidu" {
			baidu = c
		}
	}
	sec, err := cloudacct.Open(testJWTSecret, baidu.SecretBlob)
	if err != nil || sec.AccessToken != "access-abc" || sec.RefreshToken != "refresh-abc" || sec.ClientSecret != "csecret-value" {
		t.Fatalf("sealed baidu %+v %v", sec, err)
	}
	// 更新 clientId 以外的字段时不丢失已有的 clientSecret。
	rec = env.do(http.MethodPut, "/api/clouds/baidu", `{"clientId":"cid","clientSecret":""}`)
	if rec.Code != 200 {
		t.Fatalf("put baidu again %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range env.store.clouds {
		if c.Provider == "baidu" {
			sec, _ = cloudacct.Open(testJWTSecret, c.SecretBlob)
		}
	}
	if sec.ClientSecret != "csecret-value" || sec.AccessToken != "access-abc" {
		t.Fatalf("secrets lost %+v", sec)
	}

	// WebDAV 不支持授权。
	if rec = env.do(http.MethodPost, "/api/clouds/webdav/authorize", `{}`); rec.Code != 400 {
		t.Fatalf("webdav authorize %d", rec.Code)
	}
	// 授权失败返回 400，且不泄露细节。
	env.tokens.exchangeErr = errors.New("token=leaky")
	rec = env.do(http.MethodPost, "/api/clouds/baidu/authorize", `{"code":"bad"}`)
	if rec.Code != 400 || strings.Contains(rec.Body.String(), "leaky") {
		t.Fatalf("authorize fail %d %s", rec.Code, rec.Body.String())
	}
}

func TestRunRefreshesTokenOrRecordsFailure(t *testing.T) {
	env := newAPIEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob, err := cloudacct.Seal(testJWTSecret, cloudacct.Secrets{
		ClientID: "cid", ClientSecret: "cs", AccessToken: "old", RefreshToken: "rt",
		ExpiresAt: time.Now().Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.UpsertCloud(context.Background(), &store.CloudAccount{Provider: "baidu", DisplayName: "百度网盘", SecretBlob: blob, Status: "connected"}); err != nil {
		t.Fatal(err)
	}
	rec := env.do(http.MethodPost, "/api/tasks", `{"name":"b","localDir":"`+dir+`","provider":"baidu","remoteDir":"/apps/bak","scheduleTime":"01:00"}`)
	if rec.Code != 200 && rec.Code != 201 {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}

	// 令牌 5 分钟内到期：先刷新再运行。
	started := env.startRun(t, "/api/tasks/1/run")
	final := env.waitRun(t, uint(started["id"].(float64)))
	if final["status"] != "success" || env.tokens.refreshed != 1 {
		t.Fatalf("run after refresh %v refreshed=%d", final, env.tokens.refreshed)
	}
	sec, _ := cloudacct.Open(testJWTSecret, env.store.clouds[0].SecretBlob)
	if sec.AccessToken != "access-new" {
		t.Fatalf("token not persisted: %+v", sec)
	}

	// 令牌尚未到期不刷新。
	started = env.startRun(t, "/api/tasks/1/run")
	env.waitRun(t, uint(started["id"].(float64)))
	if env.tokens.refreshed != 1 {
		t.Fatalf("second run refreshed=%d", env.tokens.refreshed)
	}

	// 刷新失败：返回 200，运行记录为 failed。
	blob, _ = cloudacct.Seal(testJWTSecret, cloudacct.Secrets{
		ClientID: "cid", ClientSecret: "cs", AccessToken: "old", RefreshToken: "rt",
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	env.store.clouds[0].SecretBlob = blob
	env.tokens.refreshErr = errors.New("refresh_token=leaky")
	before := len(env.prov.uploaded)
	for i := 0; i < 200; i++ { // 等上一次备份的 goroutine 释放锁
		rec = env.do(http.MethodPost, "/api/tasks/1/run", "")
		if rec.Code != http.StatusConflict {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"failed"`) || strings.Contains(rec.Body.String(), "leaky") {
		t.Fatalf("refresh failure %d %s", rec.Code, rec.Body.String())
	}
	if len(env.prov.uploaded) != before {
		t.Fatal("uploaded despite refresh failure")
	}
	last := env.store.runs[len(env.store.runs)-1]
	if last.Status != "failed" || last.Trigger != "manual" || last.FinishedAt == nil || last.ErrorSummary == "" {
		t.Fatalf("stored run %+v", last)
	}
}

func TestRunDetailListsFiles(t *testing.T) {
	env := newAPIEnv(t)
	env.store.runs = append(env.store.runs, backup.Run{ID: 1, TaskID: 1, Trigger: "manual", Status: "partial", StartedAt: time.Now()},
		backup.Run{ID: 2, TaskID: 1, Trigger: "manual", Status: "success", StartedAt: time.Now()})
	ctx := context.Background()
	_ = env.store.SaveFileResult(ctx, 1, 1, backup.FileResult{RelativePath: "docs/a.txt", Status: "success"})
	_ = env.store.SaveFileResult(ctx, 1, 1, backup.FileResult{RelativePath: "b.txt", Status: "failed", Error: "网盘拒绝"})
	_ = env.store.SaveFileResult(ctx, 2, 1, backup.FileResult{RelativePath: "other.txt", Status: "skipped"})

	rec := env.do(http.MethodGet, "/api/runs/1", "")
	if rec.Code != 200 {
		t.Fatalf("detail %d %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Files []struct {
			RelativePath string `json:"relativePath"`
			Status       string `json:"status"`
			Error        string `json:"error"`
		} `json:"files"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Files) != 2 || detail.Files[0].RelativePath != "docs/a.txt" || detail.Files[0].Status != "success" ||
		detail.Files[1].RelativePath != "b.txt" || detail.Files[1].Status != "failed" || detail.Files[1].Error != "网盘拒绝" {
		t.Fatalf("files %s", rec.Body.String())
	}

	// 没有文件时 files 是空数组而不是 null；列表接口不含逐文件数据。
	var empty map[string]json.RawMessage
	env.store.runs = append(env.store.runs, backup.Run{ID: 3, TaskID: 1, Status: "success", StartedAt: time.Now()})
	rec = env.do(http.MethodGet, "/api/runs/3", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &empty); err != nil || string(empty["files"]) != "[]" {
		t.Fatalf("empty files %s", rec.Body.String())
	}
	rec = env.do(http.MethodGet, "/api/runs", "")
	if strings.Contains(rec.Body.String(), "relativePath") || strings.Contains(rec.Body.String(), `"files"`) {
		t.Fatalf("list must not carry files: %s", rec.Body.String())
	}
}

// blockingProvider 在 Upload 里等待放行，用来证明 POST /run 不会等备份结束。
type blockingProvider struct {
	fakeProvider
	release chan struct{}
	entered chan struct{}
}

func (b *blockingProvider) Upload(ctx context.Context, localPath, remotePath string) error {
	select {
	case b.entered <- struct{}{}:
	default:
	}
	<-b.release
	return b.fakeProvider.Upload(ctx, localPath, remotePath)
}

func TestManualRunReturns202WhileWalking(t *testing.T) {
	env := newAPIEnv(t)
	bp := &blockingProvider{release: make(chan struct{}), entered: make(chan struct{}, 1)}
	released := false
	release := func() {
		if !released {
			released = true
			close(bp.release)
		}
	}
	defer release()
	env.h = NewMux(Deps{
		AuthDeps:  AuthDeps{Users: &memAuthUsers{}, Sessions: &memSessions{}, SessionTTL: time.Hour},
		Store:     env.store,
		JWTSecret: testJWTSecret,
		Providers: func(name string, s cloudacct.Secrets) (provider.Provider, error) { return bp, nil },
		Tokens:    env.tokens,
	})
	// 新的 mux 有自己的用户和会话，重新 setup。
	req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(`{"username":"me","password":"password1","confirm":"password1"}`))
	w := httptest.NewRecorder()
	env.h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("setup %d %s", w.Code, w.Body.String())
	}
	env.cookie = w.Result().Cookies()[0]

	env.saveWebDAV(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := env.do(http.MethodPost, "/api/tasks", `{"name":"docs","localDir":"`+dir+`","provider":"webdav","remoteDir":"/bak"}`)
	if rec.Code != 200 {
		t.Fatalf("create %d %s", rec.Code, rec.Body.String())
	}

	rec = env.do(http.MethodPost, "/api/tasks/1/run", "")
	if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), `"running"`) {
		t.Fatalf("run %d %s", rec.Code, rec.Body.String())
	}
	var v struct {
		ID uint `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &v)
	if v.ID == 0 {
		t.Fatalf("no id: %s", rec.Body.String())
	}
	select {
	case <-bp.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("walk never started")
	}
	// 备份进行中：状态仍为 running，再次运行返回 409（锁和 running 记录都在）。
	if got := env.waitRunOnce(t, v.ID); got["status"] != "running" {
		t.Fatalf("expected running, got %v", got)
	}
	if rec = env.do(http.MethodPost, "/api/tasks/1/run", ""); rec.Code != http.StatusConflict {
		t.Fatalf("second run %d", rec.Code)
	}
	release()
	if final := env.waitRun(t, v.ID); final["status"] != "success" {
		t.Fatalf("final %v", final)
	}
}

func TestListCloudsReturnsBaiduSyncFailure(t *testing.T) {
	env := newAPIEnv(t)
	env.h = NewMux(Deps{
		AuthDeps:  AuthDeps{Users: &memAuthUsers{}, Sessions: &memSessions{}, SessionTTL: time.Hour},
		Store:     env.store,
		JWTSecret: testJWTSecret,
		Providers: func(name string, s cloudacct.Secrets) (provider.Provider, error) { return env.prov, nil },
		Tokens:    env.tokens,
		SyncBaidu: func(s cloudacct.Secrets) (cloudacct.Secrets, bool, error) {
			return s, false, errors.New("refresh rejected")
		},
	})
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup",
		strings.NewReader(`{"username":"me2","password":"password1","confirm":"password1"}`)))
	if rec.Code != 200 {
		t.Fatalf("setup %d %s", rec.Code, rec.Body.String())
	}
	env.cookie = rec.Result().Cookies()[0]

	blob, err := cloudacct.Seal(testJWTSecret, cloudacct.Secrets{
		ClientID: "cid", DeviceID: "dev", AccessToken: "old", RefreshToken: "rt",
		ExpiresAt: time.Now().Add(-time.Hour),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.UpsertCloud(context.Background(), &store.CloudAccount{
		Provider: "baidu", DisplayName: "百度网盘", SecretBlob: blob, Status: "connected",
	}); err != nil {
		t.Fatal(err)
	}

	rec = env.do(http.MethodGet, "/api/clouds", "")
	if rec.Code != 200 {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}
	var clouds []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &clouds); err != nil {
		t.Fatal(err)
	}
	var baidu map[string]any
	for _, c := range clouds {
		if c["provider"] == "baidu" {
			baidu = c
		}
	}
	if baidu == nil {
		t.Fatalf("missing baidu: %s", rec.Body.String())
	}
	if baidu["status"] != "error" {
		t.Fatalf("status = %v body %s", baidu["status"], rec.Body.String())
	}
	last, _ := baidu["lastError"].(string)
	if !strings.Contains(last, "refresh rejected") {
		t.Fatalf("lastError = %q", last)
	}
	cfg, _ := baidu["config"].(map[string]any)
	if cfg["clientId"] != "cid" || cfg["deviceId"] != "dev" {
		t.Fatalf("config %+v", cfg)
	}
	var stored store.CloudAccount
	for _, c := range env.store.clouds {
		if c.Provider == "baidu" {
			stored = c
		}
	}
	if stored.Status != "error" || !strings.Contains(stored.LastError, "refresh rejected") {
		t.Fatalf("stored %+v", stored)
	}
}

func TestListCloudsClearsBaiduSyncErrorAfterSuccess(t *testing.T) {
	env := newAPIEnv(t)
	env.h = NewMux(Deps{
		AuthDeps:  AuthDeps{Users: &memAuthUsers{}, Sessions: &memSessions{}, SessionTTL: time.Hour},
		Store:     env.store,
		JWTSecret: testJWTSecret,
		Providers: func(name string, s cloudacct.Secrets) (provider.Provider, error) { return env.prov, nil },
		Tokens:    env.tokens,
		SyncBaidu: func(s cloudacct.Secrets) (cloudacct.Secrets, bool, error) {
			return s, false, nil
		},
	})
	rec := httptest.NewRecorder()
	env.h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup",
		strings.NewReader(`{"username":"me3","password":"password1","confirm":"password1"}`)))
	if rec.Code != 200 {
		t.Fatalf("setup %d %s", rec.Code, rec.Body.String())
	}
	env.cookie = rec.Result().Cookies()[0]

	blob, err := cloudacct.Seal(testJWTSecret, cloudacct.Secrets{ClientID: "cid", AccessToken: "ok", RefreshToken: "rt"})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.store.UpsertCloud(context.Background(), &store.CloudAccount{
		Provider: "baidu", DisplayName: "百度网盘", SecretBlob: blob,
		Status: "error", LastError: "同步百度令牌失败: refresh rejected",
	}); err != nil {
		t.Fatal(err)
	}

	rec = env.do(http.MethodGet, "/api/clouds", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"connected"`) || strings.Contains(rec.Body.String(), "refresh rejected") {
		t.Fatalf("list %d %s", rec.Code, rec.Body.String())
	}
}

func (e *apiEnv) waitRunOnce(t *testing.T, id uint) map[string]any {
	t.Helper()
	rec := e.do(http.MethodGet, "/api/runs/"+strconv.Itoa(int(id)), "")
	var v map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v
}
