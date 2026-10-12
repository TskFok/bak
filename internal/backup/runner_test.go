package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeProvider struct {
	uploads []string
	fail    string
}

func (f *fakeProvider) Test(ctx context.Context) error                        { return nil }
func (f *fakeProvider) EnsureDir(ctx context.Context, remoteDir string) error { return nil }
func (f *fakeProvider) Upload(ctx context.Context, localPath, remotePath string) error {
	if filepath.Base(localPath) == f.fail {
		return errors.New("网盘拒绝")
	}
	f.uploads = append(f.uploads, remotePath)
	return nil
}

type memStore struct {
	running     bool
	runs        []Run
	files       []FileResult
	marked      int
	progress    []Run
	progressErr error
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
func (m *memStore) UpdateRunProgress(ctx context.Context, run Run) error {
	m.progress = append(m.progress, run)
	return m.progressErr
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

func TestUnreadableRootDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if _, err := os.ReadDir(dir); err == nil {
		t.Skip("chmod 000 did not prevent listing on this platform")
	}
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	store := &memStore{}
	run, err := RunTask(context.Background(), store, Task{ID: 1, LocalDir: dir, RemoteDir: "/bak"}, &fakeProvider{}, "manual", start)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "failed" || run.ErrorSummary != "本地目录不存在或不可读" {
		t.Fatalf("%+v", run)
	}
	assertFinishedAt(t, run, start)
}

func TestFinishedAtRecorded(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &memStore{}
	run, err := RunTask(context.Background(), store, Task{ID: 1, LocalDir: dir, RemoteDir: "/bak"}, &fakeProvider{}, "manual", start)
	if err != nil || run.Status != "success" {
		t.Fatalf("%+v %v", run, err)
	}
	assertFinishedAt(t, run, start)

	store = &memStore{}
	run, err = RunTask(context.Background(), store, Task{ID: 1, LocalDir: filepath.Join(t.TempDir(), "nope"), RemoteDir: "/bak"}, &fakeProvider{}, "manual", start)
	if err != nil || run.Status != "failed" {
		t.Fatalf("%+v %v", run, err)
	}
	assertFinishedAt(t, run, start)
}

func assertFinishedAt(t *testing.T, run Run, startedAt time.Time) {
	t.Helper()
	if run.FinishedAt == nil || run.FinishedAt.IsZero() {
		t.Fatalf("FinishedAt unset: %+v", run)
	}
	if !run.StartedAt.Equal(startedAt) {
		t.Fatalf("StartedAt=%v want %v", run.StartedAt, startedAt)
	}
	if run.FinishedAt.Equal(startedAt) {
		t.Fatalf("FinishedAt should not equal injected start time")
	}
}

func writeFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExecuteDoesNotRecheckBusy(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "a.txt")
	store := &memStore{}
	task := Task{ID: 1, LocalDir: dir, RemoteDir: "/bak"}
	run, err := StartRun(context.Background(), store, task, "manual", time.Now())
	if err != nil || run.ID == 0 || run.Status != "running" {
		t.Fatalf("%+v %v", run, err)
	}
	if !store.running {
		t.Fatal("running row should exist after StartRun")
	}
	done, err := Execute(context.Background(), store, task, &fakeProvider{}, run)
	if err != nil {
		t.Fatalf("Execute must not see its own running row: %v", err)
	}
	if done.ID != run.ID || done.Status != "success" || done.SuccessCount != 1 || done.FinishedAt == nil {
		t.Fatalf("%+v", done)
	}
}

func TestStartRunBusy(t *testing.T) {
	store := &memStore{running: true}
	if _, err := StartRun(context.Background(), store, Task{ID: 1}, "manual", time.Now()); !errors.Is(err, ErrBusy) {
		t.Fatalf("got %v", err)
	}
	if len(store.runs) != 0 {
		t.Fatal("no run row when busy")
	}
}

func TestProgressSavedAfterEachFile(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "a.txt", "b.txt", "c.txt")
	store := &memStore{}
	run, err := RunTask(context.Background(), store, Task{ID: 1, LocalDir: dir, RemoteDir: "/bak"}, &fakeProvider{fail: "b.txt"}, "manual", time.Now())
	if err != nil || run.Status != "partial" {
		t.Fatalf("%+v %v", run, err)
	}
	if len(store.progress) != 3 {
		t.Fatalf("progress calls %d", len(store.progress))
	}
	for i, pr := range store.progress {
		if pr.ID != run.ID || pr.Status != "running" || pr.FinishedAt != nil {
			t.Fatalf("progress %d %+v", i, pr)
		}
		if pr.SuccessCount+pr.SkipCount+pr.FailCount != i+1 {
			t.Fatalf("progress %d counts %+v", i, pr)
		}
	}
	last := store.progress[2]
	if last.SuccessCount != 2 || last.FailCount != 1 {
		t.Fatalf("%+v", last)
	}
}

func TestProgressErrorDoesNotAbortWalk(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, "a.txt", "b.txt")
	store := &memStore{progressErr: errors.New("db busy")}
	run, err := RunTask(context.Background(), store, Task{ID: 1, LocalDir: dir, RemoteDir: "/bak"}, &fakeProvider{}, "manual", time.Now())
	if err != nil || run.Status != "success" || run.SuccessCount != 2 {
		t.Fatalf("%+v %v", run, err)
	}
	if len(store.files) != 2 {
		t.Fatalf("files %d", len(store.files))
	}
}
