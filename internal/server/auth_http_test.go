package server

import (
	"bytes"
	"context"
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

func TestLogoutRequiresSession(t *testing.T) {
	h := NewAuthMux(AuthDeps{Users: &memAuthUsers{}, Sessions: &memSessions{}, SessionTTL: time.Hour})
	setupBody := []byte(`{"username":"me","password":"password1","confirm":"password1"}`)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/setup", bytes.NewReader(setupBody)))
	if rec.Code != 200 {
		t.Fatalf("setup %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/logout", nil))
	if rec.Code != 401 {
		t.Fatalf("logout without cookie %d", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader([]byte(`{"username":"me","password":"password1"}`))))
	if rec.Code != 200 {
		t.Fatalf("login %d", rec.Code)
	}
	sessionCookie := rec.Result().Cookies()[0]

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, logoutReq)
	if rec.Code != 204 {
		t.Fatalf("logout with session %d", rec.Code)
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	meReq.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, meReq)
	if rec.Code != 401 {
		t.Fatalf("me after logout %d", rec.Code)
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
