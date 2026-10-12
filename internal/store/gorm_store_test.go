package store

import (
	"context"
	"testing"
	"time"

	"BaiduYunPanBak/internal/auth"
	"BaiduYunPanBak/internal/backup"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestSetupTransactionAndStaleRuns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	s := NewGorm(db)
	ctx := context.Background()
	if _, err := auth.Setup(ctx, s, "me", "password1", "password1"); err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Setup(ctx, s, "me", "password1", "password1"); err != auth.ErrUserExists {
		t.Fatalf("got %v", err)
	}
	task := backup.Task{Name: "t", LocalDir: "/tmp", RemoteDir: "/bak", ScheduleTime: "09:00", Enabled: true, CloudAccountID: 1}
	if err := s.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	run := &backup.Run{TaskID: task.ID, Trigger: "manual", Status: "running", StartedAt: time.Now()}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := s.FailStaleRuns(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, run.ID)
	if err != nil || got.Status != "failed" || got.ErrorSummary != "进程已退出" {
		t.Fatalf("%+v %v", got, err)
	}
	if err := s.DeleteTask(ctx, task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRun(ctx, run.ID); err != nil {
		t.Fatal("run deleted with task")
	}
}

func newTestStore(t *testing.T, name string) *GormStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+name+"?mode=memory&cache=shared"), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{SingularTable: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return NewGorm(db)
}

func TestLatestSuccessAndScheduling(t *testing.T) {
	s := newTestStore(t, "latest")
	ctx := context.Background()
	task := backup.Task{Name: "t", LocalDir: "/tmp", RemoteDir: "/bak", ScheduleTime: "09:00", Enabled: true, CloudAccountID: 1}
	off := backup.Task{Name: "off", LocalDir: "/tmp", RemoteDir: "/bak", ScheduleTime: "09:00", CloudAccountID: 1}
	for _, tk := range []*backup.Task{&task, &off} {
		if err := s.CreateTask(ctx, tk); err != nil {
			t.Fatal(err)
		}
	}
	enabled, err := s.ListEnabled(ctx)
	if err != nil || len(enabled) != 1 || enabled[0].ID != task.ID {
		t.Fatalf("%+v %v", enabled, err)
	}
	day := time.Date(2026, 10, 12, 15, 4, 0, 0, time.Local)
	if err := s.MarkScheduled(ctx, task.ID, day); err != nil {
		t.Fatal(err)
	}
	enabled, _ = s.ListEnabled(ctx)
	if enabled[0].LastScheduledOn == nil {
		t.Fatal("last_scheduled_on not saved")
	}
	y, m, d := enabled[0].LastScheduledOn.In(time.Local).Date()
	if y != 2026 || m != 10 || d != 12 {
		t.Fatalf("got %v", enabled[0].LastScheduledOn)
	}

	run := &backup.Run{TaskID: task.ID, Trigger: "manual", Status: "running", StartedAt: time.Now()}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	if busy, err := s.HasRunning(ctx, task.ID); err != nil || !busy {
		t.Fatalf("busy=%v err=%v", busy, err)
	}
	mod := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	big := int64(1) << 40
	save := func(status, fp string, size int64) {
		t.Helper()
		if err := s.SaveFileResult(ctx, run.ID, task.ID, backup.FileResult{RelativePath: "a.txt", Status: status, SourceFingerprint: fp, Size: size, ModTime: mod}); err != nil {
			t.Fatal(err)
		}
	}
	save("success", "fp1", 1)
	save("success", "fp1", big)
	save("failed", "fp1", 3)
	save("success", "fp2", 4)
	got, ok, err := s.LatestSuccess(ctx, task.ID, "a.txt", "fp1")
	if err != nil || !ok || got.Size != big || !got.ModTime.UTC().Equal(mod) {
		t.Fatalf("%+v %v %v", got, ok, err)
	}
	if _, ok, err := s.LatestSuccess(ctx, task.ID, "b.txt", "fp1"); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}

	fin := time.Now()
	run.Status, run.FinishedAt, run.SuccessCount = "success", &fin, 2
	if err := s.FinishRun(ctx, *run); err != nil {
		t.Fatal(err)
	}
	if busy, _ := s.HasRunning(ctx, task.ID); busy {
		t.Fatal("still running")
	}
	runs, err := s.ListRuns(ctx, 10)
	if err != nil || len(runs) != 1 || runs[0].Status != "success" || runs[0].SuccessCount != 2 || runs[0].FinishedAt == nil {
		t.Fatalf("%+v %v", runs, err)
	}
}

func TestUpsertCloud(t *testing.T) {
	s := newTestStore(t, "cloud")
	ctx := context.Background()
	if err := s.UpsertCloud(ctx, &CloudAccount{Provider: "webdav", DisplayName: "a", SecretBlob: "x", Status: "disconnected"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertCloud(ctx, &CloudAccount{Provider: "webdav", DisplayName: "b", SecretBlob: "y", Status: "connected"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListClouds(ctx)
	if err != nil || len(list) != 1 || list[0].DisplayName != "b" || list[0].Status != "connected" || list[0].CreatedAt == nil {
		t.Fatalf("%+v %v", list, err)
	}
	got, err := s.GetCloud(ctx, list[0].ID)
	if err != nil || got.SecretBlob != "y" {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestUpdateTask(t *testing.T) {
	s := newTestStore(t, "update_task")
	ctx := context.Background()
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	task := backup.Task{Name: "a", LocalDir: "/tmp", RemoteDir: "/bak", ScheduleTime: "09:00", Enabled: true, CloudAccountID: 1, LastScheduledOn: &day}
	if err := s.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	upd := backup.Task{ID: task.ID, Name: "b", LocalDir: "/var", RemoteDir: "/bak2", ScheduleTime: "10:30", Enabled: false, CloudAccountID: 2}
	if err := s.UpdateTask(ctx, &upd); err != nil {
		t.Fatal(err)
	}
	tasks, err := s.ListTasks(ctx)
	if err != nil || len(tasks) != 1 {
		t.Fatalf("%v %v", tasks, err)
	}
	got := tasks[0]
	if got.Name != "b" || got.LocalDir != "/var" || got.RemoteDir != "/bak2" || got.ScheduleTime != "10:30" || got.Enabled || got.CloudAccountID != 2 {
		t.Fatalf("not updated: %+v", got)
	}
	if got.LastScheduledOn == nil {
		t.Fatal("LastScheduledOn was cleared")
	}
	if err := s.UpdateTask(ctx, &backup.Task{ID: 999}); err == nil {
		t.Fatal("expected not found")
	}
}

func TestUpdateRunProgress(t *testing.T) {
	s := newTestStore(t, "run_progress")
	ctx := context.Background()
	task := backup.Task{Name: "t", LocalDir: "/tmp", RemoteDir: "/bak", ScheduleTime: "09:00", Enabled: true, CloudAccountID: 1}
	if err := s.CreateTask(ctx, &task); err != nil {
		t.Fatal(err)
	}
	run := &backup.Run{TaskID: task.ID, Trigger: "manual", Status: "running", StartedAt: time.Now()}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	run.SuccessCount, run.SkipCount, run.FailCount = 2, 1, 3
	if err := s.UpdateRunProgress(ctx, *run); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetRun(ctx, run.ID)
	if err != nil || got.Status != "running" || got.FinishedAt != nil || got.SuccessCount != 2 || got.SkipCount != 1 || got.FailCount != 3 {
		t.Fatalf("%+v %v", got, err)
	}

	// 已结束的运行不会被进度更新改写。
	fin := time.Now()
	run.Status, run.FinishedAt, run.SuccessCount = "partial", &fin, 5
	if err := s.FinishRun(ctx, *run); err != nil {
		t.Fatal(err)
	}
	run.SuccessCount = 99
	if err := s.UpdateRunProgress(ctx, *run); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetRun(ctx, run.ID)
	if got.SuccessCount != 5 || got.Status != "partial" {
		t.Fatalf("%+v", got)
	}
}

func TestListFileResults(t *testing.T) {
	s := newTestStore(t, "list_file_results")
	ctx := context.Background()
	mod := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	save := func(runID uint, rel, status, errText string) {
		t.Helper()
		if err := s.SaveFileResult(ctx, runID, 1, backup.FileResult{RelativePath: rel, Status: status, Error: errText, SourceFingerprint: "fp", ModTime: mod}); err != nil {
			t.Fatal(err)
		}
	}
	save(1, "z.txt", "success", "")
	save(2, "other.txt", "success", "")
	save(1, "a.txt", "failed", "网盘拒绝")
	got, err := s.ListFileResults(ctx, 1)
	if err != nil || len(got) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	if got[0].RelativePath != "z.txt" || got[1].RelativePath != "a.txt" || got[1].Status != "failed" || got[1].Error != "网盘拒绝" {
		t.Fatalf("order/content %+v", got)
	}
	none, err := s.ListFileResults(ctx, 99)
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("%+v %v", none, err)
	}
}
