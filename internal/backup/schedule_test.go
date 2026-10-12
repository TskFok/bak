package backup

import (
	"context"
	"testing"
	"time"
)

func TestShouldSchedule(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, loc)
	task := Task{Enabled: true, ScheduleTime: "09:30"}
	if ShouldSchedule(now, task, false) {
		t.Fatal("too early")
	}
	now = time.Date(2026, 10, 12, 9, 30, 0, 0, loc)
	if !ShouldSchedule(now, task, false) {
		t.Fatal("due")
	}
	if ShouldSchedule(now, task, true) {
		t.Fatal("running")
	}
	today := now
	task.LastScheduledOn = &today
	if ShouldSchedule(now, task, false) {
		t.Fatal("already today")
	}
}

type schedMem struct {
	tasks   []Task
	running map[uint]bool
	marked  []uint
	stale   int
}

func (s *schedMem) ListEnabled(ctx context.Context) ([]Task, error) { return s.tasks, nil }
func (s *schedMem) HasRunning(ctx context.Context, taskID uint) (bool, error) {
	return s.running[taskID], nil
}
func (s *schedMem) MarkScheduled(ctx context.Context, taskID uint, day time.Time) error {
	s.marked = append(s.marked, taskID)
	return nil
}
func (s *schedMem) FailStaleRuns(ctx context.Context) error { s.stale++; return nil }

func TestTickSkipsBusyAndManualDoesNotMark(t *testing.T) {
	now := time.Date(2026, 10, 12, 10, 0, 0, 0, time.Local)
	st := &schedMem{tasks: []Task{{ID: 1, Enabled: true, ScheduleTime: "09:00"}, {ID: 2, Enabled: true, ScheduleTime: "09:00"}}, running: map[uint]bool{1: true}}
	err := Tick(context.Background(), st, func(ctx context.Context, task Task) error {
		if task.ID == 1 {
			return ErrBusy
		}
		return nil
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.marked) != 1 || st.marked[0] != 2 {
		t.Fatalf("marked %v", st.marked)
	}
}

func TestFailStaleMessage(t *testing.T) {
	if FailStaleMessage() != "进程已退出" {
		t.Fatal(FailStaleMessage())
	}
}
