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
