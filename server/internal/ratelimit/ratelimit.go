// Package ratelimit counts failures per key and blocks a key for a while once it has failed
// too often, e.g. wrong PINs per phone, per IP address and in total.
package ratelimit

import (
	"sync"
	"time"
)

// Limiter allows Max failures per key within Window; the next one blocks the key for Block.
// A success doesn't erase failures, except through Reset.
type Limiter struct {
	Max    int
	Window time.Duration
	Block  time.Duration

	mu      sync.Mutex
	entries map[string]*entry
}

type entry struct {
	windowStart  time.Time
	failures     int
	blockedUntil time.Time
}

// New returns a limiter.
func New(max int, window, block time.Duration) *Limiter {
	return &Limiter{Max: max, Window: window, Block: block, entries: map[string]*entry{}}
}

// Check reports whether key is blocked at now, and for how much longer.
func (l *Limiter) Check(key string, now time.Time) (blocked bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil || !now.Before(e.blockedUntil) {
		return false, 0
	}
	return true, e.blockedUntil.Sub(now)
}

// Fail records a failure for key. It returns how many failures are left before the key is
// blocked, and, if this failure blocked it, for how long.
func (l *Limiter) Fail(key string, now time.Time) (left int, blockedFor time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[key]
	if e == nil {
		e = &entry{}
		l.entries[key] = e
	}
	if now.Before(e.blockedUntil) {
		return 0, e.blockedUntil.Sub(now)
	}
	if e.failures == 0 || now.Sub(e.windowStart) >= l.Window {
		e.windowStart, e.failures = now, 0
	}
	e.failures++
	if e.failures >= l.Max {
		e.blockedUntil = now.Add(l.Block)
		e.failures = 0
		return 0, l.Block
	}
	return l.Max - e.failures, 0
}

// Reset forgets the failures of key, e.g. after the right PIN.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.entries, key)
}

// Prune drops keys with nothing left to remember, so memory stays flat.
func (l *Limiter) Prune(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for k, e := range l.entries {
		if !now.Before(e.blockedUntil) && (e.failures == 0 || now.Sub(e.windowStart) >= l.Window) {
			delete(l.entries, k)
		}
	}
}

// Len reports how many keys are remembered.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
