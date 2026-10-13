package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListLocalDirs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "note.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	docs := filepath.Join(root, "alpha")
	if err := os.Symlink(docs, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}

	got, err := listLocalDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != filepath.Clean(root) {
		t.Fatalf("path = %q", got.Path)
	}
	if got.Parent != filepath.Dir(filepath.Clean(root)) {
		t.Fatalf("parent = %q", got.Parent)
	}
	names := make([]string, len(got.Entries))
	for i, e := range got.Entries {
		names[i] = e.Name
		if e.Path != filepath.Join(got.Path, e.Name) {
			t.Fatalf("entry path = %q", e.Path)
		}
	}
	if strings.Join(names, ",") != "alpha,linked,zeta" {
		t.Fatalf("entries = %v", names)
	}

	viaDotDot, err := listLocalDirs(filepath.Join(docs, ".."))
	if err != nil {
		t.Fatal(err)
	}
	if viaDotDot.Path != got.Path {
		t.Fatalf("cleaned path = %q, want %q", viaDotDot.Path, got.Path)
	}

	if _, err := listLocalDirs(filepath.Join(root, "missing")); err != errDirUnreadable {
		t.Fatalf("missing: %v", err)
	}
	if _, err := listLocalDirs(filepath.Join(root, "note.txt")); err != errDirUnreadable {
		t.Fatalf("file: %v", err)
	}
}

func TestListLocalDirsRootHasNoParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("根目录行为只在 Unix 上断言")
	}
	got, err := listLocalDirs("/")
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != "/" || got.Parent != "" {
		t.Fatalf("path=%q parent=%q", got.Path, got.Parent)
	}
}

func TestListLocalDirsEmptyStartsAtHome(t *testing.T) {
	got, err := listLocalDirs("  ")
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != filepath.Clean(home) {
		t.Fatalf("path = %q, want home %q", got.Path, filepath.Clean(home))
	}
}

func TestListDirsHTTP(t *testing.T) {
	env := newAPIEnv(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec := env.do(http.MethodGet, "/api/dirs?path="+url.QueryEscape(root), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	var body dirListing
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Path != filepath.Clean(root) || len(body.Entries) != 1 || body.Entries[0].Name != "docs" {
		t.Fatalf("body = %+v", body)
	}

	rec = env.do(http.MethodGet, "/api/dirs?path="+url.QueryEscape(filepath.Join(root, "missing")), "")
	if rec.Code != http.StatusBadRequest || rec.Body.String() != "目录不存在或不可读\n" {
		t.Fatalf("missing: %d %q", rec.Code, rec.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dirs", nil)
	rec = httptest.NewRecorder()
	env.h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no session: %d", rec.Code)
	}
}
