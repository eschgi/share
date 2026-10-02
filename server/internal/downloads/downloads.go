// Package downloads keeps what people asked to download as one ZIP, so that the browser can
// fetch it with a plain link and resume it, for a day after it was last used. Only the files'
// ids are kept, in the archive's order; names and sizes come from the library each time.
package downloads

import (
	"sort"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/ids"
)

const (
	// Keep is how long a selection lasts after it was last used.
	Keep = 24 * time.Hour
	// A person's oldest selections go beyond this many, and the oldest of all beyond total.
	perPerson = 10
	total     = 50
	// ZIPs being sent at once, per person and in all: each one keeps the drive busy.
	streamsPerPerson = 2
	streamsTotal     = 4
)

// Selection is one ZIP someone asked for.
type Selection struct {
	ID      string
	UserID  string
	FileIDs []string // in the archive's order
	Name    string   // the ZIP's file name
	used    time.Time
}

// Store keeps selections in memory: after a restart, a download starts over.
type Store struct {
	Now func() time.Time

	mu        sync.Mutex
	sel       map[string]*Selection
	streams   map[string]int
	streaming int
}

// Add keeps a new selection for userID.
func (s *Store) Add(userID string, fileIDs []string, name string) *Selection {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sel == nil {
		s.sel = map[string]*Selection{}
	}
	sel := &Selection{ID: ids.New(), UserID: userID, FileIDs: fileIDs, Name: name, used: s.Now()}
	s.sel[sel.ID] = sel
	s.trim(func(o *Selection) bool { return o.UserID == userID }, perPerson)
	s.trim(func(*Selection) bool { return true }, total)
	return sel
}

// trim drops the least recently used of the selections that match, beyond max.
func (s *Store) trim(match func(*Selection) bool, max int) {
	var list []*Selection
	for _, o := range s.sel {
		if match(o) {
			list = append(list, o)
		}
	}
	if len(list) <= max {
		return
	}
	sort.Slice(list, func(i, j int) bool { return list[i].used.Before(list[j].used) })
	for _, o := range list[:len(list)-max] {
		delete(s.sel, o.ID)
	}
}

// Get returns userID's selection id, and counts it as used.
func (s *Store) Get(id, userID string) (*Selection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sel, ok := s.sel[id]
	now := s.Now()
	if !ok || sel.UserID != userID || now.Sub(sel.used) >= Keep {
		return nil, false
	}
	sel.used = now
	return sel, true
}

// Prune forgets selections nobody used for Keep.
func (s *Store) Prune() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.Now()
	for id, sel := range s.sel {
		if now.Sub(sel.used) >= Keep {
			delete(s.sel, id)
		}
	}
}

// Stream takes a slot for sending a ZIP to userID, if one is free; release gives it back.
func (s *Store) Stream(userID string) (release func(), ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streams == nil {
		s.streams = map[string]int{}
	}
	if s.streaming >= streamsTotal || s.streams[userID] >= streamsPerPerson {
		return nil, false
	}
	s.streaming++
	s.streams[userID]++
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.streaming--
			if s.streams[userID]--; s.streams[userID] == 0 {
				delete(s.streams, userID)
			}
		})
	}, true
}
