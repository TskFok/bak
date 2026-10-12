# 本机多云盘备份面板 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在现有 Go 命令行程序上增加只监听本机的 Web 面板，用一个本地账号登录后，把本地目录单向备份到百度网盘、阿里云盘或 WebDAV，并支持立即备份和每天定时。

**Architecture:** 同一个 Go 进程提供 chi HTTP 服务和 30 秒定时器。网盘收成 `Provider` 接口；备份运行器依赖该接口和存储接口，测试使用内存实现。React 页面构建后由 `go:embed` 提供。百度分片上传从 `cmd/upload.go` 挪到百度适配器。

**Tech Stack:** Go 1.20、chi v5、GORM、MySQL、Redis、bcrypt、AES-GCM、React 18、Vite。

## Global Constraints

- 默认监听 `127.0.0.1:8080`；`--addr` 只接受回环地址（`127.0.0.1`、`::1`、`localhost`），否则 `serve` 拒绝启动。
- `--dev` 把非 `/api` 请求代理到 `http://127.0.0.1:5173`。未加 `--dev` 时用 `go:embed` 提供 `web/dist`。
- 路由使用 chi。保持 Go 1.20。不引入 Gin。
- 继续使用现有 MySQL、Redis 和 `utils/conf/conf.yaml`。
- 网盘只有 `baidu`、`aliyun`、`webdav`。每种最多一个账号。
- 单向备份。云端变化不回写本地，本地删除不删除云端文件。
- 面板只有一个本地账号。用户名 1 到 32 个字符。密码至少 8 位。设置接口在事务中拒绝第二行。
- Cookie 名 `session`，HttpOnly，SameSite=Lax，Path=/，不设置 Secure。Redis 键 `session:<id>`，TTL 为 `jwt.expire` 小时。
- 密钥用 AES-GCM 加密。密钥材料是 `jwt.secret` 的 SHA-256。响应和错误文本不包含密码、client secret、access token、refresh token。
- 百度和阿里云盘密钥 JSON 字段：`client_id`、`client_secret`、`access_token`、`refresh_token`、`expires_at`。百度另存 `device_id`，因为现有授权链接需要它。WebDAV 字段：`url`、`username`、`password`。
- 阿里云盘只调用 `https://openapi.alipan.com` 的官方接口。
- 分片目录是 `runtime/chunks/`，上传结束后删除。分片大小默认 `4 * 1024 * 1024`。
- 跳过条件：同一任务、相对路径、大小、修改时间（精确到秒）、`source_fingerprint` 都与最近一条 `success` 记录一致。
- `source_fingerprint` 是 `local_dir + "\n" + cloud_account_id + "\n" + remote_dir` 的 SHA-256 十六进制。
- 运行状态：失败数为 0 时 `success`；成功数和跳过数都为 0 且失败数大于 0 时 `failed`；其余有失败时 `partial`。空目录为 `success`。
- 令牌在 5 分钟内到期时先刷新。刷新失败则整次 `failed`。`EnsureDir` 失败也是整次 `failed`。单文件失败继续。
- 同一任务已有 `running` 时，手动运行返回 409，定时本轮跳过且不更新 `last_scheduled_on`。
- 定时器每 30 秒检查。手动运行不修改 `last_scheduled_on`。
- `serve` 启动时把遗留 `running` 改为 `failed`，`error_summary` 为 `进程已退出`。
- 删除任务保留运行记录和文件结果。
- 除 `POST /api/setup`、`GET /api/setup/status`、`POST /api/login` 和静态页面外，无会话的 `/api` 返回 401。
- 概览和运行详情每 3 秒刷新进行中的记录。
- 单元测试不访问真实网盘。

---

### Task 1: 密钥加密

**Files:**
- Create: `internal/crypto/secret.go`
- Test: `internal/crypto/secret_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `func Encrypt(jwtSecret, plaintext string) (string, error)`
  - `func Decrypt(jwtSecret, blob string) (string, error)`

- [ ] **Step 1: Write the failing test**

```go
package crypto

import "testing"

func TestRoundTrip(t *testing.T) {
	blob, err := Encrypt("jwt-secret", `{"password":"p@ss"}`)
	if err != nil {
		t.Fatal(err)
	}
	if blob == `{"password":"p@ss"}` {
		t.Fatal("ciphertext must not equal plaintext")
	}
	got, err := Decrypt("jwt-secret", blob)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"password":"p@ss"}` {
		t.Fatalf("got %s", got)
	}
}

func TestDecryptRejectsWrongKey(t *testing.T) {
	blob, err := Encrypt("jwt-secret", "token-value")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt("other", blob); err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/crypto/ -count=1`
Expected: FAIL，`Encrypt` 未定义

- [ ] **Step 3: Write minimal implementation**

```go
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

func Encrypt(jwtSecret, plaintext string) (string, error) {
	sum := sha256.Sum256([]byte(jwtSecret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func Decrypt(jwtSecret, blob string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(jwtSecret))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("密文太短")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/crypto/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/crypto/secret.go internal/crypto/secret_test.go
git commit -m "feat: encrypt cloud secrets with AES-GCM"
```

---

### Task 2: 本地账号设置与登录

**Files:**
- Create: `internal/auth/auth.go`
- Test: `internal/auth/auth_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `type User struct { ID uint; Username string; PasswordHash string }`
  - `type UserStore interface { Count(ctx context.Context) (int64, error); Create(ctx context.Context, user *User) error; FindByUsername(ctx context.Context, username string) (User, error) }`
  - `type FieldError struct { Fields map[string]string }`
  - `func Setup(ctx context.Context, store UserStore, username, password, confirm string) (User, error)`
  - `func Login(ctx context.Context, store UserStore, username, password string) (User, error)`
  - `var ErrUserExists = errors.New("已经创建过本地账号")`
  - `var ErrBadLogin = errors.New("用户名或密码错误")`

- [ ] **Step 1: Write the failing test**

```go
package auth

import (
	"context"
	"errors"
	"testing"
)

type memUsers struct {
	users []User
}

func (m *memUsers) Count(ctx context.Context) (int64, error) { return int64(len(m.users)), nil }
func (m *memUsers) Create(ctx context.Context, user *User) error {
	user.ID = uint(len(m.users) + 1)
	m.users = append(m.users, *user)
	return nil
}
func (m *memUsers) FindByUsername(ctx context.Context, username string) (User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return User{}, errors.New("not found")
}

func TestSetupOnceAndLogin(t *testing.T) {
	store := &memUsers{}
	ctx := context.Background()
	if _, err := Setup(ctx, store, "me", "password1", "password1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Setup(ctx, store, "other", "password1", "password1"); !errors.Is(err, ErrUserExists) {
		t.Fatalf("got %v", err)
	}
	if _, err := Login(ctx, store, "me", "wrong-password"); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("got %v", err)
	}
	user, err := Login(ctx, store, "me", "password1")
	if err != nil || user.Username != "me" {
		t.Fatalf("user %+v err %v", user, err)
	}
}

func TestSetupFieldErrors(t *testing.T) {
	store := &memUsers{}
	_, err := Setup(context.Background(), store, "", "short", "other")
	fe, ok := err.(*FieldError)
	if !ok {
		t.Fatalf("got %v", err)
	}
	if fe.Fields["username"] == "" || fe.Fields["password"] == "" || fe.Fields["confirm"] == "" {
		t.Fatalf("%v", fe.Fields)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ -count=1`
Expected: FAIL，`Setup` 未定义

- [ ] **Step 3: Write minimal implementation**

先执行 `go get golang.org/x/crypto/bcrypt`。

```go
package auth

import (
	"context"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

var ErrUserExists = errors.New("已经创建过本地账号")
var ErrBadLogin = errors.New("用户名或密码错误")

type User struct {
	ID           uint
	Username     string
	PasswordHash string
}

type UserStore interface {
	Count(ctx context.Context) (int64, error)
	Create(ctx context.Context, user *User) error
	FindByUsername(ctx context.Context, username string) (User, error)
}

type FieldError struct {
	Fields map[string]string
}

func (e *FieldError) Error() string { return "字段校验失败" }

func Setup(ctx context.Context, store UserStore, username, password, confirm string) (User, error) {
	fields := map[string]string{}
	if username == "" || utf8.RuneCountInString(username) > 32 {
		fields["username"] = "请填写 1 到 32 个字符的用户名"
	}
	if len(password) < 8 {
		fields["password"] = "密码至少 8 位"
	}
	if password != confirm {
		fields["confirm"] = "两次输入的密码不一致"
	}
	if len(fields) > 0 {
		return User{}, &FieldError{Fields: fields}
	}
	n, err := store.Count(ctx)
	if err != nil {
		return User{}, err
	}
	if n > 0 {
		return User{}, ErrUserExists
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	user := &User{Username: username, PasswordHash: string(hash)}
	if err := store.Create(ctx, user); err != nil {
		return User{}, err
	}
	return *user, nil
}

func Login(ctx context.Context, store UserStore, username, password string) (User, error) {
	user, err := store.FindByUsername(ctx, username)
	if err != nil {
		return User{}, ErrBadLogin
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return User{}, ErrBadLogin
	}
	return user, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/auth/auth.go internal/auth/auth_test.go go.mod go.sum
git commit -m "feat: add single local account setup and login"
```

---

### Task 3: 会话、回环地址和登录接口

**Files:**
- Create: `internal/auth/session.go`
- Create: `internal/server/addr.go`
- Create: `internal/server/auth_http.go`
- Create: `internal/server/auth_http_test.go`
- Test: `internal/auth/session_test.go`

**Interfaces:**
- Consumes: Task 2 的 `User`、`UserStore`、`Setup`、`Login`、`FieldError`、`ErrUserExists`、`ErrBadLogin`
- Produces:
  - `type SessionStore interface { Set(ctx context.Context, id string, userID uint, ttl time.Duration) error; Get(ctx context.Context, id string) (uint, error); Delete(ctx context.Context, id string) error }`
  - `func NewSessionID() (string, error)`
  - `func ValidateAddr(addr string) error`
  - `func NewAuthMux(deps AuthDeps) http.Handler`
  - `type AuthDeps struct { Users UserStore; Sessions SessionStore; SessionTTL time.Duration }`

- [ ] **Step 1: Write the failing test**

`internal/auth/session_test.go`:

```go
package auth

import "testing"

func TestNewSessionID(t *testing.T) {
	a, err := NewSessionID()
	if err != nil || len(a) < 32 {
		t.Fatalf("id %q err %v", a, err)
	}
	b, _ := NewSessionID()
	if a == b {
		t.Fatal("session ids collided")
	}
}
```

`internal/server/auth_http_test.go`:

```go
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"BaiduYunPanBak/internal/auth"
)

type memSessions struct{ m map[string]uint }

func (m *memSessions) Set(ctx context.Context, id string, userID uint, ttl time.Duration) error {
	if m.m == nil {
		m.m = map[string]uint{}
	}
	m.m[id] = userID
	return nil
}
func (m *memSessions) Get(ctx context.Context, id string) (uint, error) {
	idv, ok := m.m[id]
	if !ok {
		return 0, auth.ErrBadLogin
	}
	return idv, nil
}
func (m *memSessions) Delete(ctx context.Context, id string) error {
	delete(m.m, id)
	return nil
}

func TestSetupLoginAndGuard(t *testing.T) {
	h := NewAuthMux(AuthDeps{Users: &memAuthUsers{}, Sessions: &memSessions{}, SessionTTL: time.Hour})
	body := []byte(`{"username":"me","password":"password1","confirm":"password1"}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("setup %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(body)))
	if rec.Code != 409 {
		t.Fatalf("second setup %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/me", nil))
	if rec.Code != 401 {
		t.Fatalf("me %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`{"username":"me","password":"nope"}`))))
	if rec.Code != 401 || !bytes.Contains(rec.Body.Bytes(), []byte("用户名或密码错误")) {
		t.Fatalf("login %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`{"username":"me","password":"password1"}`))))
	if rec.Code != 200 || rec.Result().Cookies()[0].Name != "session" || !rec.Result().Cookies()[0].HttpOnly {
		t.Fatalf("cookie missing: %d", rec.Code)
	}
}

func TestValidateAddr(t *testing.T) {
	if err := ValidateAddr("127.0.0.1:8080"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAddr("localhost:8080"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAddr("0.0.0.0:8080"); err == nil {
		t.Fatal("expected reject")
	}
}

type memAuthUsers struct{ users []auth.User }

func (m *memAuthUsers) Count(ctx context.Context) (int64, error) { return int64(len(m.users)), nil }
func (m *memAuthUsers) Create(ctx context.Context, user *auth.User) error {
	user.ID = uint(len(m.users) + 1)
	m.users = append(m.users, *user)
	return nil
}
func (m *memAuthUsers) FindByUsername(ctx context.Context, username string) (auth.User, error) {
	for _, u := range m.users {
		if u.Username == username {
			return u, nil
		}
	}
	return auth.User{}, auth.ErrBadLogin
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/auth/ ./internal/server/ -count=1`
Expected: FAIL，函数未定义

- [ ] **Step 3: Write minimal implementation**

执行 `go get github.com/go-chi/chi/v5`。

`internal/auth/session.go`:

```go
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

type SessionStore interface {
	Set(ctx context.Context, id string, userID uint, ttl time.Duration) error
	Get(ctx context.Context, id string) (uint, error)
	Delete(ctx context.Context, id string) error
}

func NewSessionID() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
```

`internal/server/addr.go`:

```go
package server

import (
	"fmt"
	"net"
	"strings"
)

func ValidateAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("只允许监听回环地址")
	}
	return nil
}
```

`internal/server/auth_http.go`:

```go
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"BaiduYunPanBak/internal/auth"

	"github.com/go-chi/chi/v5"
)

type AuthDeps struct {
	Users      auth.UserStore
	Sessions   auth.SessionStore
	SessionTTL time.Duration
}

func NewAuthMux(deps AuthDeps) http.Handler {
	r := chi.NewRouter()
	r.Get("/api/setup/status", func(w http.ResponseWriter, req *http.Request) {
		n, err := deps.Users.Count(req.Context())
		if err != nil {
			http.Error(w, "服务器错误", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": n == 0})
	})
	r.Post("/api/setup", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Confirm  string `json:"confirm"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "请求无效", http.StatusBadRequest)
			return
		}
		user, err := auth.Setup(req.Context(), deps.Users, body.Username, body.Password, body.Confirm)
		if fe, ok := err.(*auth.FieldError); ok {
			writeJSON(w, http.StatusBadRequest, fe)
			return
		}
		if errors.Is(err, auth.ErrUserExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		if err != nil {
			http.Error(w, "服务器错误", http.StatusInternalServerError)
			return
		}
		if err := startSession(w, req, deps, user.ID); err != nil {
			http.Error(w, "服务器错误", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"username": user.Username})
	})
	r.Post("/api/login", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			http.Error(w, "请求无效", http.StatusBadRequest)
			return
		}
		user, err := auth.Login(req.Context(), deps.Users, body.Username, body.Password)
		if errors.Is(err, auth.ErrBadLogin) {
			http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
			return
		}
		if err != nil {
			http.Error(w, "服务器错误", http.StatusInternalServerError)
			return
		}
		if err := startSession(w, req, deps, user.ID); err != nil {
			http.Error(w, "服务器错误", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"username": user.Username})
	})
	r.Post("/api/logout", func(w http.ResponseWriter, req *http.Request) {
		if c, err := req.Cookie("session"); err == nil {
			_ = deps.Sessions.Delete(req.Context(), c.Value)
		}
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
		w.WriteHeader(http.StatusNoContent)
	})
	r.Group(func(pr chi.Router) {
		pr.Use(requireSession(deps))
		pr.Get("/api/me", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, map[string]uint{"userId": req.Context().Value(userIDKey).(uint)})
		})
	})
	return r
}

type ctxKey int

const userIDKey ctxKey = 1

func requireSession(deps AuthDeps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			c, err := req.Cookie("session")
			if err != nil {
				http.Error(w, "未登录", http.StatusUnauthorized)
				return
			}
			id, err := deps.Sessions.Get(req.Context(), c.Value)
			if err != nil || id == 0 {
				http.Error(w, "未登录", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, req.WithContext(contextWithUser(req, id)))
		})
	}
}

func contextWithUser(req *http.Request, id uint) context.Context {
	return context.WithValue(req.Context(), userIDKey, id)
}

func startSession(w http.ResponseWriter, req *http.Request, deps AuthDeps, userID uint) error {
	id, err := auth.NewSessionID()
	if err != nil {
		return err
	}
	if err := deps.Sessions.Set(req.Context(), id, userID, deps.SessionTTL); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{Name: "session", Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/auth/ ./internal/server/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/auth/session.go internal/auth/session_test.go internal/server go.mod go.sum
git commit -m "feat: add loopback check, session cookie, and auth API"
```

---

### Task 4: WebDAV 适配器

**Files:**
- Create: `internal/provider/provider.go`
- Create: `internal/provider/webdav/client.go`
- Test: `internal/provider/webdav/client_test.go`

**Interfaces:**
- Consumes: 无
- Produces:
  - `type Provider interface { Test(ctx context.Context) error; EnsureDir(ctx context.Context, remoteDir string) error; Upload(ctx context.Context, localPath, remotePath string) error }`
  - `func New(url, username, password string) *Client`
  - `func (c *Client) Test/EnsureDir/Upload`

- [ ] **Step 1: Write the failing test**

```go
package webdav

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(srv.URL, "user", "secret-password")
	err := c.Test(context.Background())
	if err == nil || strings.Contains(err.Error(), "secret-password") {
		t.Fatalf("err %v", err)
	}
}

func TestMkcolAndPut(t *testing.T) {
	var putBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			w.WriteHeader(http.StatusNotFound)
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		case http.MethodPut:
			buf := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(buf)
			putBody = string(buf)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := New(srv.URL, "user", "pw")
	if err := c.EnsureDir(context.Background(), "/bak"); err != nil {
		t.Fatal(err)
	}
	if err := c.Upload(context.Background(), path, "/bak/a.txt"); err != nil {
		t.Fatal(err)
	}
	if putBody != "hello" {
		t.Fatalf("body %q", putBody)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/webdav/ -count=1`
Expected: FAIL，`New` 未定义

- [ ] **Step 3: Write minimal implementation**

`internal/provider/provider.go`:

```go
package provider

import "context"

type Provider interface {
	Test(ctx context.Context) error
	EnsureDir(ctx context.Context, remoteDir string) error
	Upload(ctx context.Context, localPath, remotePath string) error
}
```

`internal/provider/webdav/client.go`:

```go
package webdav

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type Client struct {
	base     string
	username string
	password string
	http     *http.Client
}

func New(url, username, password string) *Client {
	return &Client{base: strings.TrimRight(url, "/"), username: username, password: password, http: http.DefaultClient}
}

func (c *Client) Test(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", c.base+"/", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Depth", "0")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	return c.statusError(res.StatusCode)
}

func (c *Client) EnsureDir(ctx context.Context, remoteDir string) error {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", c.base+remoteDir, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Depth", "0")
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	if res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	mk, err := http.NewRequestWithContext(ctx, "MKCOL", c.base+remoteDir, nil)
	if err != nil {
		return err
	}
	mk.SetBasicAuth(c.username, c.password)
	res, err = c.http.Do(mk)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusMethodNotAllowed || (res.StatusCode >= 200 && res.StatusCode < 300) {
		return nil
	}
	return c.statusError(res.StatusCode)
}

func (c *Client) Upload(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+remotePath, f)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.username, c.password)
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("无法访问")
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return c.statusError(res.StatusCode)
}

func (c *Client) statusError(code int) error {
	if code >= 200 && code < 300 {
		return nil
	}
	if code == http.StatusUnauthorized || code == http.StatusForbidden {
		return fmt.Errorf("认证失败")
	}
	return fmt.Errorf("无法访问")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/webdav/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provider
git commit -m "feat: add WebDAV provider"
```

---

### Task 5: 网盘密钥与授权链接

**Files:**
- Create: `internal/cloudacct/secrets.go`
- Test: `internal/cloudacct/secrets_test.go`

**Interfaces:**
- Consumes: `crypto.Encrypt`、`crypto.Decrypt`
- Produces:
  - `type Secrets struct { ClientID, ClientSecret, AccessToken, RefreshToken, DeviceID, URL, Username, Password string; ExpiresAt time.Time }`
  - `func Seal(jwtSecret string, s Secrets) (string, error)`
  - `func Open(jwtSecret, blob string) (Secrets, error)`
  - `func BaiduAuthorizeURL(clientID, deviceID string) string`
  - `func AliyunAuthorizeURL(clientID string) string`
  - `func PublicView(provider, status, lastError string) map[string]string` 不返回任何密钥字段

- [ ] **Step 1: Write the failing test**

```go
package cloudacct

import (
	"strings"
	"testing"
	"time"
)

func TestSealOpen(t *testing.T) {
	in := Secrets{URL: "https://dav.example", Username: "u", Password: "secret-password", ExpiresAt: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)}
	blob, err := Seal("jwt", in)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(blob, "secret-password") {
		t.Fatal("blob leaked password")
	}
	out, err := Open("jwt", blob)
	if err != nil {
		t.Fatal(err)
	}
	if out.Password != "secret-password" || !out.ExpiresAt.Equal(in.ExpiresAt) {
		t.Fatalf("%+v", out)
	}
}

func TestAuthorizeURL(t *testing.T) {
	b := BaiduAuthorizeURL("appkey", "appid")
	if !strings.Contains(b, "client_id=appkey") || !strings.Contains(b, "device_id=appid") || !strings.Contains(b, "redirect_uri=oob") {
		t.Fatal(b)
	}
	a := AliyunAuthorizeURL("cid")
	if !strings.Contains(a, "https://openapi.alipan.com/oauth/authorize") || !strings.Contains(a, "client_id=cid") {
		t.Fatal(a)
	}
}

func TestPublicViewHidesSecrets(t *testing.T) {
	view := PublicView("webdav", "disconnected", "认证失败")
	raw := view["provider"] + view["status"] + view["lastError"]
	if strings.Contains(raw, "password") || view["provider"] != "webdav" {
		t.Fatal(view)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cloudacct/ -count=1`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```go
package cloudacct

import (
	"encoding/json"
	"net/url"
	"time"

	"BaiduYunPanBak/internal/crypto"
)

type Secrets struct {
	ClientID     string    `json:"client_id,omitempty"`
	ClientSecret string    `json:"client_secret,omitempty"`
	AccessToken  string    `json:"access_token,omitempty"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	DeviceID     string    `json:"device_id,omitempty"`
	URL          string    `json:"url,omitempty"`
	Username     string    `json:"username,omitempty"`
	Password     string    `json:"password,omitempty"`
}

func Seal(jwtSecret string, s Secrets) (string, error) {
	raw, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return crypto.Encrypt(jwtSecret, string(raw))
}

func Open(jwtSecret, blob string) (Secrets, error) {
	plain, err := crypto.Decrypt(jwtSecret, blob)
	if err != nil {
		return Secrets{}, err
	}
	var s Secrets
	err = json.Unmarshal([]byte(plain), &s)
	return s, err
}

func BaiduAuthorizeURL(clientID, deviceID string) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "oob")
	q.Set("scope", "basic,netdisk")
	q.Set("device_id", deviceID)
	return "https://openapi.baidu.com/oauth/2.0/authorize?" + q.Encode()
}

func AliyunAuthorizeURL(clientID string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", "oob")
	q.Set("scope", "user:base,file:all:read,file:all:write")
	q.Set("response_type", "code")
	return "https://openapi.alipan.com/oauth/authorize?" + q.Encode()
}

func PublicView(provider, status, lastError string) map[string]string {
	return map[string]string{"provider": provider, "status": status, "lastError": lastError}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cloudacct/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cloudacct
git commit -m "feat: seal cloud credentials and build authorize URLs"
```

---

### Task 6: 备份运行器

**Files:**
- Create: `internal/backup/runner.go`
- Test: `internal/backup/runner_test.go`

**Interfaces:**
- Consumes: `provider.Provider`
- Produces:
  - `type Task struct { ID, CloudAccountID uint; Name, LocalDir, RemoteDir, ScheduleTime string; Enabled bool; LastScheduledOn *time.Time }`
  - `type Run struct { ID, TaskID uint; Trigger, Status, ErrorSummary string; StartedAt time.Time; FinishedAt *time.Time; SuccessCount, SkipCount, FailCount int }`
  - `type FileResult struct { RelativePath, Status, Error, SourceFingerprint string; Size int64; ModTime time.Time }`
  - `var ErrBusy = errors.New("该任务正在备份")`
  - `func Fingerprint(localDir string, accountID uint, remoteDir string) string`
  - `func FinalStatus(success, skip, fail int) string`
  - `type Store interface { HasRunning(ctx context.Context, taskID uint) (bool, error); CreateRun(ctx context.Context, run *Run) error; FinishRun(ctx context.Context, run Run) error; LatestSuccess(ctx context.Context, taskID uint, rel, fingerprint string) (FileResult, bool, error); SaveFileResult(ctx context.Context, runID, taskID uint, result FileResult) error }`
  - `func RunTask(ctx context.Context, store Store, task Task, p provider.Provider, trigger string, now time.Time) (Run, error)`

- [ ] **Step 1: Write the failing test**

```go
package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeProvider struct{ uploads []string; fail string }

func (f *fakeProvider) Test(ctx context.Context) error { return nil }
func (f *fakeProvider) EnsureDir(ctx context.Context, remoteDir string) error { return nil }
func (f *fakeProvider) Upload(ctx context.Context, localPath, remotePath string) error {
	if filepath.Base(localPath) == f.fail {
		return errors.New("网盘拒绝")
	}
	f.uploads = append(f.uploads, remotePath)
	return nil
}

type memStore struct {
	running bool
	runs    []Run
	files   []FileResult
	marked  int
}

func (m *memStore) HasRunning(ctx context.Context, taskID uint) (bool, error) { return m.running, nil }
func (m *memStore) CreateRun(ctx context.Context, run *Run) error {
	run.ID = uint(len(m.runs) + 1)
	m.runs = append(m.runs, *run)
	m.running = true
	return nil
}
func (m *memStore) FinishRun(ctx context.Context, run Run) error {
	m.running = false
	m.runs[len(m.runs)-1] = run
	return nil
}
func (m *memStore) LatestSuccess(ctx context.Context, taskID uint, rel, fingerprint string) (FileResult, bool, error) {
	for i := len(m.files) - 1; i >= 0; i-- {
		f := m.files[i]
		if f.RelativePath == rel && f.Status == "success" && f.SourceFingerprint == fingerprint {
			return f, true, nil
		}
	}
	return FileResult{}, false, nil
}
func (m *memStore) SaveFileResult(ctx context.Context, runID, taskID uint, result FileResult) error {
	m.files = append(m.files, result)
	return nil
}

func TestSkipPartialAndBusy(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.Local)
	old := now.Add(-time.Hour)
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(dir, "keep.txt"), old, old); err != nil {
		t.Fatal(err)
	}
	task := Task{ID: 1, CloudAccountID: 2, LocalDir: dir, RemoteDir: "/bak"}
	fp := Fingerprint(dir, 2, "/bak")
	info, _ := os.Stat(filepath.Join(dir, "keep.txt"))
	store := &memStore{files: []FileResult{{
		RelativePath: "keep.txt", Status: "success", Size: 1,
		ModTime: info.ModTime().UTC().Truncate(time.Second), SourceFingerprint: fp,
	}}}
	p := &fakeProvider{fail: "new.txt"}
	run, err := RunTask(context.Background(), store, task, p, "manual", now)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "partial" || run.SkipCount != 1 || run.FailCount != 1 || run.SuccessCount != 0 {
		t.Fatalf("%+v", run)
	}
	store.running = true
	if _, err := RunTask(context.Background(), store, task, p, "manual", now); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
	changed := Fingerprint(dir, 9, "/bak")
	if changed == fp {
		t.Fatal("fingerprint ignored account")
	}
}

func TestAllFailedAndEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &memStore{}
	run, err := RunTask(context.Background(), store, Task{ID: 1, LocalDir: dir, RemoteDir: "/bak"}, &fakeProvider{fail: "x.txt"}, "manual", time.Now())
	if err != nil || run.Status != "failed" {
		t.Fatalf("%+v %v", run, err)
	}
	empty := t.TempDir()
	store = &memStore{}
	run, err = RunTask(context.Background(), store, Task{ID: 1, LocalDir: empty, RemoteDir: "/bak"}, &fakeProvider{}, "manual", time.Now())
	if err != nil || run.Status != "success" || run.SuccessCount != 0 {
		t.Fatalf("%+v %v", run, err)
	}
}

func TestMissingDir(t *testing.T) {
	store := &memStore{}
	run, err := RunTask(context.Background(), store, Task{ID: 1, LocalDir: filepath.Join(t.TempDir(), "missing"), RemoteDir: "/bak"}, &fakeProvider{}, "manual", time.Now())
	if err != nil || run.Status != "failed" || run.ErrorSummary == "" {
		t.Fatalf("%+v %v", run, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -count=1 -run 'TestSkipPartialAndBusy|TestAllFailedAndEmpty|TestMissingDir'`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```go
package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"BaiduYunPanBak/internal/provider"
)

var ErrBusy = errors.New("该任务正在备份")

type Task struct {
	ID               uint
	CloudAccountID   uint
	Name             string
	LocalDir         string
	RemoteDir        string
	ScheduleTime     string
	Enabled          bool
	LastScheduledOn  *time.Time
}

type Run struct {
	ID            uint
	TaskID        uint
	Trigger       string
	Status        string
	ErrorSummary  string
	StartedAt     time.Time
	FinishedAt    *time.Time
	SuccessCount  int
	SkipCount     int
	FailCount     int
}

type FileResult struct {
	RelativePath      string
	Status            string
	Error             string
	SourceFingerprint string
	Size              int64
	ModTime           time.Time
}

type Store interface {
	HasRunning(ctx context.Context, taskID uint) (bool, error)
	CreateRun(ctx context.Context, run *Run) error
	FinishRun(ctx context.Context, run Run) error
	LatestSuccess(ctx context.Context, taskID uint, rel, fingerprint string) (FileResult, bool, error)
	SaveFileResult(ctx context.Context, runID, taskID uint, result FileResult) error
}

func Fingerprint(localDir string, accountID uint, remoteDir string) string {
	sum := sha256.Sum256([]byte(localDir + "\n" + strconv.FormatUint(uint64(accountID), 10) + "\n" + remoteDir))
	return hex.EncodeToString(sum[:])
}

func FinalStatus(success, skip, fail int) string {
	if fail == 0 {
		return "success"
	}
	if success == 0 && skip == 0 {
		return "failed"
	}
	return "partial"
}

func RunTask(ctx context.Context, store Store, task Task, p provider.Provider, trigger string, now time.Time) (Run, error) {
	busy, err := store.HasRunning(ctx, task.ID)
	if err != nil {
		return Run{}, err
	}
	if busy {
		return Run{}, ErrBusy
	}
	run := &Run{TaskID: task.ID, Trigger: trigger, Status: "running", StartedAt: now}
	if err := store.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	info, err := os.Stat(task.LocalDir)
	if err != nil || !info.IsDir() {
		return finishFailed(ctx, store, *run, now, "本地目录不存在或不可读")
	}
	fp := Fingerprint(task.LocalDir, task.CloudAccountID, task.RemoteDir)
	if err := p.EnsureDir(ctx, task.RemoteDir); err != nil {
		return finishFailed(ctx, store, *run, now, "无法准备网盘目录")
	}
	err = filepath.WalkDir(task.LocalDir, func(local string, d fs.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(task.LocalDir, local)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if walkErr != nil || d.IsDir() {
			if walkErr != nil {
				run.FailCount++
				_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "failed", Error: "无法读取", SourceFingerprint: fp})
			}
			return nil
		}
		st, statErr := d.Info()
		if statErr != nil {
			run.FailCount++
			_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "failed", Error: "无法读取", SourceFingerprint: fp})
			return nil
		}
		mod := st.ModTime().UTC().Truncate(time.Second)
		prev, ok, _ := store.LatestSuccess(ctx, task.ID, rel, fp)
		if ok && prev.Size == st.Size() && prev.ModTime.UTC().Truncate(time.Second).Equal(mod) {
			run.SkipCount++
			_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "skipped", Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
			return nil
		}
		remote := path.Join(task.RemoteDir, rel)
		if upErr := p.Upload(ctx, local, remote); upErr != nil {
			run.FailCount++
			_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "failed", Error: upErr.Error(), Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
			return nil
		}
		run.SuccessCount++
		_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "success", Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
		return nil
	})
	if err != nil {
		return finishFailed(ctx, store, *run, now, "扫描失败")
	}
	done := now
	run.FinishedAt = &done
	run.Status = FinalStatus(run.SuccessCount, run.SkipCount, run.FailCount)
	if err := store.FinishRun(ctx, *run); err != nil {
		return Run{}, err
	}
	return *run, nil
}

func finishFailed(ctx context.Context, store Store, run Run, now time.Time, summary string) (Run, error) {
	done := now
	run.FinishedAt = &done
	run.Status = "failed"
	run.ErrorSummary = summary
	if err := store.FinishRun(ctx, run); err != nil {
		return Run{}, err
	}
	return run, nil
}

```

删除最后的 `fmt` 占位。`RemoteJoin` 可保留给测试使用，去掉 `fmt` import 和 `var _ = fmt.Sprintf`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backup/runner.go internal/backup/runner_test.go
git commit -m "feat: run one-way backups with skip and partial failure"
```

---

### Task 7: 定时判断与启动清理

**Files:**
- Create: `internal/backup/schedule.go`
- Test: `internal/backup/schedule_test.go`

**Interfaces:**
- Consumes: `Task`、`Run`、`Store`
- Produces:
  - `func ShouldSchedule(now time.Time, task Task, running bool) bool`
  - `type ScheduleStore interface { ListEnabled(ctx context.Context) ([]Task, error); HasRunning(ctx context.Context, taskID uint) (bool, error); MarkScheduled(ctx context.Context, taskID uint, day time.Time) error; FailStaleRuns(ctx context.Context) error }`
  - `func Tick(ctx context.Context, st ScheduleStore, run func(ctx context.Context, task Task) error, now time.Time) error`
  - `ErrBusy` 时 `Tick` 不调用 `MarkScheduled`

- [ ] **Step 1: Write the failing test**

```go
package backup

import (
	"context"
	"testing"
	"time"
)

func TestShouldSchedule(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, loc)
	task := Task{Enabled: true, ScheduleTime: "09:30"}
	if ShouldSchedule(now, task, false) {
		t.Fatal("too early")
	}
	now = time.Date(2026, 10, 12, 9, 30, 0, 0, loc)
	if !ShouldSchedule(now, task, false) {
		t.Fatal("due")
	}
	if ShouldSchedule(now, task, true) {
		t.Fatal("running")
	}
	today := now
	task.LastScheduledOn = &today
	if ShouldSchedule(now, task, false) {
		t.Fatal("already today")
	}
}

type schedMem struct {
	tasks   []Task
	running map[uint]bool
	marked  []uint
	stale   int
}

func (s *schedMem) ListEnabled(ctx context.Context) ([]Task, error) { return s.tasks, nil }
func (s *schedMem) HasRunning(ctx context.Context, taskID uint) (bool, error) {
	return s.running[taskID], nil
}
func (s *schedMem) MarkScheduled(ctx context.Context, taskID uint, day time.Time) error {
	s.marked = append(s.marked, taskID)
	return nil
}
func (s *schedMem) FailStaleRuns(ctx context.Context) error { s.stale++; return nil }

func TestTickSkipsBusyAndManualDoesNotMark(t *testing.T) {
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.Local)
	st := &schedMem{tasks: []Task{{ID: 1, Enabled: true, ScheduleTime: "09:00"}, {ID: 2, Enabled: true, ScheduleTime: "09:00"}}, running: map[uint]bool{1: true}}
	err := Tick(context.Background(), st, func(ctx context.Context, task Task) error {
		if task.ID == 1 {
			return ErrBusy
		}
		return nil
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.marked) != 1 || st.marked[0] != 2 {
		t.Fatalf("marked %v", st.marked)
	}
}

func TestFailStaleMessage(t *testing.T) {
	if FailStaleMessage() != "进程已退出" {
		t.Fatal(FailStaleMessage())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/backup/ -count=1 -run 'TestShouldSchedule|TestTick|TestFailStaleMessage'`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

```go
package backup

import (
	"context"
	"errors"
	"time"
)

type ScheduleStore interface {
	ListEnabled(ctx context.Context) ([]Task, error)
	HasRunning(ctx context.Context, taskID uint) (bool, error)
	MarkScheduled(ctx context.Context, taskID uint, day time.Time) error
	FailStaleRuns(ctx context.Context) error
}

func FailStaleMessage() string { return "进程已退出" }

func ShouldSchedule(now time.Time, task Task, running bool) bool {
	if !task.Enabled || running {
		return false
	}
	clock, err := time.ParseInLocation("15:04", task.ScheduleTime, now.Location())
	if err != nil {
		return false
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if now.Before(due) {
		return false
	}
	if task.LastScheduledOn != nil {
		ly, lm, ld := task.LastScheduledOn.In(now.Location()).Date()
		ny, nm, nd := now.Date()
		if ly == ny && lm == nm && ld == nd {
			return false
		}
	}
	return true
}

func Tick(ctx context.Context, st ScheduleStore, run func(ctx context.Context, task Task) error, now time.Time) error {
	tasks, err := st.ListEnabled(ctx)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		running, err := st.HasRunning(ctx, task.ID)
		if err != nil {
			return err
		}
		if !ShouldSchedule(now, task, running) {
			continue
		}
		if err := st.MarkScheduled(ctx, task.ID, now); err != nil {
			return err
		}
		if err := run(ctx, task); err != nil && !errors.Is(err, ErrBusy) {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/backup/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/backup/schedule.go internal/backup/schedule_test.go
git commit -m "feat: decide daily backup schedule without consuming a busy day"
```

---

### Task 8: MySQL 模型与 GORM 存储

**Files:**
- Create: `internal/store/models.go`
- Create: `internal/store/gorm_store.go`
- Test: `internal/store/gorm_store_test.go`
- Modify: `utils/conf/conf.go`
- Modify: `global/global.go`

**Interfaces:**
- Consumes: `auth.User`、`backup.Task`、`backup.Run`、`backup.FileResult`、`backup.Store`、`backup.ScheduleStore`、`auth.UserStore`、`auth.SessionStore`
- Produces:
  - `func AutoMigrate(db *gorm.DB) error`
  - `type GormStore struct`
  - `func NewGorm(db *gorm.DB) *GormStore` 实现 `auth.UserStore`、`backup.Store`、`backup.ScheduleStore`
  - `type RedisSessions struct` 实现 `auth.SessionStore`，键为 `session:<id>`
  - `global.JwtSecret string`、`global.JwtExpire int`
  - `FailStaleRuns` 把 `running` 更新为 `failed`，`error_summary` 为 `backup.FailStaleMessage()`
  - `DeleteTask` 只删除 `backup_tasks` 行

- [ ] **Step 1: Write the failing test**

使用纯 Go SQLite：`go get gorm.io/gorm github.com/glebarez/sqlite`。测试打开 `file::memory:?cache=shared`。

```go
package store

import (
	"context"
	"testing"
	"time"

	"BaiduYunPanBak/internal/auth"
	"BaiduYunPanBak/internal/backup"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSetupTransactionAndStaleRuns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	s := NewGorm(db)
	ctx := context.Background()
	if _, err := auth.Setup(ctx, s, "me", "password1", "password1"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Setup(ctx, s, "me", "password1", "password1"); err != auth.ErrUserExists {
		t.Fatalf("got %v", err)
	}
	task := backup.Task{Name: "t", LocalDir: "/tmp", RemoteDir: "/bak", ScheduleTime: "09:00", Enabled: true, CloudAccountID: 1}
	if err := s.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	run := &backup.Run{TaskID: task.ID, Trigger: "manual", Status: "running", StartedAt: time.Now()}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := s.FailStaleRuns(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, run.ID)
	if err != nil || got.Status != "failed" || got.ErrorSummary != "进程已退出" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRun(ctx, run.ID); err != nil {
		t.Fatal("run deleted with task")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -count=1`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

`global/global.go` 增加：

```go
var JwtSecret string
var JwtExpire int
```

`utils/conf/conf.go` 的 `InitConfig` 末尾增加：

```go
global.JwtSecret = viper.GetString("jwt.secret")
global.JwtExpire = viper.GetInt("jwt.expire")
if global.JwtExpire <= 0 {
    global.JwtExpire = 3
}
```

`internal/store/models.go` 定义表 `users`、`cloud_accounts`、`backup_tasks`、`backup_runs`、`backup_file_results`，字段与规格一致。`size` 使用 `int64`。`users.username` 唯一。`cloud_accounts.provider` 唯一。时间列继续使用 `model.BaseModel` 的 `CreatedAt`、`UpdatedAt`。

`internal/store/gorm_store.go`：

- `Count` 使用 `db.Transaction`，`SELECT COUNT(*) FROM users`。
- `Create` 在同一事务里再次 `Count`，大于 0 返回 `auth.ErrUserExists`。`auth.Setup` 已经先 `Count` 再 `Create`；GORM `Create` 必须再次在事务中检查，满足「设置接口在事务中统计行数」。在 `Create` 内实现该事务。
- `CreateRun`、`FinishRun`、`HasRunning`、`LatestSuccess`（`status = success` 且 fingerprint、relative_path、task_id 匹配，按 id 降序取一条）、`SaveFileResult`。
- `ListEnabled` 查询 `enabled = true`。
- `MarkScheduled` 更新 `last_scheduled_on`。
- `FailStaleRuns`：`WHERE status = 'running'` 更新为 `failed` 和 `进程已退出`。
- `DeleteTask` 只 `Delete` 任务。
- `CreateTask`、`GetRun`、`ListRuns`、`ListTasks`、`UpsertCloud`、`ListClouds`、`GetCloud` 供后续 HTTP 使用。

`internal/store/redis_session.go`：

```go
func (r RedisSessions) Set(ctx context.Context, id string, userID uint, ttl time.Duration) error {
    return global.RedisClient.Set(ctx, "session:"+id, strconv.FormatUint(uint64(userID), 10), ttl).Err()
}
```

`Get` 读取并 `strconv.Atoi`。`Delete` 删除该键。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ ./internal/auth/ ./internal/backup/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store global/global.go utils/conf/conf.go go.mod go.sum
git commit -m "feat: persist users, clouds, tasks, and backup runs"
```

---

### Task 9: 百度适配器

**Files:**
- Create: `internal/provider/baidu/upload.go`
- Test: `internal/provider/baidu/upload_test.go`
- Modify: `cmd/upload.go`

**Interfaces:**
- Consumes: `provider.Provider`
- Produces:
  - `type Uploader interface { Precreate(ctx context.Context, token, remote string, size int64, blocks []string) (string, error); UploadPart(ctx context.Context, token, remote, uploadID string, seq int, chunkPath string) error; Create(ctx context.Context, token, remote, uploadID string, size int64, blocks []string) (fsID, md5 string, errno int, err error); Mkdir(ctx context.Context, token, remote string) error }`
  - `func New(token string, up Uploader, chunkDir string, chunkSize int) *Client`
  - `Client` 实现 `provider.Provider`
  - `func SaveHistory(size int64, remote, fsID, md5 string, errno int)` 写入现有 `model.UploadHistory`。`Size` 超出 int32 时记为 0，并仍保存 `fs_id`。这是现有 int32 列的边界，新的跳过逻辑不读这张表。

- [ ] **Step 1: Write the failing test**

```go
package baidu

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

type fakeUp struct {
	parts []string
	size  int64
}

func (f *fakeUp) Precreate(ctx context.Context, token, remote string, size int64, blocks []string) (string, error) {
	f.size = size
	return "upid", nil
}
func (f *fakeUp) UploadPart(ctx context.Context, token, remote, uploadID string, seq int, chunkPath string) error {
	b, _ := os.ReadFile(chunkPath)
	f.parts = append(f.parts, string(b))
	return nil
}
func (f *fakeUp) Create(ctx context.Context, token, remote, uploadID string, size int64, blocks []string) (string, string, int, error) {
	return "1", "abc", 0, nil
}
func (f *fakeUp) Mkdir(ctx context.Context, token, remote string) error { return nil }

func TestChunkUpload(t *testing.T) {
	dir := t.TempDir()
	local := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(local, []byte("abcde"), 0o644); err != nil {
		t.Fatal(err)
	}
	up := &fakeUp{}
	c := New("token", up, filepath.Join(dir, "chunks"), 2)
	if err := c.Upload(context.Background(), local, "/bak/f.bin"); err != nil {
		t.Fatal(err)
	}
	if up.size != 5 || len(up.parts) != 3 || up.parts[0] != "ab" || up.parts[2] != "e" {
		t.Fatalf("%+v", up)
	}
	if _, err := os.Stat(filepath.Join(dir, "chunks")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(dir, "chunks"))
		if len(entries) != 0 {
			t.Fatal("chunks left behind")
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/baidu/ -count=1`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

`internal/provider/baidu/upload.go`：

```go
func (c *Client) Upload(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(c.chunkDir, 0o755); err != nil {
		return err
	}
	buf := make([]byte, c.chunkSize)
	var blocks []string
	var parts []string
	for seq := 0; ; seq++ {
		n, readErr := io.ReadFull(f, buf)
		if readErr == io.EOF {
			break
		}
		if readErr != nil && readErr != io.ErrUnexpectedEOF {
			return readErr
		}
		chunkPath := filepath.Join(c.chunkDir, fmt.Sprintf("chunk%d.dat", seq))
		if err := os.WriteFile(chunkPath, buf[:n], 0o644); err != nil {
			return err
		}
		parts = append(parts, chunkPath)
		sum := md5.Sum(buf[:n])
		blocks = append(blocks, hex.EncodeToString(sum[:]))
		if readErr == io.ErrUnexpectedEOF {
			break
		}
	}
	defer func() {
		for _, p := range parts {
			_ = os.Remove(p)
		}
	}()
	uploadID, err := c.up.Precreate(ctx, c.token, remotePath, info.Size(), blocks)
	if err != nil {
		return err
	}
	for i, p := range parts {
		if err := c.up.UploadPart(ctx, c.token, remotePath, uploadID, i, p); err != nil {
			return err
		}
	}
	_, _, _, err = c.up.Create(ctx, c.token, remotePath, uploadID, info.Size(), blocks)
	return err
}
```

`Test` 在 `c.token == ""` 时返回 `fmt.Errorf("未连接")`。`EnsureDir` 调用 `c.up.Mkdir`。`New` 保存 token、uploader、chunkDir、chunkSize。

`cmd/upload.go` 只接收本地文件绝对路径。远程路径为 `/bak/` + `time.Now().Format("2006-01-02")` + `/` + 文件名。access token 先读 `cache.Get("access_token")`，为空再读 `model.Token.Find()`。分片目录固定为 `runtime/chunks`。真实上传器在 `internal/provider/baidu/openapi_uploader.go`，调用现有 `tool/openapi` 的 `Xpanfileprecreate`、`Pcssuperfile2`、`Xpanfilecreate`。创建目录时 `Isdir(1)`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/baidu/ ./internal/backup/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provider/baidu cmd/upload.go
git commit -m "feat: move Baidu chunk upload behind the provider interface"
```

---

### Task 10: 阿里云盘适配器

**Files:**
- Create: `internal/provider/aliyun/client.go`
- Test: `internal/provider/aliyun/client_test.go`

**Interfaces:**
- Consumes: `provider.Provider`
- Produces:
  - `type Token struct { AccessToken, RefreshToken string; ExpiresAt time.Time }`
  - `func ExchangeCode(ctx context.Context, base, clientID, clientSecret, code string) (Token, error)`
  - `func Refresh(ctx context.Context, base, clientID, clientSecret, refreshToken string) (Token, error)`
  - `func New(base, accessToken string, httpClient *http.Client) *Client`
  - `Client` 实现 `provider.Provider`
  - 端点固定为：
    - `POST {base}/oauth/access_token`
    - `POST {base}/adrive/v1.0/user/getDriveInfo`
    - `POST {base}/adrive/v1.0/openFile/list`
    - `POST {base}/adrive/v1.0/openFile/create`
    - 上传 PUT 使用 create 响应里的 `part_info_list[0].upload_url`

- [ ] **Step 1: Write the failing test**

用 `httptest.Server` 作为 `base`。`getDriveInfo` 返回 `{"default_drive_id":"d1"}`。`list` 对空目录返回 `{"items":[]}`。`create` 文件夹返回 `{"file_id":"folder1","type":"folder"}`，文件返回 `{"file_id":"file1","part_info_list":[{"upload_url":"<server>/put"}]}`。PUT 保存正文。断言 `Test` 成功、`EnsureDir("/bak")` 发出文件夹 create、`Upload` 的 PUT 正文等于文件内容。401 时错误文本为 `授权失败`，且不包含 access token。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/provider/aliyun/ -count=1`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

`ExchangeCode` POST JSON：`grant_type=authorization_code`、`client_id`、`client_secret`、`code`。`Refresh` 使用 `grant_type=refresh_token`。响应读取 `access_token`、`refresh_token`、`expires_in`，`ExpiresAt = now + expires_in 秒`。

`Test` 调用 `getDriveInfo`，Bearer 头为 access token。`EnsureDir` 从 `root` 开始按 `/` 拆分路径，list 查找同名 folder，没有则 create `type=folder`。`Upload` 先定位父目录 file_id，再 create `type=file`，然后 PUT 本地文件到 `upload_url`。HTTP 非 2xx 返回 `授权失败` 或 `上传失败`，错误字符串不拼接 token。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/provider/aliyun/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/provider/aliyun
git commit -m "feat: add Aliyun Drive official API provider"
```

---

### Task 11: 网盘、任务和运行记录接口

**Files:**
- Create: `internal/server/api.go`
- Create: `internal/server/api_test.go`
- Modify: `internal/server/auth_http.go` 把受保护路由并入同一个 mux，或由 `NewMux` 挂载 `NewAuthMux` 的公开路由并继续注册受保护路由。最终只有一个 `NewMux(Deps) http.Handler`。

**Interfaces:**
- Consumes: Task 3 到 Task 10 的类型
- Produces:
  - `GET /api/overview`
  - `GET /api/clouds`、`PUT /api/clouds/{provider}`、`POST /api/clouds/{provider}/test`、`POST /api/clouds/{provider}/authorize`
  - `GET/POST /api/tasks`、`PUT/DELETE /api/tasks/{id}`、`POST /api/tasks/{id}/run`
  - `GET /api/runs`、`GET /api/runs/{id}`
  - 保存任务时本地目录不存在返回 400，JSON `{"fields":{"localDir":"本地目录不存在或不可读"}}`
  - `remoteDir` 不以 `/` 开头返回 400
  - 立即运行遇到 `backup.ErrBusy` 返回 409，正文 `该任务正在备份`
  - 列表响应不包含 `secret_blob`、密码或 token

- [ ] **Step 1: Write the failing test**

使用 Task 3 的内存用户和会话。云和任务用内存 `APIStore`。流程：setup、带 cookie 调用 `PUT /api/clouds/webdav`（url、username、password）、`GET /api/clouds` 的 JSON 不包含 password、`POST /api/tasks` 指向临时目录、再次 `POST /api/tasks/{id}/run` 在 store 已有 running 时返回 409、无 cookie 的 `GET /api/tasks` 返回 401。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -count=1 -run TestCloudTaskAPI`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

`PUT` 用 `cloudacct.Seal(global.JwtSecret, secrets)` 保存。`test` 按 provider 构造 WebDAV、百度或阿里云盘客户端并调用 `Test`，把可展示错误写入 `last_error`。`authorize` 对百度和阿里云盘调用对应的 `ExchangeCode` 或现有百度 `OauthTokenCode2token`，成功后更新加密密钥、`tokens` 表和 Redis `access_token`。令牌距到期不足 5 分钟时，`run` 之前先刷新；刷新失败把本次运行记为 `failed` 并返回 200，运行状态为 `failed`（失败记在运行记录里，不是 500）。

`POST /api/tasks/{id}/run` 同步调用 `backup.RunTask`。触发值 `manual`。

`GET /api/overview` 返回：

```json
{"clouds":[{"provider":"baidu","status":"disconnected","lastError":""}],"taskCount":0,"recentRuns":[]}
```

三种 provider 都要出现，没有记录时 status 为 `disconnected`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ ./internal/backup/ ./internal/provider/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server
git commit -m "feat: expose cloud, task, and run APIs"
```

---

### Task 12: serve 命令

**Files:**
- Create: `cmd/serve.go`
- Create: `internal/server/static.go`
- Create: `web/dist/index.html`
- Modify: `bootstrap/init.go` 在初始化数据库后调用 `store.AutoMigrate`

**Interfaces:**
- Consumes: `server.NewMux`、`server.ValidateAddr`、`backup.Tick`、`store.NewGorm`、`store.RedisSessions`
- Produces: `serve [--addr 127.0.0.1:8080] [--dev]`

- [ ] **Step 1: Write the failing test**

`cmd` 包测试不适合启动整个 bootstrap。把监听前的地址检查留在 `server.ValidateAddr`（Task 3 已测）。本任务为静态文件增加：

`internal/server/static_test.go` 请求 `/` 返回嵌入的 `index.html` 正文。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -count=1 -run TestEmbeddedIndex`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

占位页放在 `internal/server/dist/index.html`，内容为 `<!doctype html><title>备份面板</title>`。前端构建后再把 `web/dist` 复制覆盖到这个目录。`internal/server/static.go`：

```go
package server

import "embed"

//go:embed all:dist
var distFS embed.FS
```

`--dev` 时对非 `/api` 使用 `httputil.NewSingleHostReverseProxy` 指向 `http://127.0.0.1:5173`。

`cmd/serve.go`：

- 默认 addr `127.0.0.1:8080`
- `ValidateAddr` 失败则 `os.Exit(1)`
- `store.NewGorm(global.DataBase).FailStaleRuns`
- 会话 TTL 为 `time.Duration(global.JwtExpire) * time.Hour`
- `time.NewTicker(30 * time.Second)` 调用 `backup.Tick`
- `http.ListenAndServe(addr, mux)`

`bootstrap.Init` 末尾：

```go
if err := store.AutoMigrate(global.DataBase); err != nil {
    panic(err)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ ./internal/store/ ./internal/backup/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/serve.go internal/server bootstrap/init.go
git commit -m "feat: serve the local backup panel on loopback"
```

---

### Task 13: React 面板

**Files:**
- Create: `web/package.json`
- Create: `web/vite.config.js`
- Create: `web/index.html`
- Create: `web/src/main.jsx`
- Create: `web/src/App.jsx`
- Create: `web/src/api.js`
- Create: `web/src/styles.css`
- Modify: `README.md`

**Interfaces:**
- Consumes: Task 11 的 JSON 字段名：`needsSetup`、`username`、`password`、`confirm`、`clouds[].provider/status/lastError`、`taskCount`、`recentRuns`、`localDir`、`remoteDir`、`scheduleTime`、`enabled`、`fields`
- Produces: 首次设置、登录、概览、网盘、备份任务、运行记录、退出

- [ ] **Step 1: Write the failing test**

前端没有组件测试运行器。用 Go 测试锁住页面文案不合适。本任务的检查命令是：

Run: `cd web && npm install && npm run build`
Expected: 生成 `web/dist/index.html` 和资源文件，退出码 0

构建后执行 `cp -R web/dist/. internal/server/dist/`，再 `go test ./internal/server/ -count=1 -run TestEmbeddedIndex`。

- [ ] **Step 2: Run build to verify it fails**

Run: `cd web && npm run build`
Expected: FAIL，`web/package.json` 不存在

- [ ] **Step 3: Write the UI**

`package.json` 依赖 `react`、`react-dom`，开发依赖 `vite`、`@vitejs/plugin-react`。脚本：`"dev": "vite"`、`"build": "vite build"`。

`vite.config.js` 将端口设为 5173，并把 `/api` 代理到 `http://127.0.0.1:8080`。

`api.js` 的 `request(path, options)` 使用 `credentials: "include"` 和 `Content-Type: application/json`。

`App.jsx` 在启动时请求 `GET /api/setup/status`。`needsSetup` 为真时只渲染设置表单（用户名、密码、确认密码，每个都有 `<label>`）。否则未登录渲染登录表单。登录后左侧导航为：概览、网盘、备份任务、运行记录，底部按钮「退出登录」。

样式：侧栏背景 `#1f2933`，文字 `#f5f7fa`；内容区背景 `#f7f6f3`，文字 `#1f2933`；正文字号 16px；焦点使用 `outline: 3px solid #0f6f6a`；宽度小于 768px 时侧栏改为顶部横排，容器 `max-width: 100%`，`overflow-x: hidden`。状态文字固定为：成功、部分成功、失败、进行中、跳过。颜色可以同时使用，但不能只有颜色。图标用内联 SVG。

概览每 3 秒在存在 `status === "running"` 的记录时调用 `GET /api/overview`。运行详情同样每 3 秒调用 `GET /api/runs/{id}`。

网盘页三张卡片。WebDAV 提交 url、username、password 到 `PUT /api/clouds/webdav`。百度和阿里云盘提交 client id、client secret、device id（仅百度），显示 `authorizeUrl`，并有授权码输入框提交 `POST /api/clouds/{provider}/authorize`。每张卡片有「测试连接」，结果放在该卡片内。

任务表单字段：名称、本地目录、网盘 provider、网盘目录、时刻、启用。保存失败时把 `fields.localDir` 显示在目录输入框下。列表按钮：立即备份、暂停定时（`enabled: false`）、删除。409 时在该行显示「该任务正在备份」。

- [ ] **Step 4: 构建并复制嵌入文件**

Run: `cd web && npm install && npm run build && cp -R dist/. ../internal/server/dist/ && cd .. && go test ./... -count=1`
Expected: PASS。现有包没有测试时也必须通过编译。

- [ ] **Step 5: 更新 README 并提交**

README 说明：

```text
go run main.go serve
浏览器打开 http://127.0.0.1:8080
首次打开设置本地账号
go run main.go upload 文件绝对路径
```

开发时另开 `cd web && npm run dev`，Go 使用 `go run main.go serve --dev`。

```bash
git add web internal/server/dist README.md
git commit -m "feat: add the local backup panel UI"
```

---

### Task 14: 命令行与面板共用百度令牌

**Files:**
- Modify: `cmd/flash_token.go`
- Modify: `cmd/reflash_token.go`
- Create: `internal/baidutoken/save.go`
- Test: `internal/baidutoken/save_test.go`

**Interfaces:**
- Consumes: `model.Token`、`cache.Set`、`cloudacct.Seal`
- Produces: `func Save(access, refresh string, expiresIn int, accountSaver func(blob string) error) error`  
  同时写 `model.Token`、Redis `access_token`。`accountSaver` 非空时保存加密后的百度密钥。

- [ ] **Step 1: Write the failing test**

用假的 saver 断言收到的 blob 能被 `cloudacct.Open` 解出 access token，且函数把 expiresIn 传给一个可替换的 `cacheSetter`。不要连接真实 Redis。把 `cache.Set` 抽成函数变量以便测试替换。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/baidutoken/ -count=1`
Expected: FAIL

- [ ] **Step 3: Write minimal implementation**

`flashToken` 和 `reflashToken` 在拿到开放平台响应后调用 `baidutoken.Save`。面板 `authorize` 也调用它。`upload` 仍先读 Redis `access_token`，没有再读 `model.Token`。

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/baidutoken/ ./internal/server/ ./internal/provider/... -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/baidutoken cmd/flash_token.go cmd/reflash_token.go internal/server
git commit -m "feat: share Baidu tokens between the CLI and the panel"
```
