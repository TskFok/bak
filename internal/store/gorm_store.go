package store

import (
	"context"
	"time"

	"bak/internal/auth"
	"bak/internal/backup"
	"bak/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	_ auth.UserStore       = (*GormStore)(nil)
	_ backup.Store         = (*GormStore)(nil)
	_ backup.ScheduleStore = (*GormStore)(nil)
)

type GormStore struct {
	db *gorm.DB
}

func NewGorm(db *gorm.DB) *GormStore { return &GormStore{db: db} }

func localNow() *model.LocalTime {
	t := model.LocalTime(time.Now())
	return &t
}

// ---- auth.UserStore ----

func (s *GormStore) Count(ctx context.Context) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Model(&User{}).Count(&n).Error
	})
	return n, err
}

func (s *GormStore) Create(ctx context.Context, user *auth.User) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var n int64
		if err := tx.Model(&User{}).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return auth.ErrUserExists
		}
		row := User{Username: user.Username, PasswordHash: user.PasswordHash}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		user.ID = row.ID
		return nil
	})
}

func (s *GormStore) FindByUsername(ctx context.Context, username string) (auth.User, error) {
	var row User
	if err := s.db.WithContext(ctx).Where("username = ?", username).First(&row).Error; err != nil {
		return auth.User{}, err
	}
	return auth.User{ID: row.ID, Username: row.Username, PasswordHash: row.PasswordHash}, nil
}

// ---- backup.Store ----

func (s *GormStore) HasRunning(ctx context.Context, taskID uint) (bool, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&backupRun{}).
		Where("task_id = ? AND status = ?", taskID, "running").Count(&n).Error
	return n > 0, err
}

func (s *GormStore) CreateRun(ctx context.Context, run *backup.Run) error {
	row := runToRow(*run)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	run.ID = row.ID
	return nil
}

func (s *GormStore) FinishRun(ctx context.Context, run backup.Run) error {
	return s.db.WithContext(ctx).Model(&backupRun{}).Where("id = ?", run.ID).Updates(map[string]interface{}{
		"status":        run.Status,
		"finished_at":   run.FinishedAt,
		"success_count": run.SuccessCount,
		"skip_count":    run.SkipCount,
		"fail_count":    run.FailCount,
		"error_summary": run.ErrorSummary,
		"updated_at":    localNow(),
	}).Error
}

// UpdateRunProgress 只在运行仍为 running 时更新计数，不写 finished_at。
func (s *GormStore) UpdateRunProgress(ctx context.Context, run backup.Run) error {
	return s.db.WithContext(ctx).Model(&backupRun{}).Where("id = ? AND status = ?", run.ID, "running").Updates(map[string]interface{}{
		"success_count": run.SuccessCount,
		"skip_count":    run.SkipCount,
		"fail_count":    run.FailCount,
		"updated_at":    localNow(),
	}).Error
}

// ListFileResults 按 id 升序返回某次运行的逐文件结果。
func (s *GormStore) ListFileResults(ctx context.Context, runID uint) ([]backup.FileResult, error) {
	var rows []backupFileResult
	if err := s.db.WithContext(ctx).Where("run_id = ?", runID).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]backup.FileResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, backup.FileResult{
			RelativePath:      r.RelativePath,
			Status:            r.Status,
			Error:             r.Error,
			SourceFingerprint: r.SourceFingerprint,
			Size:              r.Size,
			ModTime:           r.ModTime,
		})
	}
	return out, nil
}

func (s *GormStore) LatestSuccess(ctx context.Context, taskID uint, rel, fingerprint string) (backup.FileResult, bool, error) {
	var rows []backupFileResult
	err := s.db.WithContext(ctx).
		Where("task_id = ? AND relative_path = ? AND source_fingerprint = ? AND status = ?", taskID, rel, fingerprint, "success").
		Order("id DESC").Limit(1).Find(&rows).Error
	if err != nil || len(rows) == 0 {
		return backup.FileResult{}, false, err
	}
	r := rows[0]
	return backup.FileResult{
		RelativePath:      r.RelativePath,
		Status:            r.Status,
		Error:             r.Error,
		SourceFingerprint: r.SourceFingerprint,
		Size:              r.Size,
		ModTime:           r.ModTime,
	}, true, nil
}

func (s *GormStore) SaveFileResult(ctx context.Context, runID, taskID uint, result backup.FileResult) error {
	row := backupFileResult{
		RunID:             runID,
		TaskID:            taskID,
		RelativePath:      result.RelativePath,
		Size:              result.Size,
		ModTime:           result.ModTime,
		Status:            result.Status,
		Error:             result.Error,
		SourceFingerprint: result.SourceFingerprint,
	}
	return s.db.WithContext(ctx).Create(&row).Error
}

// ---- backup.ScheduleStore ----

func (s *GormStore) ListEnabled(ctx context.Context) ([]backup.Task, error) {
	var rows []backupTask
	if err := s.db.WithContext(ctx).Where("enabled = ?", true).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return tasksFromRows(rows), nil
}

func (s *GormStore) MarkScheduled(ctx context.Context, taskID uint, day time.Time) error {
	y, m, d := day.Date()
	midnight := time.Date(y, m, d, 0, 0, 0, 0, day.Location())
	return s.db.WithContext(ctx).Model(&backupTask{}).Where("id = ?", taskID).Updates(map[string]interface{}{
		"last_scheduled_on": midnight,
		"updated_at":        localNow(),
	}).Error
}

func (s *GormStore) FailStaleRuns(ctx context.Context) error {
	return s.db.WithContext(ctx).Model(&backupRun{}).Where("status = ?", "running").Updates(map[string]interface{}{
		"status":        "failed",
		"error_summary": backup.FailStaleMessage(),
		"updated_at":    localNow(),
	}).Error
}

// ---- 供 HTTP 层使用 ----

func (s *GormStore) CreateTask(ctx context.Context, task *backup.Task) error {
	row := taskToRow(*task)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	task.ID = row.ID
	return nil
}

// UpdateTask 更新任务的可编辑字段，保留 last_scheduled_on；任务不存在时返回 gorm.ErrRecordNotFound。
func (s *GormStore) UpdateTask(ctx context.Context, task *backup.Task) error {
	var row backupTask
	if err := s.db.WithContext(ctx).First(&row, task.ID).Error; err != nil {
		return err
	}
	return s.db.WithContext(ctx).Model(&row).Updates(map[string]interface{}{
		"name":             task.Name,
		"local_dir":        task.LocalDir,
		"cloud_account_id": task.CloudAccountID,
		"remote_dir":       task.RemoteDir,
		"schedule_time":    task.ScheduleTime,
		"enabled":          task.Enabled,
		"updated_at":       localNow(),
	}).Error
}

// DeleteTask 只删除任务行，运行记录和文件结果保留。
func (s *GormStore) DeleteTask(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).Delete(&backupTask{}, id).Error
}

func (s *GormStore) ListTasks(ctx context.Context) ([]backup.Task, error) {
	var rows []backupTask
	if err := s.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return tasksFromRows(rows), nil
}

func (s *GormStore) GetRun(ctx context.Context, id uint) (backup.Run, error) {
	var row backupRun
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return backup.Run{}, err
	}
	return runFromRow(row), nil
}

// ListRuns 按开始时间倒序返回运行记录，limit <= 0 表示不限制。
func (s *GormStore) ListRuns(ctx context.Context, limit int) ([]backup.Run, error) {
	q := s.db.WithContext(ctx).Order("id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var rows []backupRun
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	runs := make([]backup.Run, 0, len(rows))
	for _, r := range rows {
		runs = append(runs, runFromRow(r))
	}
	return runs, nil
}

// UpsertCloud 按 provider 插入或更新网盘账号。
func (s *GormStore) UpsertCloud(ctx context.Context, acct *CloudAccount) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider"}},
		DoUpdates: clause.AssignmentColumns([]string{"display_name", "secret_blob", "status", "last_error", "updated_at"}),
	}).Create(acct).Error
}

func (s *GormStore) ListClouds(ctx context.Context) ([]CloudAccount, error) {
	var rows []CloudAccount
	err := s.db.WithContext(ctx).Order("id").Find(&rows).Error
	return rows, err
}

func (s *GormStore) GetCloud(ctx context.Context, id uint) (CloudAccount, error) {
	var row CloudAccount
	err := s.db.WithContext(ctx).First(&row, id).Error
	return row, err
}

// ---- 转换 ----

func taskToRow(t backup.Task) backupTask {
	return backupTask{
		ID: t.ID, Name: t.Name, LocalDir: t.LocalDir, CloudAccountID: t.CloudAccountID,
		RemoteDir: t.RemoteDir, ScheduleTime: t.ScheduleTime, Enabled: t.Enabled,
		LastScheduledOn: t.LastScheduledOn,
	}
}

func tasksFromRows(rows []backupTask) []backup.Task {
	tasks := make([]backup.Task, 0, len(rows))
	for _, r := range rows {
		tasks = append(tasks, backup.Task{
			ID: r.ID, CloudAccountID: r.CloudAccountID, Name: r.Name, LocalDir: r.LocalDir,
			RemoteDir: r.RemoteDir, ScheduleTime: r.ScheduleTime, Enabled: r.Enabled,
			LastScheduledOn: r.LastScheduledOn,
		})
	}
	return tasks
}

func runToRow(r backup.Run) backupRun {
	return backupRun{
		ID: r.ID, TaskID: r.TaskID, Trigger: r.Trigger, Status: r.Status,
		StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		SuccessCount: r.SuccessCount, SkipCount: r.SkipCount, FailCount: r.FailCount,
		ErrorSummary: r.ErrorSummary,
	}
}

func runFromRow(r backupRun) backup.Run {
	return backup.Run{
		ID: r.ID, TaskID: r.TaskID, Trigger: r.Trigger, Status: r.Status,
		ErrorSummary: r.ErrorSummary, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt,
		SuccessCount: r.SuccessCount, SkipCount: r.SkipCount, FailCount: r.FailCount,
	}
}
