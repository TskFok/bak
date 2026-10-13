package server

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

var assetRe = regexp.MustCompile(`/assets/[A-Za-z0-9._-]+`)

func TestEmbeddedIndex(t *testing.T) {
	h := NewStaticHandler()
	var first string
	for _, path := range []string{"/", "/index.html", "/tasks"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", path, rec.Code)
		}
		body := rec.Body.String()
		if !strings.Contains(body, `<div id="root">`) || !strings.Contains(body, "<title>bak</title>") {
			t.Fatalf("%s: body is not the panel page: %q", path, body)
		}
		if first == "" {
			first = body
		} else if body != first {
			t.Fatalf("%s: body differs from /", path)
		}
	}
}

func TestEmbeddedAssetsServed(t *testing.T) {
	h := NewStaticHandler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	refs := assetRe.FindAllString(rec.Body.String(), -1)
	if len(refs) == 0 {
		t.Fatalf("index.html references no /assets/ files: %q", rec.Body.String())
	}
	for _, ref := range refs {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, ref, nil))
		if r.Code != http.StatusOK || r.Body.Len() == 0 {
			t.Fatalf("%s: status = %d, size = %d", ref, r.Code, r.Body.Len())
		}
		if strings.HasPrefix(r.Body.String(), "<!doctype html>") {
			t.Fatalf("%s: got index.html fallback instead of the asset", ref)
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
