package backup

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yeka/zip"
)

func TestArchiveFileName(t *testing.T) {
	day := time.Date(2026, 10, 13, 8, 0, 0, 0, time.Local)
	if got := ArchiveFileName("相册", false, time.Time{}); got != "相册.zip" {
		t.Fatalf("got %q", got)
	}
	if got := ArchiveFileName("a/b", false, time.Time{}); got != "a_b.zip" {
		t.Fatalf("got %q", got)
	}
	if got := ArchiveFileName(" already.zip ", false, time.Time{}); got != "already.zip" {
		t.Fatalf("got %q", got)
	}
	if got := ArchiveFileName("  ", false, time.Time{}); got != "backup.zip" {
		t.Fatalf("got %q", got)
	}
	if got := ArchiveFileName("相册", true, day); got != "相册-2026-10-13.zip" {
		t.Fatalf("got %q", got)
	}
	if got := ArchiveFileName("already.zip", true, day); got != "already-2026-10-13.zip" {
		t.Fatalf("got %q", got)
	}
}

func TestWriteArchivePassword(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "子目录")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("beta"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := collectPackedFiles(dir)
	if err != nil || len(files) != 2 {
		t.Fatalf("%v %+v", err, files)
	}
	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := writeArchive(dest, files, "p@ss", ArchiveEncryptionAES, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zipEntries(data, ""); err == nil {
		t.Fatal("encrypted archive opened without a password")
	}
	if _, err := zipEntries(data, "wrong"); err == nil {
		t.Fatal("encrypted archive opened with the wrong password")
	}
	got, err := zipEntries(data, "p@ss")
	if err != nil {
		t.Fatal(err)
	}
	if got["子目录/a.txt"] != "alpha" || got["b.txt"] != "beta" {
		t.Fatalf("%v", got)
	}

	plain := filepath.Join(t.TempDir(), "plain.zip")
	if err := writeArchive(plain, files, "", ArchiveEncryptionAES, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err = zipEntries(raw, "")
	if err != nil || got["b.txt"] != "beta" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestRunTaskPackBeforeUpload(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	a := filepath.Join(sub, "a.txt")
	if err := os.WriteFile(a, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &memStore{}
	up := &captureProvider{}
	task := Task{
		ID: 7, Name: "相册", CloudAccountID: 3, LocalDir: dir, RemoteDir: "/bak",
		PackBeforeUpload: true, ArchivePassword: "secret",
	}
	run, err := RunTask(context.Background(), store, task, up, "manual", time.Now())
	if err != nil || run.Status != "success" || run.SuccessCount != 1 || run.SkipCount != 0 {
		t.Fatalf("%+v %v", run, err)
	}
	if len(up.uploads) != 1 || up.uploads[0] != "/bak/相册.zip" {
		t.Fatalf("uploaded %v", up.uploads)
	}
	got, err := zipEntries(up.body, "secret")
	if err != nil || got["sub/a.txt"] != "hello" || got["b.txt"] != "world" {
		t.Fatalf("%v %v", got, err)
	}

	run, err = RunTask(context.Background(), store, task, up, "manual", time.Now())
	if err != nil || run.Status != "success" || run.SkipCount != 1 || run.SuccessCount != 0 || len(up.uploads) != 1 {
		t.Fatalf("skip %+v uploads %v err %v", run, up.uploads, err)
	}

	if err := os.WriteFile(a, []byte("hello!"), 0o644); err != nil {
		t.Fatal(err)
	}
	run, err = RunTask(context.Background(), store, task, up, "manual", time.Now())
	if err != nil || run.SuccessCount != 1 || len(up.uploads) != 2 {
		t.Fatalf("changed %+v uploads %d err %v", run, len(up.uploads), err)
	}
	if got, err = zipEntries(up.body, "secret"); err != nil || got["sub/a.txt"] != "hello!" {
		t.Fatalf("%v %v", got, err)
	}

	task.ArchivePassword = "secret2"
	run, err = RunTask(context.Background(), store, task, up, "manual", time.Now())
	if err != nil || run.SuccessCount != 1 || len(up.uploads) != 3 {
		t.Fatalf("password %+v uploads %d err %v", run, len(up.uploads), err)
	}
	if _, err := zipEntries(up.body, "secret"); err == nil {
		t.Fatal("new archive still opens with the old password")
	}
	if got, err = zipEntries(up.body, "secret2"); err != nil || got["b.txt"] != "world" {
		t.Fatalf("%v %v", got, err)
	}
}

func TestPackUnreadableFileDoesNotUpload(t *testing.T) {
	dir := t.TempDir()
	hidden := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(hidden, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hidden, 0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(hidden, 0o644) }()
	if f, err := os.Open(hidden); err == nil {
		_ = f.Close()
		t.Skip("chmod 000 did not prevent reading on this platform")
	}
	up := &captureProvider{}
	run, err := RunTask(context.Background(), &memStore{}, Task{
		ID: 1, Name: "n", LocalDir: dir, RemoteDir: "/bak", PackBeforeUpload: true,
	}, up, "manual", time.Now())
	if err != nil || run.Status != "failed" || run.ErrorSummary != "无法读取：secret.txt" {
		t.Fatalf("%+v %v", run, err)
	}
	if len(up.uploads) != 0 {
		t.Fatalf("uploaded %v", up.uploads)
	}
}

func TestPackSkipsUnixSocket(t *testing.T) {
	// macOS 的 Unix 套接字路径上限约 104 字节，t.TempDir() 经常超限。
	dir, err := os.MkdirTemp("/tmp", "baksock")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sockDir := filepath.Join(dir, "dnf", "mysql")
	if err := os.MkdirAll(sockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dnf", "data.txt"), []byte("rows"), 0o644); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(sockDir, "mysql.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	up := &captureProvider{}
	run, err := RunTask(context.Background(), &memStore{}, Task{
		ID: 1, Name: "n", LocalDir: dir, RemoteDir: "/bak", PackBeforeUpload: true,
	}, up, "manual", time.Now())
	if err != nil || run.Status != "success" || run.SuccessCount != 1 || run.ErrorSummary != "" {
		t.Fatalf("%+v %v", run, err)
	}
	if len(up.uploads) != 1 {
		t.Fatalf("uploaded %v", up.uploads)
	}
	got, err := zipEntries(up.body, "")
	if err != nil || got["dnf/data.txt"] != "rows" {
		t.Fatalf("%v %v", got, err)
	}
	if _, ok := got["dnf/mysql/mysql.sock"]; ok {
		t.Fatal("socket was packed")
	}
}

func TestPackUploadFailureRemovesTempZip(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	hold := &holdZip{}
	store := &removeCheckStore{path: &hold.path}
	task := Task{ID: 1, Name: "n", LocalDir: dir, RemoteDir: "/bak", PackBeforeUpload: true}
	run, err := RunTask(context.Background(), store, task, hold, "manual", time.Now())
	if err != nil || run.Status != "failed" || run.FailCount != 1 || run.ErrorSummary != "" {
		t.Fatalf("%+v %v", run, err)
	}
	if hold.path == "" {
		t.Fatal("upload was not attempted")
	}
	if !store.removedBeforeSave {
		t.Fatal("temp zip still present when recording the upload failure")
	}
	if _, err := os.Stat(hold.path); !os.IsNotExist(err) {
		t.Fatalf("temp zip remains after run: %v", err)
	}
}

func TestPackUploadAndDirFailures(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := Task{ID: 1, Name: "n", LocalDir: dir, RemoteDir: "/bak", PackBeforeUpload: true}
	run, err := RunTask(context.Background(), &memStore{}, task, errUpload{}, "manual", time.Now())
	if err != nil || run.Status != "failed" || run.FailCount != 1 || run.ErrorSummary != "" {
		t.Fatalf("%+v %v", run, err)
	}
	run, err = RunTask(context.Background(), &memStore{}, task, denyDir{}, "manual", time.Now())
	if err != nil || run.Status != "failed" || run.ErrorSummary != "无法准备网盘目录" {
		t.Fatalf("%+v %v", run, err)
	}
}

func TestPackByDateKeepsHistory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 10, 13, 9, 0, 0, 0, time.Local)
	store := &memStore{}
	up := &captureProvider{}
	task := Task{ID: 1, Name: "相册", LocalDir: dir, RemoteDir: "/bak", PackBeforeUpload: true, ArchiveByDate: true}
	run, err := RunTask(context.Background(), store, task, up, "manual", day)
	if err != nil || run.Status != "success" || len(up.uploads) != 1 || up.uploads[0] != "/bak/相册-2026-10-13.zip" {
		t.Fatalf("%+v %v", run, up.uploads)
	}
	run, err = RunTask(context.Background(), store, task, up, "manual", day.Add(time.Hour))
	if err != nil || run.SkipCount != 1 || len(up.uploads) != 1 {
		t.Fatalf("same day %+v uploads %v err %v", run, up.uploads, err)
	}
	next := day.Add(24 * time.Hour)
	run, err = RunTask(context.Background(), store, task, up, "manual", next)
	if err != nil || run.SuccessCount != 1 || len(up.uploads) != 2 || up.uploads[1] != "/bak/相册-2026-10-14.zip" {
		t.Fatalf("next day %+v uploads %v err %v", run, up.uploads, err)
	}
}

func TestPackZipCryptoAndProgress(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := &memStore{}
	up := &captureProvider{}
	task := Task{
		ID: 1, Name: "n", LocalDir: dir, RemoteDir: "/bak",
		PackBeforeUpload: true, ArchivePassword: "secret", ArchiveEncryption: ArchiveEncryptionZipCrypto,
	}
	run, err := RunTask(context.Background(), store, task, up, "manual", time.Now())
	if err != nil || run.Status != "success" || run.ProgressNote != "" {
		t.Fatalf("%+v %v", run, err)
	}
	var notes []string
	for _, p := range store.progress {
		if p.ProgressNote != "" {
			notes = append(notes, p.ProgressNote)
		}
	}
	want := []string{"正在压缩 1/2", "正在压缩 2/2", "正在上传压缩包"}
	if len(notes) < len(want) {
		t.Fatalf("notes %v", notes)
	}
	for i := range want {
		if notes[i] != want[i] {
			t.Fatalf("notes %v", notes)
		}
	}
	method, entries, err := zipMethodAndEntries(up.body, "secret")
	if err != nil || entries["a.txt"] != "one" || method != "zipcrypto" {
		t.Fatalf("method %s entries %v err %v", method, entries, err)
	}

	task.ArchiveEncryption = ArchiveEncryptionAES
	run, err = RunTask(context.Background(), store, task, up, "manual", time.Now())
	if err != nil || run.SuccessCount != 1 || len(up.uploads) != 2 {
		t.Fatalf("reupload %+v uploads %d err %v", run, len(up.uploads), err)
	}
	method, _, err = zipMethodAndEntries(up.body, "secret")
	if err != nil || method != "aes" {
		t.Fatalf("method %s err %v", method, err)
	}
}

type captureProvider struct {
	fakeProvider
	body []byte
}

func (c *captureProvider) Upload(ctx context.Context, localPath, remotePath string) error {
	b, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	c.body = append([]byte(nil), b...)
	return c.fakeProvider.Upload(ctx, localPath, remotePath)
}

type holdZip struct{ path string }

func (holdZip) Test(ctx context.Context) error                        { return nil }
func (holdZip) EnsureDir(ctx context.Context, remoteDir string) error { return nil }
func (h *holdZip) Upload(ctx context.Context, localPath, remotePath string) error {
	h.path = localPath
	if _, err := os.Stat(localPath); err != nil {
		return err
	}
	return errors.New("网盘拒绝")
}

type removeCheckStore struct {
	memStore
	path              *string
	removedBeforeSave bool
}

func (s *removeCheckStore) SaveFileResult(ctx context.Context, runID, taskID uint, result FileResult) error {
	if result.Status == "failed" && s.path != nil && *s.path != "" {
		_, err := os.Stat(*s.path)
		s.removedBeforeSave = os.IsNotExist(err)
	}
	return s.memStore.SaveFileResult(ctx, runID, taskID, result)
}

type errUpload struct{}

func (errUpload) Test(ctx context.Context) error                        { return nil }
func (errUpload) EnsureDir(ctx context.Context, remoteDir string) error { return nil }
func (errUpload) Upload(ctx context.Context, localPath, remotePath string) error {
	return errors.New("网盘拒绝")
}

type denyDir struct{}

func (denyDir) Test(ctx context.Context) error                        { return nil }
func (denyDir) EnsureDir(ctx context.Context, remoteDir string) error { return errors.New("nope") }
func (denyDir) Upload(ctx context.Context, localPath, remotePath string) error {
	return nil
}

func zipEntries(data []byte, password string) (map[string]string, error) {
	_, entries, err := zipMethodAndEntries(data, password)
	return entries, err
}

func zipMethodAndEntries(data []byte, password string) (string, map[string]string, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", nil, err
	}
	method := "plain"
	out := map[string]string{}
	for _, f := range r.File {
		if f.IsEncrypted() {
			if hasWinZipAES(f.Extra) {
				method = "aes"
			} else {
				method = "zipcrypto"
			}
		}
		if password != "" {
			f.SetPassword(password)
		}
		rc, err := f.Open()
		if err != nil {
			return method, nil, err
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return method, nil, err
		}
		out[f.Name] = string(b)
	}
	return method, out, nil
}

func hasWinZipAES(extra []byte) bool {
	for i := 0; i+4 <= len(extra); {
		id := uint16(extra[i]) | uint16(extra[i+1])<<8
		size := int(extra[i+2]) | int(extra[i+3])<<8
		if id == 0x9901 {
			return true
		}
		i += 4 + size
	}
	return false
}
