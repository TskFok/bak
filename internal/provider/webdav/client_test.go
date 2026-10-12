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
