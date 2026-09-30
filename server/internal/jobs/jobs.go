// Package jobs runs the periodic housekeeping: repairing and dropping unfinished uploads,
// ending expired PINs, forgetting old sessions.
package jobs

import (
	"context"
	"time"
)

// Task is one periodic job.
type Task struct {
	Name  string
	Every time.Duration
	Run   func(context.Context) error
}

// Scheduler runs tasks one after another, each at its own interval. A slow task delays the
// others instead of piling up; none of them is urgent.
type Scheduler struct {
	Tasks []Task
	Now   func() time.Time
	Logf  func(string, ...any)
	Tick  time.Duration // how often to look for due tasks; default one minute
}

// Run blocks until ctx ends. Every task first runs one interval after the start.
func (s *Scheduler) Run(ctx context.Context) {
	tick := s.Tick
	if tick == 0 {
		tick = time.Minute
	}
	next := make([]time.Time, len(s.Tasks))
	start := s.Now()
	for i, t := range s.Tasks {
		next[i] = start.Add(t.Every)
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
			if err := t.Run(ctx); err != nil && ctx.Err() == nil {
				s.Logf("jobs: %s: %v", t.Name, err)
			}
			next[i] = now.Add(t.Every)
		}
	}
}
