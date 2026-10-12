package baidu

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"BaiduYunPanBak/internal/provider"
)

var _ provider.Provider = (*Client)(nil)

type fakeUp struct {
	parts      []string
	size       int64
	blocks     []string
	mkdirs     []string
	chunkPaths []string
	partErr    error
	errno      int
}

func (f *fakeUp) Precreate(ctx context.Context, token, remote string, size int64, blocks []string) (string, error) {
	f.size = size
	f.blocks = blocks
	return "upid", nil
}
func (f *fakeUp) UploadPart(ctx context.Context, token, remote, uploadID string, seq int, chunkPath string) error {
	if f.partErr != nil {
		return f.partErr
	}
	b, _ := os.ReadFile(chunkPath)
	f.parts = append(f.parts, string(b))
	f.chunkPaths = append(f.chunkPaths, chunkPath)
	return nil
}
func (f *fakeUp) Create(ctx context.Context, token, remote, uploadID string, size int64, blocks []string) (string, string, int, error) {
	return "1", "abc", f.errno, nil
}
func (f *fakeUp) Mkdir(ctx context.Context, token, remote string) error {
	f.mkdirs = append(f.mkdirs, remote)
	return nil
}

func writeLocal(t *testing.T, dir, content string) string {
	t.Helper()
	local := filepath.Join(dir, "f.bin")
	if err := os.WriteFile(local, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return local
}

func chunkEntries(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestChunkUpload(t *testing.T) {
	dir := t.TempDir()
	local := writeLocal(t, dir, "abcde")
	up := &fakeUp{}
	c := New("token", up, filepath.Join(dir, "chunks"), 2)
	if err := c.Upload(context.Background(), local, "/bak/f.bin"); err != nil {
		t.Fatal(err)
	}
	if up.size != 5 || len(up.parts) != 3 || up.parts[0] != "ab" || up.parts[2] != "e" {
		t.Fatalf("%+v", up)
	}
	if len(up.blocks) != 3 || up.blocks[0] != "187ef4436122d1cc2f40dc2b92f0eba0" {
		t.Fatalf("blocks %v", up.blocks)
	}
	for _, p := range up.chunkPaths {
		if !strings.HasPrefix(p, filepath.Join(dir, "chunks")) {
			t.Fatalf("chunk outside chunkDir: %s", p)
		}
	}
	if n := chunkEntries(t, filepath.Join(dir, "chunks")); n != 0 {
		t.Fatal("chunks left behind")
	}
}

func TestChunksRemovedWhenPartFails(t *testing.T) {
	dir := t.TempDir()
	local := writeLocal(t, dir, "abcde")
	up := &fakeUp{partErr: errors.New("boom")}
	c := New("token", up, filepath.Join(dir, "chunks"), 2)
	if err := c.Upload(context.Background(), local, "/bak/f.bin"); err == nil {
		t.Fatal("expected error")
	}
	if n := chunkEntries(t, filepath.Join(dir, "chunks")); n != 0 {
		t.Fatal("chunks left behind")
	}
}

func TestExactMultipleOfChunkSize(t *testing.T) {
	dir := t.TempDir()
	local := writeLocal(t, dir, "abcd")
	up := &fakeUp{}
	c := New("token", up, filepath.Join(dir, "chunks"), 2)
	if err := c.Upload(context.Background(), local, "/bak/f.bin"); err != nil {
		t.Fatal(err)
	}
	if len(up.parts) != 2 || up.parts[1] != "cd" {
		t.Fatalf("%v", up.parts)
	}
}

func TestOnCreatedAndErrno(t *testing.T) {
	dir := t.TempDir()
	local := writeLocal(t, dir, "abc")
	up := &fakeUp{errno: 31}
	c := New("token", up, filepath.Join(dir, "chunks"), 2)
	var gotSize int64
	var gotFs, gotMd5 string
	var gotErrno int
	c.OnCreated = func(size int64, remote, fsID, md5 string, errno int) {
		gotSize, gotFs, gotMd5, gotErrno = size, fsID, md5, errno
	}
	if err := c.Upload(context.Background(), local, "/bak/f.bin"); err == nil {
		t.Fatal("non-zero errno must fail")
	}
	if gotSize != 3 || gotFs != "1" || gotMd5 != "abc" || gotErrno != 31 {
		t.Fatalf("%d %s %s %d", gotSize, gotFs, gotMd5, gotErrno)
	}
}

func TestTestRequiresToken(t *testing.T) {
	c := New("", &fakeUp{}, t.TempDir(), 2)
	err := c.Test(context.Background())
	if err == nil || err.Error() != "未连接" {
		t.Fatalf("err %v", err)
	}
	if err := New("t", &fakeUp{}, t.TempDir(), 2).Test(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureDirCallsMkdir(t *testing.T) {
	up := &fakeUp{}
	c := New("token", up, t.TempDir(), 2)
	if err := c.EnsureDir(context.Background(), "/bak/2026-10-12"); err != nil {
		t.Fatal(err)
	}
	if len(up.mkdirs) != 1 || up.mkdirs[0] != "/bak/2026-10-12" {
		t.Fatalf("%v", up.mkdirs)
	}
}

func TestRemotePath(t *testing.T) {
	now := time.Date(2026, 10, 12, 23, 59, 0, 0, time.Local)
	got := RemotePath("/data/some/file.txt", now)
	if got != "/bak/2026-10-12/file.txt" {
		t.Fatal(got)
	}
}

func TestHistoryRecord(t *testing.T) {
	h := historyRecord(10, "/bak/a", "99", "m", 0)
	if h.Size != 10 || h.FsId != "99" || h.Path != "/bak/a" || h.Md5 != "m" {
		t.Fatalf("%+v", h)
	}
	big := historyRecord(int64(math.MaxInt32)+1, "/bak/a", "99", "m", 0)
	if big.Size != 0 || big.FsId != "99" {
		t.Fatalf("%+v", big)
	}
	edge := historyRecord(math.MaxInt32, "/bak/a", "99", "m", 0)
	if edge.Size != math.MaxInt32 {
		t.Fatalf("%+v", edge)
	}
}
