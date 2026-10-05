// Package jobs runs the periodic housekeeping: repairing and dropping unfinished uploads,
// ending expired PINs, forgetting old sessions, emptying the trash. The last run of each task
// can be kept outside the process, so a server that only lives for minutes at a time, as on
// Cloud Run, still does its daily work: what is due runs when it starts.
package jobs

import (
	"context"
	"time"
)

// Task is one periodic job.
type Task struct {
	Name  string
	Every time.Duration
	// AtStart runs the task at every start, however recently it ran: repairs after a crash.
	AtStart bool
	Run     func(context.Context) error
}

// Scheduler runs tasks one after another, each at its own interval. A slow task delays the
// others instead of piling up; none of them is urgent.
type Scheduler struct {
	Tasks []Task
	Now   func() time.Time
	Logf  func(string, ...any)
	Tick  time.Duration // how often to look for due tasks; default one minute

	// Last tells when a task last ran without an error, the zero time if never, and Ran
	// records such a run. Without them, a start counts as a long time since every run.
	Last func(ctx context.Context, name string) time.Time
	Ran  func(ctx context.Context, name string, at time.Time)
}

// RunDue runs, one after another, the tasks that run at every start and those whose time has
// come. It is meant for the start, before requests are answered.
func (s *Scheduler) RunDue(ctx context.Context) {
	now := s.Now()
	for _, t := range s.Tasks {
		if last := s.last(ctx, t); t.AtStart || last.IsZero() || !now.Before(last.Add(t.Every)) {
			s.run(ctx, t)
		}
	}
}

// Run blocks until ctx ends, running every task when its interval since its last run is
// over. A task that failed is tried again one interval later, and at the next start.
func (s *Scheduler) Run(ctx context.Context) {
	tick := s.Tick
	if tick == 0 {
		tick = time.Minute
	}
	next := make([]time.Time, len(s.Tasks))
	start := s.Now()
	for i, t := range s.Tasks {
		next[i] = start.Add(t.Every)
		if last := s.last(ctx, t); !last.IsZero() && last.Add(t.Every).Before(next[i]) {
			next[i] = last.Add(t.Every)
		}
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := s.Now()
		for i, t := range s.Tasks {
			if now.Before(next[i]) {
				continue
			}
			s.run(ctx, t)
			next[i] = now.Add(t.Every)
		}
	}
}

// last is when a task last ran without an error, the zero time if that isn't known.
func (s *Scheduler) last(ctx context.Context, t Task) time.Time {
	if s.Last == nil {
		return time.Time{}
	}
	return s.Last(ctx, t.Name)
}

// run runs a task, logs what went wrong, and records it when it didn't.
func (s *Scheduler) run(ctx context.Context, t Task) {
	at := s.Now()
	if err := t.Run(ctx); err != nil {
		if ctx.Err() == nil {
			s.Logf("jobs: %s: %v", t.Name, err)
		}
		return
	}
	if s.Ran != nil {
		s.Ran(ctx, t.Name, at)
	}
}
