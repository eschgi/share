package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// ErrIncomplete means an upload hasn't received all its bytes yet.
var ErrIncomplete = errors.New("upload is not complete yet")

// Library moves finished uploads into the day folders of their folder, and keeps track of
// where each file's bytes are.
type Library struct {
	DB     *db.DB
	Layout Layout
	Loc    *time.Location
	Now    func() time.Time
	Logf   func(string, ...any)
	// OnReady is called after a file reaches the library (the thumbnail worker listens).
	OnReady func(id string)
	// OnPurged is called after a file is removed for good (its thumbnail goes too).
	OnPurged func(id string)

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

// inFolder is a path in a folder's directory.
func inFolder(folder db.Folder, rel string) string { return path.Join(folder.Dir, rel) }

// folderOf returns the folder a file lies in.
func (lib *Library) folderOf(ctx context.Context, f db.File) (db.Folder, error) {
	if f.FolderID == "" {
		return db.Folder{}, fmt.Errorf("file %s has no folder", f.ID)
	}
	return lib.DB.FolderByID(ctx, f.FolderID)
}

// locate finds a file's bytes on the drive: the path in its folder's directory.
func (lib *Library) locate(ctx context.Context, f db.File) (string, error) {
	folder, err := lib.folderOf(ctx, f)
	if err != nil {
		return "", err
	}
	return inFolder(folder, f.RelPath), nil
}

// Open opens a file of the library for reading. A row read before a rename or a move may
// point to an old place; then the file is looked up again.
func (lib *Library) Open(ctx context.Context, f db.File) (*os.File, error) {
	for try := 0; ; try++ {
		p, err := lib.locate(ctx, f)
		if err != nil {
			return nil, err
		}
		file, err := lib.root.Open(p)
		if err == nil || !errors.Is(err, fs.ErrNotExist) || try == 2 {
			return file, err
		}
		if f, err = lib.DB.FileByID(ctx, f.ID); err != nil {
			return nil, fs.ErrNotExist
		}
	}
}

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
	dst, err := lib.locate(ctx, f)
	if err != nil {
		return err
	}
	if err := lib.moveIntoLibrary(f, dst); err != nil {
		return err
	}
	mime, kind := lib.classify(f, dst)
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

// claimPath picks the path in its folder for f and records it, retrying if a name is taken.
func (lib *Library) claimPath(ctx context.Context, f db.File) (db.File, error) {
	folder, err := lib.folderOf(ctx, f)
	if err != nil {
		return f, err
	}
	now := lib.Now()
	day := Day(now, lib.Loc)
	for range 5 {
		rel, err := lib.freePath(ctx, folder, day, f.Name)
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

// freePath returns day/name in a folder, or day/name (2) and so on if taken. Names are
// compared without case, both in the database and on disk, because exFAT and NTFS drives
// ignore case.
func (lib *Library) freePath(ctx context.Context, folder db.Folder, day, name string) (string, error) {
	for n := 1; n <= 10000; n++ {
		candidate := name
		if n > 1 {
			candidate = numbered(name, n)
		}
		rel := day + "/" + candidate
		taken, err := lib.DB.RelPathTaken(ctx, folder.ID, rel)
		if err != nil {
			return "", err
		}
		if taken {
			continue
		}
		if _, err := lib.root.Lstat(inFolder(folder, rel)); err == nil {
			continue // a file someone put there by hand
		}
		return rel, nil
	}
	return "", fmt.Errorf("no free name for %q", name)
}

// moveIntoLibrary renames the upload's bytes to dst, their place in the library, with fsyncs
// so a power cut can't leave a file that looks complete but isn't.
func (lib *Library) moveIntoLibrary(f db.File, dst string) error {
	src := uploadPath(f.ID)
	if _, err := lib.root.Lstat(src); errors.Is(err, fs.ErrNotExist) {
		if _, err := lib.root.Lstat(dst); err == nil {
			return nil // moved before a crash
		}
		return fmt.Errorf("finalize %s: the uploaded data is gone", f.ID)
	}
	if err := syncFile(lib.root, src); err != nil {
		return err
	}
	dayDir := path.Dir(dst)
	if err := lib.root.MkdirAll(dayDir, 0o755); err != nil {
		return err
	}
	if err := lib.root.Rename(src, dst); err != nil {
		return fmt.Errorf("finalize %s: %w", f.ID, err)
	}
	for _, dir := range []string{dayDir, ".uploads"} {
		if err := syncFile(lib.root, dir); err != nil {
			return err
		}
	}
	return nil
}

func (lib *Library) classify(f db.File, at string) (mime, kind string) {
	head := make([]byte, 512)
	fh, err := lib.root.Open(at)
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
//   - files in .uploads without a row, and rows without files, are removed after an hour;
//   - a trash, restore or purge that was cut short is finished.
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
	if err := lib.removeOrphans(ctx, now); err != nil {
		return err
	}
	return lib.reconcileTrash(ctx)
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
