package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const placeholderIndex = "<!doctype html><title>备份面板</title>"

func TestEmbeddedIndex(t *testing.T) {
	h := NewStaticHandler()
	for _, path := range []string{"/", "/index.html", "/tasks"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
		if got := rec.Body.String(); got != placeholderIndex {
			t.Fatalf("%s: body = %q", path, got)
		}
	}
}

func TestFrontendRoutesAPIAndStatic(t *testing.T) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("api")) })
	front := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("front")) })
	h := WithFrontend(api, front)
	cases := map[string]string{"/api/overview": "api", "/api": "api", "/": "front", "/apix": "front", "/tasks": "front"}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Body.String() != want {
			t.Fatalf("%s: got %q want %q", path, rec.Body.String(), want)
		}
	}
}

func TestDevProxyForwards(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("vite " + r.URL.Path)) }))
	defer up.Close()
	h, err := NewDevProxy(up.URL)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/src/main.ts", nil))
	if rec.Body.String() != "vite /src/main.ts" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
