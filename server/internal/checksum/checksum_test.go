package checksum

import (
	"context"
	"crypto/rand"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

type fixture struct {
	t     *testing.T
	store *Store
	dir   string
	at    time.Time
}

func newFixture(t *testing.T, pace int64) *fixture {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(filepath.Join(dir, "share.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	files := filepath.Join(dir, "files")
	os.MkdirAll(filepath.Join(files, "2026-09-27"), 0o755)
	root, err := os.OpenRoot(files)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return &fixture{t: t, dir: files, at: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC),
		store: &Store{DB: d, Root: root, Pace: pace, Logf: t.Logf}}
}

// add puts a file of size bytes into the library and returns it with its contents. With a
// shorter body, the file on the drive is cut off.
func (f *fixture) add(name string, size, body int) (db.File, []byte) {
	f.t.Helper()
	ctx := context.Background()
	data := make([]byte, size)
	rand.Read(data)
	f.at = f.at.Add(time.Minute)
	file := db.File{ID: ids.New(), Name: name, Size: int64(size), CreatedAt: f.at, UpdatedAt: f.at}
	rel := "2026-09-27/" + name
	if err := f.store.DB.InsertReceiving(ctx, file); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.store.DB.MarkFinalizing(ctx, file.ID, rel, "2026-09-27", f.at); err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.DB.MarkReady(ctx, file.ID, "application/octet-stream", db.KindDocument, f.at); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.dir, rel), data[:body], 0o644); err != nil {
		f.t.Fatal(err)
	}
	stored, err := f.store.DB.FileByID(ctx, file.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return stored, data
}

func TestWorkerKeepsTheChecksums(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()
	a, aData := f.add("a.bin", 300_000, 300_000)
	b, bData := f.add("b.bin", 0, 0)
	if n, err := f.store.DoPending(ctx); n != 2 || err != nil {
		t.Fatalf("DoPending = %d, %v", n, err)
	}
	for _, c := range []struct {
		file db.File
		data []byte
	}{{a, aData}, {b, bData}} {
		crc, ok, err := f.store.DB.FileCRC32(ctx, c.file.ID)
		if !ok || err != nil || crc != crc32.ChecksumIEEE(c.data) {
			t.Errorf("%s: %08x %v %v, want %08x", c.file.Name, crc, ok, err, crc32.ChecksumIEEE(c.data))
		}
	}
	if n, _ := f.store.DoPending(ctx); n != 0 {
		t.Errorf("a second run did %d files", n)
	}
	// Once known, the file isn't read again.
	os.Remove(filepath.Join(f.dir, a.RelPath))
	known, _ := f.store.DB.FileByID(ctx, a.ID)
	if crc, err := f.store.Get(ctx, known); err != nil || crc != crc32.ChecksumIEEE(aData) {
		t.Errorf("Get of a known one: %08x %v", crc, err)
	}
}

func TestDownloadsDontWaitForThePace(t *testing.T) {
	// At 64 KiB a second, the worker would need half a minute for this file.
	f := newFixture(t, 64<<10)
	file, data := f.add("video.mp4", 2<<20, 2<<20)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.store.DoPending(ctx)
	time.Sleep(100 * time.Millisecond)

	var wg sync.WaitGroup
	start := time.Now()
	for range 3 {
		wg.Go(func() {
			if crc, err := f.store.Get(ctx, file); err != nil || crc != crc32.ChecksumIEEE(data) {
				t.Errorf("Get: %08x %v", crc, err)
			}
		})
	}
	wg.Wait()
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Get waited %v for the paced worker", d)
	}
}

func TestAnAbandonedChecksumIsStartedAgain(t *testing.T) {
	f := newFixture(t, 64<<10)
	file, data := f.add("video.mp4", 1<<20, 1<<20)
	first, cancel := context.WithCancel(context.Background())
	go f.store.DoPending(first)
	time.Sleep(100 * time.Millisecond)
	cancel() // the one who started it went away
	if crc, err := f.store.Get(context.Background(), file); err != nil || crc != crc32.ChecksumIEEE(data) {
		t.Errorf("Get after the first gave up: %08x %v", crc, err)
	}
}

func TestBrokenFilesAreLeftAlone(t *testing.T) {
	f := newFixture(t, 0)
	ctx := context.Background()
	broken, _ := f.add("cut.bin", 1000, 400)
	if _, err := f.store.Get(ctx, broken); err == nil {
		t.Fatal("a file shorter than its size got a checksum")
	}
	for range tries {
		if n, err := f.store.DoPending(ctx); n != 0 || err != nil {
			t.Fatalf("DoPending = %d, %v", n, err)
		}
	}
	good, data := f.add("fine.bin", 100, 100)
	if n, _ := f.store.DoPending(ctx); n != 1 {
		t.Errorf("DoPending after giving up on the broken one = %d", n)
	}
	if crc, ok, _ := f.store.DB.FileCRC32(ctx, good.ID); !ok || crc != crc32.ChecksumIEEE(data) {
		t.Errorf("the good file: %08x %v", crc, ok)
	}
	if _, ok, _ := f.store.DB.FileCRC32(ctx, broken.ID); ok {
		t.Error("the broken file has a checksum")
	}
}

func TestWakeNeverBlocks(t *testing.T) {
	s := &Store{}
	for range 3 {
		s.Wake("id")
	}
}
