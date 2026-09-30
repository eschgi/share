package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// ErrIncomplete means an upload hasn't received all its bytes yet.
var ErrIncomplete = errors.New("upload is not complete yet")

// Library moves finished uploads into the day folders.
type Library struct {
	DB     *db.DB
	Layout Layout
	Loc    *time.Location
	Now    func() time.Time
	Logf   func(string, ...any)
	// OnReady is called after a file reaches the library (the thumbnail worker listens).
	OnReady func(id string)

	root *os.Root // the storage folder; every library path is opened through it
	mu   sync.Mutex
}

// OpenLibrary opens the storage folder for the library.
func OpenLibrary(d *db.DB, l Layout, loc *time.Location, now func() time.Time, logf func(string, ...any)) (*Library, error) {
	root, err := os.OpenRoot(l.StorageDir)
	if err != nil {
		return nil, err
	}
	return &Library{DB: d, Layout: l, Loc: loc, Now: now, Logf: logf, root: root}, nil
}

// Close releases the storage folder.
func (lib *Library) Close() error { return lib.root.Close() }

// Root gives read access to the storage folder, e.g. for downloads.
func (lib *Library) Root() *os.Root { return lib.root }

func uploadPath(id string) string { return ".uploads/" + id }
func infoPath(id string) string   { return ".uploads/" + id + ".info" }

// Finalize moves a complete upload into the library. It is idempotent: the tus hook, a tus
// HEAD after a lost response and the reconciler may all call it, in any state, and a crash
// at any step is picked up again by the next call.
func (lib *Library) Finalize(ctx context.Context, id string) error {
	lib.mu.Lock()
	defer lib.mu.Unlock()

	f, err := lib.DB.FileByID(ctx, id)
	if err != nil {
		return err
	}
	switch f.State {
	case db.StateReady, db.StateTrashed:
		return nil
	case db.StateReceiving:
		info, err := lib.root.Stat(uploadPath(id))
		if err != nil {
			return fmt.Errorf("finalize %s: %w", id, err)
		}
		if info.Size() != f.Size {
			return ErrIncomplete
		}
		if f, err = lib.claimPath(ctx, f); err != nil {
			return err
		}
	}

	// State is finalizing: the path is chosen, the bytes may or may not be there yet.
	if err := lib.moveIntoLibrary(f); err != nil {
		return err
	}
	mime, kind := lib.classify(f)
	if err := lib.DB.MarkReady(ctx, id, mime, kind, lib.Now()); err != nil {
		return err
	}
	if err := lib.root.Remove(infoPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		lib.Logf("storage: removing %s: %v", infoPath(id), err)
	}
	if lib.OnReady != nil {
		lib.OnReady(id)
	}
	return nil
}

// claimPath picks the library path for f and records it, retrying if a name is taken.
func (lib *Library) claimPath(ctx context.Context, f db.File) (db.File, error) {
	now := lib.Now()
	day := Day(now, lib.Loc)
	for range 5 {
		rel, err := lib.freePath(ctx, day, f.Name)
		if err != nil {
			return f, err
		}
		ok, err := lib.DB.MarkFinalizing(ctx, f.ID, rel, day, now)
		if errors.Is(err, db.ErrConflict) {
			continue
		}
		if err != nil {
			return f, err
		}
		if !ok {
			return lib.DB.FileByID(ctx, f.ID)
		}
		f.State, f.RelPath, f.UploadDay = db.StateFinalizing, rel, day
		return f, nil
	}
	return f, fmt.Errorf("finalize %s: no free name for %q", f.ID, f.Name)
}

// freePath returns day/name, or day/name (2) and so on if taken. Names are compared without
// case, both in the database and on disk, because exFAT and NTFS drives ignore case.
func (lib *Library) freePath(ctx context.Context, day, name string) (string, error) {
	for n := 1; n <= 10000; n++ {
		candidate := name
		if n > 1 {
			candidate = numbered(name, n)
		}
		rel := day + "/" + candidate
		taken, err := lib.DB.RelPathTaken(ctx, rel)
		if err != nil {
			return "", err
		}
		if taken {
			continue
		}
		if _, err := lib.root.Lstat(rel); err == nil {
			continue // a file someone put there by hand
		}
		return rel, nil
	}
	return "", fmt.Errorf("no free name for %q", name)
}

// moveIntoLibrary renames the upload's bytes to their library path, with fsyncs so a power cut
// can't leave a file that looks complete but isn't.
func (lib *Library) moveIntoLibrary(f db.File) error {
	src := uploadPath(f.ID)
	if _, err := lib.root.Lstat(src); errors.Is(err, fs.ErrNotExist) {
		if _, err := lib.root.Lstat(f.RelPath); err == nil {
			return nil // moved before a crash
		}
		return fmt.Errorf("finalize %s: the uploaded data is gone", f.ID)
	}
	if err := syncFile(lib.root, src); err != nil {
		return err
	}
	if err := lib.root.MkdirAll(f.UploadDay, 0o755); err != nil {
		return err
	}
	if err := lib.root.Rename(src, f.RelPath); err != nil {
		return fmt.Errorf("finalize %s: %w", f.ID, err)
	}
	for _, dir := range []string{f.UploadDay, ".uploads"} {
		if err := syncFile(lib.root, dir); err != nil {
			return err
		}
	}
	return nil
}

func syncFile(root *os.Root, name string) error {
	fh, err := root.Open(name)
	if err != nil {
		return err
	}
	defer fh.Close()
	if err := fh.Sync(); err != nil && !errors.Is(err, os.ErrInvalid) {
		return err
	}
	return nil
}

func (lib *Library) classify(f db.File) (mime, kind string) {
	head := make([]byte, 512)
	fh, err := lib.root.Open(f.RelPath)
	if err == nil {
		n, _ := io.ReadFull(fh, head)
		head = head[:n]
		fh.Close()
	}
	return Classify(f.Name, head)
}

// Terminate drops an unfinished upload: its bytes, its tus info file and its row.
func (lib *Library) Terminate(ctx context.Context, id string) error {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	for _, p := range []string{uploadPath(id), infoPath(id)} {
		if err := lib.root.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return lib.DB.DeleteFileRow(ctx, id)
}

// Reconcile repairs what a crash or an abandoned upload leaves behind. It runs before the
// server starts listening and then every few minutes:
//   - uploads stuck halfway through finalizing are finished;
//   - receiving uploads whose bytes are all there are finalized (the response was lost);
//   - receiving uploads idle for longer than ttl are dropped;
//   - files in .uploads without a row, and rows without files, are removed after an hour.
func (lib *Library) Reconcile(ctx context.Context, ttl time.Duration) error {
	now := lib.Now()
	rows, err := lib.DB.FilesInStates(ctx, db.StateFinalizing, db.StateReceiving)
	if err != nil {
		return err
	}
	for _, f := range rows {
		if f.State == db.StateFinalizing {
			if err := lib.Finalize(ctx, f.ID); err != nil {
				lib.Logf("storage: finishing %s: %v", f.ID, err)
			}
			continue
		}
		info, statErr := lib.root.Stat(uploadPath(f.ID))
		switch {
		case statErr == nil && info.Size() == f.Size:
			if err := lib.Finalize(ctx, f.ID); err != nil {
				lib.Logf("storage: finishing %s: %v", f.ID, err)
			}
		case now.Sub(f.UpdatedAt) > ttl:
			lib.Logf("storage: dropping %s (%q), idle since %s", f.ID, f.Name, f.UpdatedAt.Format(time.RFC3339))
			if err := lib.Terminate(ctx, f.ID); err != nil {
				lib.Logf("storage: dropping %s: %v", f.ID, err)
			}
		case errors.Is(statErr, fs.ErrNotExist) && now.Sub(f.CreatedAt) > time.Hour:
			if err := lib.Terminate(ctx, f.ID); err != nil {
				lib.Logf("storage: dropping %s: %v", f.ID, err)
			}
		}
	}
	return lib.removeOrphans(ctx, now)
}

func (lib *Library) removeOrphans(ctx context.Context, now time.Time) error {
	entries, err := fs.ReadDir(lib.root.FS(), ".uploads")
	if err != nil {
		return err
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".info")
		if !ids.Valid(id) {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < time.Hour {
			continue
		}
		if _, err := lib.DB.FileByID(ctx, id); errors.Is(err, db.ErrNotFound) {
			if err := lib.root.Remove(".uploads/" + e.Name()); err != nil && !errors.Is(err, fs.ErrNotExist) {
				lib.Logf("storage: removing orphan %s: %v", e.Name(), err)
			}
		}
	}
	return nil
}
