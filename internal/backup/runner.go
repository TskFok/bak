package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"BaiduYunPanBak/internal/provider"
)

var ErrBusy = errors.New("该任务正在备份")

type Task struct {
	ID               uint
	CloudAccountID   uint
	Name             string
	LocalDir         string
	RemoteDir        string
	ScheduleTime     string
	Enabled          bool
	LastScheduledOn  *time.Time
}

type Run struct {
	ID            uint
	TaskID        uint
	Trigger       string
	Status        string
	ErrorSummary  string
	StartedAt     time.Time
	FinishedAt    *time.Time
	SuccessCount  int
	SkipCount     int
	FailCount     int
}

type FileResult struct {
	RelativePath      string
	Status            string
	Error             string
	SourceFingerprint string
	Size              int64
	ModTime           time.Time
}

type Store interface {
	HasRunning(ctx context.Context, taskID uint) (bool, error)
	CreateRun(ctx context.Context, run *Run) error
	FinishRun(ctx context.Context, run Run) error
	LatestSuccess(ctx context.Context, taskID uint, rel, fingerprint string) (FileResult, bool, error)
	SaveFileResult(ctx context.Context, runID, taskID uint, result FileResult) error
}

func Fingerprint(localDir string, accountID uint, remoteDir string) string {
	sum := sha256.Sum256([]byte(localDir + "\n" + strconv.FormatUint(uint64(accountID), 10) + "\n" + remoteDir))
	return hex.EncodeToString(sum[:])
}

func FinalStatus(success, skip, fail int) string {
	if fail == 0 {
		return "success"
	}
	if success == 0 && skip == 0 {
		return "failed"
	}
	return "partial"
}

func RunTask(ctx context.Context, store Store, task Task, p provider.Provider, trigger string, now time.Time) (Run, error) {
	busy, err := store.HasRunning(ctx, task.ID)
	if err != nil {
		return Run{}, err
	}
	if busy {
		return Run{}, ErrBusy
	}
	run := &Run{TaskID: task.ID, Trigger: trigger, Status: "running", StartedAt: now}
	if err := store.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	info, err := os.Stat(task.LocalDir)
	if err != nil || !info.IsDir() {
		return finishFailed(ctx, store, *run, now, "本地目录不存在或不可读")
	}
	fp := Fingerprint(task.LocalDir, task.CloudAccountID, task.RemoteDir)
	if err := p.EnsureDir(ctx, task.RemoteDir); err != nil {
		return finishFailed(ctx, store, *run, now, "无法准备网盘目录")
	}
	err = filepath.WalkDir(task.LocalDir, func(local string, d fs.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(task.LocalDir, local)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if walkErr != nil || d.IsDir() {
			if walkErr != nil {
				run.FailCount++
				_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "failed", Error: "无法读取", SourceFingerprint: fp})
			}
			return nil
		}
		st, statErr := d.Info()
		if statErr != nil {
			run.FailCount++
			_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "failed", Error: "无法读取", SourceFingerprint: fp})
			return nil
		}
		mod := st.ModTime().UTC().Truncate(time.Second)
		prev, ok, _ := store.LatestSuccess(ctx, task.ID, rel, fp)
		if ok && prev.Size == st.Size() && prev.ModTime.UTC().Truncate(time.Second).Equal(mod) {
			run.SkipCount++
			_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "skipped", Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
			return nil
		}
		remote := path.Join(task.RemoteDir, rel)
		if upErr := p.Upload(ctx, local, remote); upErr != nil {
			run.FailCount++
			_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "failed", Error: upErr.Error(), Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
			return nil
		}
		run.SuccessCount++
		_ = store.SaveFileResult(ctx, run.ID, task.ID, FileResult{RelativePath: rel, Status: "success", Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
		return nil
	})
	if err != nil {
		return finishFailed(ctx, store, *run, now, "扫描失败")
	}
	done := now
	run.FinishedAt = &done
	run.Status = FinalStatus(run.SuccessCount, run.SkipCount, run.FailCount)
	if err := store.FinishRun(ctx, *run); err != nil {
		return Run{}, err
	}
	return *run, nil
}

func finishFailed(ctx context.Context, store Store, run Run, now time.Time, summary string) (Run, error) {
	done := now
	run.FinishedAt = &done
	run.Status = "failed"
	run.ErrorSummary = summary
	if err := store.FinishRun(ctx, run); err != nil {
		return Run{}, err
	}
	return run, nil
}
