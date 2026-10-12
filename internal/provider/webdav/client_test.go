package webdav

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

type reqLog struct {
	mu    sync.Mutex
	items []string
}

func (l *reqLog) add(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.items = append(l.items, s)
}

func (l *reqLog) count(prefix string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, s := range l.items {
		if strings.HasPrefix(s, prefix) {
			n++
		}
	}
	return n
}

func (l *reqLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.items...)
}

// nestedServer 模拟严格的 WebDAV：父集合不存在时 MKCOL/PUT 返回 409，已存在的 MKCOL 返回 405。
func nestedServer(t *testing.T, log *reqLog, puts map[string]string) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	dirs := map[string]bool{"": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.EscapedPath()
		log.add(r.Method + " " + p)
		parent := p[:strings.LastIndex(p, "/")]
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case "MKCOL":
			switch {
			case dirs[p]:
				w.WriteHeader(http.StatusMethodNotAllowed)
			case !dirs[parent]:
				w.WriteHeader(http.StatusConflict)
			default:
				dirs[p] = true
				w.WriteHeader(http.StatusCreated)
			}
		case http.MethodPut:
			if !dirs[parent] {
				w.WriteHeader(http.StatusConflict)
				return
			}
			if r.ContentLength != 5 {
				w.WriteHeader(http.StatusLengthRequired)
				return
			}
			b, _ := io.ReadAll(r.Body)
			puts[p] = string(b)
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEnsureDirCreatesParentsInOrder(t *testing.T) {
	log := &reqLog{}
	srv := nestedServer(t, log, map[string]string{})
	c := New(srv.URL, "u", "pw")
	if err := c.EnsureDir(context.Background(), "/backup/home"); err != nil {
		t.Fatal(err)
	}
	got := log.all()
	if len(got) != 2 || got[0] != "MKCOL /backup" || got[1] != "MKCOL /backup/home" {
		t.Fatalf("%v", got)
	}
}

func TestUploadNestedCreatesEachParentOnce(t *testing.T) {
	log := &reqLog{}
	puts := map[string]string{}
	srv := nestedServer(t, log, puts)
	c := New(srv.URL, "u", "pw")
	ctx := context.Background()
	if err := c.EnsureDir(ctx, "/remote"); err != nil {
		t.Fatal(err)
	}
	a := writeFile(t, "a.txt", "hello")
	b := writeFile(t, "b.txt", "world")
	if err := c.Upload(ctx, a, "/remote/sub/a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := c.Upload(ctx, b, "/remote/sub/b.txt"); err != nil {
		t.Fatal(err)
	}
	if log.count("MKCOL /remote/sub") != 1 || log.count("MKCOL /remote") != 2 || log.count("MKCOL ") != 2 {
		t.Fatalf("mkcol not cached: %v", log.all())
	}
	if puts["/remote/sub/a.txt"] != "hello" || puts["/remote/sub/b.txt"] != "world" {
		t.Fatalf("%v", puts)
	}
}

func TestUploadEscapesSegments(t *testing.T) {
	log := &reqLog{}
	puts := map[string]string{}
	srv := nestedServer(t, log, puts)
	c := New(srv.URL, "u", "pw")
	f := writeFile(t, "x.txt", "hello")
	if err := c.Upload(context.Background(), f, "/d#1/a?b%c.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := puts["/d%231/a%3Fb%25c.txt"]; !ok {
		t.Fatalf("puts %v log %v", puts, log.all())
	}
	if log.count("MKCOL /d%231") != 1 {
		t.Fatalf("%v", log.all())
	}
}

func TestBasePathNotEscaped(t *testing.T) {
	log := &reqLog{}
	puts := map[string]string{}
	srv := nestedServer(t, log, puts)
	c := New(srv.URL+"/dav/", "u", "pw")
	got := c.remoteURL([]string{"a b"})
	if got != srv.URL+"/dav/a%20b" {
		t.Fatal(got)
	}
}

func TestMkcolErrorsHidePassword(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		code := code
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) }))
		c := New(srv.URL, "u", "secret-password")
		err := c.EnsureDir(context.Background(), "/x")
		srv.Close()
		if err == nil || strings.Contains(err.Error(), "secret-password") {
			t.Fatalf("code %d err %v", code, err)
		}
		if code == http.StatusUnauthorized && err.Error() != "认证失败" {
			t.Fatal(err)
		}
		if code != http.StatusUnauthorized && err.Error() != "无法访问" {
			t.Fatal(err)
		}
	}
}
