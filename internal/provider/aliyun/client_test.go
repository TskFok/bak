package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"BaiduYunPanBak/internal/provider"
)

var _ provider.Provider = (*Client)(nil)

type fakeServer struct {
	mu       sync.Mutex
	creates  []map[string]any
	lists    []map[string]any
	putBody  string
	putAuth  string
	existing map[string]string // name -> file_id for children of root
}

func newFake(t *testing.T, f *fakeServer) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		body, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/adrive/v1.0/user/getDriveInfo":
			_, _ = w.Write([]byte(`{"default_drive_id":"d1"}`))
		case "/adrive/v1.0/openFile/list":
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			f.lists = append(f.lists, m)
			if id, ok := f.existing[m["parent_file_id"].(string)+"/"]; ok {
				_, _ = w.Write([]byte(`{"items":[{"file_id":"` + id + `","name":"bak","type":"folder"}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"items":[]}`))
		case "/adrive/v1.0/openFile/create":
			var m map[string]any
			_ = json.Unmarshal(body, &m)
			f.creates = append(f.creates, m)
			if m["type"] == "folder" {
				_, _ = w.Write([]byte(`{"file_id":"folder1","type":"folder"}`))
				return
			}
			_, _ = w.Write([]byte(`{"file_id":"file1","part_info_list":[{"upload_url":"` + srv.URL + `/put"}]}`))
		case "/put":
			f.putBody = string(body)
			f.putAuth = r.Header.Get("Authorization")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientFlow(t *testing.T) {
	f := &fakeServer{}
	srv := newFake(t, f)
	c := New(srv.URL, "tok-secret", srv.Client())
	ctx := context.Background()

	noErr(t, c.Test(ctx))
	noErr(t, c.EnsureDir(ctx, "/bak"))
	eq(t, 1, len(f.creates))
	eq(t, "folder", f.creates[0]["type"])
	eq(t, "bak", f.creates[0]["name"])
	eq(t, "root", f.creates[0]["parent_file_id"])
	eq(t, "d1", f.creates[0]["drive_id"])

	local := filepath.Join(t.TempDir(), "a.txt")
	noErr(t, os.WriteFile(local, []byte("hello aliyun"), 0o600))
	noErr(t, c.Upload(ctx, local, "/bak/a.txt"))
	eq(t, "hello aliyun", f.putBody)
	eq(t, "", f.putAuth)
	last := f.creates[len(f.creates)-1]
	eq(t, "file", last["type"])
	eq(t, "a.txt", last["name"])
	eq(t, "folder1", last["parent_file_id"])
}

func TestEnsureDirSkipsExistingFolder(t *testing.T) {
	f := &fakeServer{existing: map[string]string{"root/": "existing1"}}
	srv := newFake(t, f)
	c := New(srv.URL, "tok", srv.Client())
	noErr(t, c.EnsureDir(context.Background(), "/bak"))
	eq(t, 0, len(f.creates))
}

func TestUnauthorizedErrorHidesToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := New(srv.URL, "tok-secret", srv.Client())
	err := c.Test(context.Background())
	isTrue(t, err != nil && err.Error() == "授权失败", "want 授权失败")
	isTrue(t, !strings.Contains(err.Error(), "tok-secret"), "token leaked")
}

func TestUploadPutFailure(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/adrive/v1.0/user/getDriveInfo":
			_, _ = w.Write([]byte(`{"default_drive_id":"d1"}`))
		case "/adrive/v1.0/openFile/create":
			_, _ = w.Write([]byte(`{"file_id":"file1","part_info_list":[{"upload_url":"` + srv.URL + `/put"}]}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	local := filepath.Join(t.TempDir(), "a.txt")
	noErr(t, os.WriteFile(local, []byte("x"), 0o600))
	c := New(srv.URL, "tok-secret", srv.Client())
	err := c.Upload(context.Background(), local, "/a.txt")
	isTrue(t, err != nil && err.Error() == "上传失败", "want 上传失败")
}

func TestExchangeCodeAndRefresh(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eq(t, "/oauth/access_token", r.URL.Path)
		var m map[string]any
		noErr(t, json.NewDecoder(r.Body).Decode(&m))
		got = append(got, m)
		_, _ = w.Write([]byte(`{"access_token":"a1","refresh_token":"r1","expires_in":7200}`))
	}))
	defer srv.Close()
	ctx := context.Background()

	before := time.Now()
	tok, err := ExchangeCode(ctx, srv.URL, "cid", "sec", "code1")
	noErr(t, err)
	eq(t, "a1", tok.AccessToken)
	eq(t, "r1", tok.RefreshToken)
	d := tok.ExpiresAt.Sub(before.Add(7200 * time.Second))
	isTrue(t, d >= 0 && d < 5*time.Second, "bad ExpiresAt")
	eq(t, "authorization_code", got[0]["grant_type"])
	eq(t, "code1", got[0]["code"])
	eq(t, "cid", got[0]["client_id"])
	eq(t, "sec", got[0]["client_secret"])

	_, err = Refresh(ctx, srv.URL, "cid", "sec", "r0")
	noErr(t, err)
	eq(t, "refresh_token", got[1]["grant_type"])
	eq(t, "r0", got[1]["refresh_token"])
}

func eq(t *testing.T, want, got any) {
	t.Helper()
	if fmt.Sprint(want) != fmt.Sprint(got) {
		t.Fatalf("want %v, got %v", want, got)
	}
}

func noErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func isTrue(t *testing.T, ok bool, msg string) {
	t.Helper()
	if !ok {
		t.Fatal(msg)
	}
}
