// Package checksum works out each library file's CRC-32, which a ZIP download has to send
// before the file's bytes. A worker does it soon after each upload, at a pace that leaves the
// drive to uploads and downloads; a download that needs one sooner gets it at full speed.
package checksum

import (
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/db"
)

// Pace is how fast the worker reads files in the background, in bytes per second.
const Pace = 16 << 20

// Store keeps the CRC-32s of the library's files in the database.
type Store struct {
	DB   *db.DB
	Root *os.Root // the storage folder
	Pace int64    // bytes per second for the worker; 0 reads at full speed
	Logf func(format string, args ...any)

	mu      sync.Mutex
	running map[string]*call
	failed  map[string]int // read errors per file, so the worker doesn't try one forever
	wake    chan struct{}
}

// call is one file's CRC-32 being worked out; others who need the same file wait for it.
type call struct {
	done  chan struct{}
	crc   uint32
	err   error
	hurry chan struct{} // closed when someone waits: no more pacing
	once  sync.Once
}

func (c *call) rush() { c.once.Do(func() { close(c.hurry) }) }

func (c *call) rushed() bool {
	select {
	case <-c.hurry:
		return true
	default:
		return false
	}
}

// tries is how often the worker reads a file that fails before it leaves it to downloads.
const tries = 3

// Wake tells the worker that a file arrived. It never blocks, so it can be the library's
// OnReady.
func (s *Store) Wake(string) {
	select {
	case s.wakeChan() <- struct{}{}:
	default:
	}
}

func (s *Store) wakeChan() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	return s.wake
}

// Run works out missing CRC-32s until ctx ends: after each upload, and once a minute for
// anything left over.
func (s *Store) Run(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	wake := s.wakeChan()
	for {
		for {
			n, err := s.DoPending(ctx)
			if err != nil {
				if ctx.Err() == nil {
					s.Logf("checksum: %v", err)
				}
				break
			}
			if n == 0 {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-wake:
		}
	}
}

// DoPending works out the CRC-32 of a batch of files that have none, the newest first, and
// returns how many it got. A file that can't be read is tried again a few times later on, and
// then left to the downloads that need it.
func (s *Store) DoPending(ctx context.Context) (int, error) {
	const batch = 50
	files, err := s.DB.CRCCandidates(ctx, batch+s.givenUpCount())
	if err != nil {
		return 0, err
	}
	done, tried := 0, 0
	for _, f := range files {
		if s.givenUp(f.ID) || tried == batch {
			continue
		}
		tried++
		if _, err := s.get(ctx, f, false); err != nil {
			if ctx.Err() != nil {
				return done, ctx.Err()
			}
			s.Logf("checksum: %s: %v", f.RelPath, err)
			s.fail(f.ID)
			continue
		}
		done++
	}
	return done, nil
}

// Get returns a file's CRC-32, working it out now at full speed if it isn't known yet.
func (s *Store) Get(ctx context.Context, f db.File) (uint32, error) {
	if f.CRC32 != nil {
		return *f.CRC32, nil
	}
	return s.get(ctx, f, true)
}

// get works out f's CRC-32 once, however many ask for it at the same time. If the one who
// started it went away, the next one starts again.
func (s *Store) get(ctx context.Context, f db.File, hurry bool) (uint32, error) {
	for {
		s.mu.Lock()
		c, ok := s.running[f.ID]
		if !ok {
			if s.running == nil {
				s.running = map[string]*call{}
			}
			c = &call{done: make(chan struct{}), hurry: make(chan struct{})}
			s.running[f.ID] = c
		}
		s.mu.Unlock()
		if hurry {
			c.rush()
		}
		if !ok {
			c.crc, c.err = s.compute(ctx, f, c)
			s.mu.Lock()
			delete(s.running, f.ID)
			s.mu.Unlock()
			close(c.done)
			return c.crc, c.err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-c.done:
		}
		if c.err == nil || !(errors.Is(c.err, context.Canceled) || errors.Is(c.err, context.DeadlineExceeded)) {
			return c.crc, c.err
		}
	}
}

// compute reads f and stores its CRC-32, unless it is known already.
func (s *Store) compute(ctx context.Context, f db.File, c *call) (uint32, error) {
	if crc, ok, err := s.DB.FileCRC32(ctx, f.ID); err != nil || ok {
		return crc, err
	}
	file, err := s.Root.Open(f.RelPath)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	h := crc32.NewIEEE()
	buf := make([]byte, 1<<20)
	start, read := time.Now(), int64(0)
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		n, err := file.Read(buf)
		h.Write(buf[:n])
		read += int64(n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, err
		}
		if s.Pace > 0 && !c.rushed() {
			ahead := time.Duration(float64(read)/float64(s.Pace)*float64(time.Second)) - time.Since(start)
			if ahead > 0 {
				timer := time.NewTimer(ahead)
				select {
				case <-ctx.Done():
				case <-c.hurry:
				case <-timer.C:
				}
				timer.Stop()
			}
		}
	}
	if read != f.Size {
		return 0, fmt.Errorf("%s has %d bytes, not %d", f.RelPath, read, f.Size)
	}
	crc := h.Sum32()
	if err := s.DB.SetCRC32(context.WithoutCancel(ctx), f.ID, crc); err != nil {
		return 0, err
	}
	s.mu.Lock()
	delete(s.failed, f.ID)
	s.mu.Unlock()
	return crc, nil
}

func (s *Store) fail(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failed == nil {
		s.failed = map[string]int{}
	}
	s.failed[id]++
}

func (s *Store) givenUp(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed[id] >= tries
}

func (s *Store) givenUpCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range s.failed {
		if k >= tries {
			n++
		}
	}
	return n
}
