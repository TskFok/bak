package backup

import (
	"context"
	"errors"
	"time"
)

type ScheduleStore interface {
	ListEnabled(ctx context.Context) ([]Task, error)
	HasRunning(ctx context.Context, taskID uint) (bool, error)
	MarkScheduled(ctx context.Context, taskID uint, day time.Time) error
	FailStaleRuns(ctx context.Context) error
}

func FailStaleMessage() string { return "进程已退出" }

func ShouldSchedule(now time.Time, task Task, running bool) bool {
	if !task.Enabled || running {
		return false
	}
	clock, err := time.ParseInLocation("15:04", task.ScheduleTime, now.Location())
	if err != nil {
		return false
	}
	due := time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, now.Location())
	if now.Before(due) {
		return false
	}
	if task.LastScheduledOn != nil {
		ly, lm, ld := task.LastScheduledOn.In(now.Location()).Date()
		ny, nm, nd := now.Date()
		if ly == ny && lm == nm && ld == nd {
			return false
		}
	}
	return true
}

func Tick(ctx context.Context, st ScheduleStore, run func(ctx context.Context, task Task) error, now time.Time) error {
	tasks, err := st.ListEnabled(ctx)
	if err != nil {
		return err
	}
	var runErrs []error
	for _, task := range tasks {
		running, err := st.HasRunning(ctx, task.ID)
		if err != nil {
			return err
		}
		if !ShouldSchedule(now, task, running) {
			continue
		}
		if err := st.MarkScheduled(ctx, task.ID, now); err != nil {
			return err
		}
		if err := run(ctx, task); err != nil && !errors.Is(err, ErrBusy) {
			runErrs = append(runErrs, err)
		}
	}
	return errors.Join(runErrs...)
}
