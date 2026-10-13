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

	"bak/internal/provider"
)

var (
	ErrBusy              = errors.New("该任务正在备份")
	errRootDirUnreadable = errors.New("root dir unreadable")
)

type Task struct {
	ID              uint
	CloudAccountID  uint
	Name            string
	LocalDir        string
	RemoteDir       string
	ScheduleTime    string
	Enabled         bool
	LastScheduledOn *time.Time
}

type Run struct {
	ID           uint
	TaskID       uint
	Trigger      string
	Status       string
	ErrorSummary string
	StartedAt    time.Time
	FinishedAt   *time.Time
	SuccessCount int
	SkipCount    int
	FailCount    int
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
	// UpdateRunProgress 在运行期间保存当前计数，不改变状态与结束时间。
	UpdateRunProgress(ctx context.Context, run Run) error
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

// RunTask 同步执行一次备份：检查是否已有运行、创建运行记录、扫描上传并收尾。
func RunTask(ctx context.Context, store Store, task Task, p provider.Provider, trigger string, now time.Time) (Run, error) {
	run, err := StartRun(ctx, store, task, trigger, now)
	if err != nil {
		return Run{}, err
	}
	return Execute(ctx, store, task, p, run)
}

// StartRun 检查任务是否已在运行，没有则创建一条 running 记录并返回（含真实 ID）。
func StartRun(ctx context.Context, store Store, task Task, trigger string, now time.Time) (Run, error) {
	busy, err := store.HasRunning(ctx, task.ID)
	if err != nil {
		return Run{}, err
	}
	if busy {
		return Run{}, ErrBusy
	}
	run := Run{TaskID: task.ID, Trigger: trigger, Status: "running", StartedAt: now}
	if err := store.CreateRun(ctx, &run); err != nil {
		return Run{}, err
	}
	return run, nil
}

// Execute 在 StartRun 创建的 running 记录上扫描并上传，最后 FinishRun。
// 它不再检查 HasRunning（否则会看到自己的 running 记录）。
// 每处理完一个文件会保存当前计数，保存失败不影响备份。
func Execute(ctx context.Context, store Store, task Task, p provider.Provider, started Run) (Run, error) {
	run := &started
	info, err := os.Stat(task.LocalDir)
	if err != nil || !info.IsDir() {
		return finishFailed(ctx, store, *run, "本地目录不存在或不可读")
	}
	fp := Fingerprint(task.LocalDir, task.CloudAccountID, task.RemoteDir)
	if err := p.EnsureDir(ctx, task.RemoteDir); err != nil {
		return finishFailed(ctx, store, *run, "无法准备网盘目录")
	}
	record := func(res FileResult) {
		_ = store.SaveFileResult(ctx, run.ID, task.ID, res)
		_ = store.UpdateRunProgress(ctx, *run)
	}
	err = filepath.WalkDir(task.LocalDir, func(local string, d fs.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(task.LocalDir, local)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			if walkErr != nil {
				return errRootDirUnreadable
			}
			return nil
		}
		rel = filepath.ToSlash(rel)
		if walkErr != nil || d.IsDir() {
			if walkErr != nil {
				run.FailCount++
				record(FileResult{RelativePath: rel, Status: "failed", Error: "无法读取", SourceFingerprint: fp})
			}
			return nil
		}
		st, statErr := d.Info()
		if statErr != nil {
			run.FailCount++
			record(FileResult{RelativePath: rel, Status: "failed", Error: "无法读取", SourceFingerprint: fp})
			return nil
		}
		mod := st.ModTime().UTC().Truncate(time.Second)
		prev, ok, _ := store.LatestSuccess(ctx, task.ID, rel, fp)
		if ok && prev.Size == st.Size() && prev.ModTime.UTC().Truncate(time.Second).Equal(mod) {
			run.SkipCount++
			record(FileResult{RelativePath: rel, Status: "skipped", Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
			return nil
		}
		remote := path.Join(task.RemoteDir, rel)
		if upErr := p.Upload(ctx, local, remote); upErr != nil {
			run.FailCount++
			record(FileResult{RelativePath: rel, Status: "failed", Error: upErr.Error(), Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
			return nil
		}
		run.SuccessCount++
		record(FileResult{RelativePath: rel, Status: "success", Size: st.Size(), ModTime: mod, SourceFingerprint: fp})
		return nil
	})
	if err != nil {
		if errors.Is(err, errRootDirUnreadable) {
			return finishFailed(ctx, store, *run, "本地目录不存在或不可读")
		}
		return finishFailed(ctx, store, *run, "扫描失败")
	}
	done := time.Now()
	run.FinishedAt = &done
	run.Status = FinalStatus(run.SuccessCount, run.SkipCount, run.FailCount)
	if err := store.FinishRun(ctx, *run); err != nil {
		return Run{}, err
	}
	return *run, nil
}

func finishFailed(ctx context.Context, store Store, run Run, summary string) (Run, error) {
	done := time.Now()
	run.FinishedAt = &done
	run.Status = "failed"
	run.ErrorSummary = summary
	if err := store.FinishRun(ctx, run); err != nil {
		return Run{}, err
	}
	return run, nil
}
