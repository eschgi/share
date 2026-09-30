package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// The trash is the .trash folder on the same drive, one file per id, so deleting and
// restoring are renames. The database changes first and the bytes follow; reconcileTrash
// finishes whatever a crash left between the two.

func trashPath(id string) string { return ".trash/" + id }

// Trash takes files out of the library into the trash. It returns the files it took; ids
// that aren't in the library are left alone.
func (lib *Library) Trash(ctx context.Context, fileIDs []string, by string) ([]db.File, error) {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	files, err := lib.DB.TrashFiles(ctx, fileIDs, by, lib.Now())
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if err := lib.moveToTrash(f); err != nil {
			lib.Logf("storage: trashing %s: %v", f.ID, err) // the reconciler tries again
		}
	}
	return files, nil
}

func (lib *Library) moveToTrash(f db.File) error {
	if _, err := lib.root.Lstat(trashPath(f.ID)); err == nil {
		return nil // moved before a crash
	}
	if err := lib.root.Rename(f.RelPath, trashPath(f.ID)); err != nil {
		return err
	}
	for _, dir := range []string{".trash", f.UploadDay} {
		if err := syncFile(lib.root, dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	// A day's folder goes with its last file; with files left, removing it just fails.
	lib.root.Remove(path.Dir(f.RelPath))
	return nil
}

// Restore puts trashed files back into the library, each at its old path, or with a number
// added if a new file has that name now. It returns the files it restored.
func (lib *Library) Restore(ctx context.Context, fileIDs []string) ([]db.File, error) {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	files, err := lib.DB.TrashedByID(ctx, fileIDs)
	if err != nil {
		return nil, err
	}
	var out []db.File
	for _, f := range files {
		rel, err := lib.restorePath(ctx, f)
		if err != nil {
			return out, err
		}
		if rel == "" {
			continue // restored meanwhile
		}
		f.State, f.RelPath, f.DeletedAt, f.DeletedBy = db.StateReady, rel, nil, ""
		if err := lib.moveFromTrash(f); err != nil {
			lib.Logf("storage: restoring %s: %v", f.ID, err) // the reconciler tries again
		}
		out = append(out, f)
	}
	return out, nil
}

// restorePath claims a library path for a trashed file and records it; "" if the file isn't
// in the trash any more.
func (lib *Library) restorePath(ctx context.Context, f db.File) (string, error) {
	for try := range 5 {
		rel := f.RelPath
		taken, err := lib.DB.RelPathTaken(ctx, rel)
		if err != nil {
			return "", err
		}
		if _, statErr := lib.root.Lstat(rel); taken || statErr == nil || try > 0 {
			if rel, err = lib.freePath(ctx, f.UploadDay, f.Name); err != nil {
				return "", err
			}
		}
		ok, err := lib.DB.RestoreFile(ctx, f.ID, rel, lib.Now())
		if errors.Is(err, db.ErrConflict) {
			continue
		}
		if err != nil || !ok {
			return "", err
		}
		return rel, nil
	}
	return "", fmt.Errorf("restore %s: no free name for %q", f.ID, f.Name)
}

func (lib *Library) moveFromTrash(f db.File) error {
	if _, err := lib.root.Lstat(trashPath(f.ID)); errors.Is(err, fs.ErrNotExist) {
		if _, err := lib.root.Lstat(f.RelPath); err == nil {
			return nil // moved before a crash
		}
		return fmt.Errorf("restore %s: the file is gone from the trash", f.ID)
	}
	if err := lib.root.MkdirAll(f.UploadDay, 0o755); err != nil {
		return err
	}
	if err := lib.root.Rename(trashPath(f.ID), f.RelPath); err != nil {
		return err
	}
	for _, dir := range []string{f.UploadDay, ".trash"} {
		if err := syncFile(lib.root, dir); err != nil {
			return err
		}
	}
	return nil
}

// Purge removes trashed files for good: their rows, their bytes and (through OnPurged) their
// thumbnails. It returns the files it removed.
func (lib *Library) Purge(ctx context.Context, fileIDs []string) ([]db.File, error) {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	files, err := lib.DB.TrashedByID(ctx, fileIDs)
	if err != nil {
		return nil, err
	}
	var out []db.File
	for _, f := range files {
		ok, err := lib.DB.PurgeFile(ctx, f.ID)
		if err != nil {
			return out, err
		}
		if !ok {
			continue
		}
		lib.removePurged(f.ID)
		out = append(out, f)
	}
	return out, nil
}

// PurgeOld removes what has been in the trash for longer than days.
func (lib *Library) PurgeOld(ctx context.Context, days int) (int, error) {
	files, err := lib.DB.TrashedBefore(ctx, lib.Now().Add(-time.Duration(days)*24*time.Hour))
	if err != nil || len(files) == 0 {
		return 0, err
	}
	fileIDs := make([]string, len(files))
	for i, f := range files {
		fileIDs[i] = f.ID
	}
	purged, err := lib.Purge(ctx, fileIDs)
	return len(purged), err
}

func (lib *Library) removePurged(id string) {
	if err := lib.root.Remove(trashPath(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		lib.Logf("storage: removing %s from the trash: %v", id, err)
	}
	if lib.OnPurged != nil {
		lib.OnPurged(id)
	}
}

// reconcileTrash finishes a trash, restore or purge that a crash interrupted:
//   - a trashed file whose bytes are still in the library folder moves them to the trash;
//     one whose bytes are gone everywhere is forgotten;
//   - bytes in the trash whose file is ready again (a restore) go back to the library;
//     bytes without a row (a purge) are removed.
func (lib *Library) reconcileTrash(ctx context.Context) error {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	trashed, err := lib.DB.Trashed(ctx)
	if err != nil {
		return err
	}
	for _, f := range trashed {
		if _, err := lib.root.Lstat(trashPath(f.ID)); err == nil {
			continue
		}
		if _, err := lib.root.Lstat(f.RelPath); err == nil {
			if err := lib.moveToTrash(f); err != nil {
				lib.Logf("storage: trashing %s: %v", f.ID, err)
			}
			continue
		}
		lib.Logf("storage: %s (%q) is gone from the trash; forgetting it", f.ID, f.Name)
		if _, err := lib.DB.PurgeFile(ctx, f.ID); err != nil {
			return err
		}
		if lib.OnPurged != nil {
			lib.OnPurged(f.ID)
		}
	}

	entries, err := fs.ReadDir(lib.root.FS(), ".trash")
	if errors.Is(err, fs.ErrNotExist) {
		return nil // share check reports it
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		id := e.Name()
		if !ids.Valid(id) {
			continue // not ours
		}
		f, err := lib.DB.FileByID(ctx, id)
		switch {
		case errors.Is(err, db.ErrNotFound):
			lib.removePurged(id)
		case err != nil:
			return err
		case f.State == db.StateReady:
			if err := lib.moveFromTrash(f); err != nil {
				lib.Logf("storage: restoring %s: %v", f.ID, err)
			}
		}
	}
	return nil
}
