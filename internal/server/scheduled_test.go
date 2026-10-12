package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"BaiduYunPanBak/internal/backup"
	"BaiduYunPanBak/internal/cloudacct"
	"BaiduYunPanBak/internal/provider"
	"BaiduYunPanBak/internal/store"
)

func TestScheduleRunnerRefreshesAndRunsAsSchedule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	st, prov, tokens := &memAPIStore{}, &fakeProvider{}, &fakeTokens{}
	_, run := NewMuxWithRunner(Deps{
		AuthDeps:  AuthDeps{Users: &memAuthUsers{}, Sessions: &memSessions{}, SessionTTL: time.Hour},
		Store:     st,
		JWTSecret: testJWTSecret,
		Providers: func(name string, s cloudacct.Secrets) (provider.Provider, error) { return prov, nil },
		Tokens:    tokens,
	})
	ctx := context.Background()
	blob, err := cloudacct.Seal(testJWTSecret, cloudacct.Secrets{
		ClientID: "cid", ClientSecret: "cs", AccessToken: "old", RefreshToken: "rt",
		ExpiresAt: time.Now().Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertCloud(ctx, &store.CloudAccount{Provider: "baidu", SecretBlob: blob, Status: "connected"}); err != nil {
		t.Fatal(err)
	}
	task := backup.Task{Name: "b", LocalDir: dir, CloudAccountID: st.clouds[0].ID, RemoteDir: "/apps/bak", ScheduleTime: "01:00", Enabled: true}
	if err := st.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}

	// 令牌 5 分钟内到期：先刷新，再以 schedule 触发运行。
	if err := run(ctx, task); err != nil {
		t.Fatal(err)
	}
	if tokens.refreshed != 1 {
		t.Fatalf("refreshed = %d", tokens.refreshed)
	}
	if len(st.runs) != 1 || st.runs[0].Trigger != "schedule" || st.runs[0].Status != "success" {
		t.Fatalf("runs = %+v", st.runs)
	}
	sec, _ := cloudacct.Open(testJWTSecret, st.clouds[0].SecretBlob)
	if sec.AccessToken != "access-new" {
		t.Fatalf("token not persisted: %+v", sec)
	}

	// 刷新失败：记一条 failed 的 schedule 运行，不上传，不返回错误。
	blob, _ = cloudacct.Seal(testJWTSecret, cloudacct.Secrets{
		ClientID: "cid", ClientSecret: "cs", AccessToken: "old", RefreshToken: "rt",
		ExpiresAt: time.Now().Add(-time.Minute),
	})
	st.clouds[0].SecretBlob = blob
	tokens.refreshErr = errors.New("refresh_token=leaky")
	before := len(prov.uploaded)
	if err := run(ctx, task); err != nil {
		t.Fatal(err)
	}
	last := st.runs[len(st.runs)-1]
	if last.Trigger != "schedule" || last.Status != "failed" || last.FinishedAt == nil || last.ErrorSummary == "" || last.ErrorSummary == "refresh_token=leaky" {
		t.Fatalf("failed run = %+v", last)
	}
	if len(prov.uploaded) != before {
		t.Fatal("uploaded despite refresh failure")
	}

	// 网盘不存在：同样记为 failed。
	task.CloudAccountID = 999
	if err := run(ctx, task); err != nil {
		t.Fatal(err)
	}
	if last = st.runs[len(st.runs)-1]; last.Trigger != "schedule" || last.Status != "failed" {
		t.Fatalf("missing cloud run = %+v", last)
	}
}
