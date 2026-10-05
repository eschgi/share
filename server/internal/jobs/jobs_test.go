package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// runs keeps the last runs, as the database does for the server.
type runs struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (r *runs) Last(_ context.Context, name string) time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last[name]
}

func (r *runs) Ran(_ context.Context, name string, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.last[name] = at
}

var t0 = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func TestRunDue(t *testing.T) {
	c := &clock{t: t0}
	var ran []string
	task := func(name string, every time.Duration, atStart bool) Task {
		return Task{Name: name, Every: every, AtStart: atStart, Run: func(context.Context) error {
			ran = append(ran, name)
			return nil
		}}
	}
	r := &runs{last: map[string]time.Time{}}
	s := &Scheduler{Now: c.Now, Logf: t.Logf, Last: r.Last, Ran: r.Ran, Tasks: []Task{
		task("repair", 5*time.Minute, true),
		task("hourly", time.Hour, false),
		task("daily", 24*time.Hour, false),
	}}
	ctx := context.Background()
	for _, step := range []struct {
		after time.Duration
		want  []string
	}{
		{0, []string{"repair", "hourly", "daily"}}, // nothing ran before
		{10 * time.Minute, []string{"repair"}},     // started again a little later
		{time.Hour, []string{"repair", "hourly"}},
		{23 * time.Hour, []string{"repair", "hourly", "daily"}},
	} {
		c.Add(step.after)
		ran = nil
		s.RunDue(ctx)
		if !slices.Equal(ran, step.want) {
			t.Errorf("after %v more: ran %v, want %v", step.after, ran, step.want)
		}
	}
	if got := r.Last(ctx, "daily"); !got.Equal(c.Now()) {
		t.Errorf("the daily task's run is noted at %v", got)
	}
}

func TestFailedTasksAreTriedAgain(t *testing.T) {
	c := &clock{t: t0}
	r := &runs{last: map[string]time.Time{}}
	tries := 0
	var logged []string
	s := &Scheduler{Now: c.Now, Last: r.Last, Ran: r.Ran,
		Logf: func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) },
		Tasks: []Task{{Name: "daily", Every: 24 * time.Hour, Run: func(context.Context) error {
			if tries++; tries == 1 {
				return errors.New("the database is away")
			}
			return nil
		}}},
	}
	ctx := context.Background()
	s.RunDue(ctx)
	if len(logged) != 1 || !strings.Contains(logged[0], "daily: the database is away") || !r.Last(ctx, "daily").IsZero() {
		t.Fatalf("after a failure: logged %q, last run %v", logged, r.Last(ctx, "daily"))
	}
	c.Add(time.Minute)
	s.RunDue(ctx) // the next start
	if tries != 2 || !r.Last(ctx, "daily").Equal(c.Now()) {
		t.Errorf("tried %d times, last run %v", tries, r.Last(ctx, "daily"))
	}
}

func TestRunGoesOnFromTheLastRun(t *testing.T) {
	c := &clock{t: t0}
	r := &runs{last: map[string]time.Time{"hourly": t0.Add(-50 * time.Minute)}}
	ran := make(chan string, 10)
	task := func(name string, every time.Duration) Task {
		return Task{Name: name, Every: every, Run: func(context.Context) error {
			ran <- name
			return nil
		}}
	}
	s := &Scheduler{Now: c.Now, Logf: t.Logf, Tick: time.Millisecond, Last: r.Last, Ran: r.Ran, Tasks: []Task{
		task("hourly", time.Hour),
		task("daily", 24*time.Hour), // never ran: one interval after the start, as before
	}}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(stopped)
	}()
	defer func() {
		cancel()
		<-stopped
	}()
	select {
	case name := <-ran:
		t.Fatalf("%s ran at once", name)
	case <-time.After(30 * time.Millisecond):
	}
	c.Add(11 * time.Minute) // an hour since the hourly task's last run
	select {
	case name := <-ran:
		if name != "hourly" {
			t.Fatalf("%s ran", name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hourly task didn't run")
	}
	select {
	case name := <-ran:
		t.Fatalf("%s ran too", name)
	case <-time.After(30 * time.Millisecond):
	}
}
