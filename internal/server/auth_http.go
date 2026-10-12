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
	registerAuthRoutes(r, deps)
	return r
}

// registerAuthRoutes 注册 setup、login、logout、me 路由，供 NewAuthMux 和 NewMux 共用。
func registerAuthRoutes(r chi.Router, deps AuthDeps) {
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
	r.Group(func(pr chi.Router) {
		pr.Use(requireSession(deps))
		pr.Post("/api/logout", func(w http.ResponseWriter, req *http.Request) {
			c, _ := req.Cookie("session")
			_ = deps.Sessions.Delete(req.Context(), c.Value)
			http.SetCookie(w, &http.Cookie{Name: "session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
			w.WriteHeader(http.StatusNoContent)
		})
		pr.Get("/api/me", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, map[string]uint{"userId": req.Context().Value(userIDKey).(uint)})
		})
	})
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
