package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// EnsureFirstFolder makes the first folder when there is none: after the upgrade to folders,
// or on a new server. It is named after the server and gets everything there is; the day
// folders from before folders move into its directory when the server starts
// (Library.Reconcile). It returns the oldest folder.
func EnsureFirstFolder(ctx context.Context, d *db.DB, storageDir, name string, now time.Time) (db.Folder, error) {
	if name = FolderName(name); name == "" {
		name = "Share"
	}
	dir, err := freeDir(ctx, d, name, func(dir string) bool {
		_, err := os.Lstat(filepath.Join(storageDir, dir))
		return err == nil
	})
	if err != nil {
		return db.Folder{}, err
	}
	f := db.Folder{ID: ids.New(), Name: name, Dir: dir, CreatedBy: "first-start"}
	folder, _, err := d.EnsureFirstFolder(ctx, f, now)
	return folder, err
}

// freeDir picks the directory for a new folder called name: one no folder has, deleted or
// not, and that isn't on the drive yet.
func freeDir(ctx context.Context, d *db.DB, name string, exists func(string) bool) (string, error) {
	for n := 1; n <= 1000; n++ {
		dir := FolderDir(name, n)
		taken, err := d.DirTaken(ctx, dir)
		if err != nil {
			return "", err
		}
		if !taken && !exists(dir) {
			return dir, nil
		}
	}
	return "", fmt.Errorf("no free directory for the folder %q", name)
}

// relocate finishes moving folders' files from an older directory: the day folders from
// before folders, which lie in the storage folder itself, or a folder's directory before a
// rename. A name taken at both places stays where it is and keeps the folder's note, so the
// next run tries again; meanwhile files are read from wherever they are (locate).
func (lib *Library) relocate(ctx context.Context) error {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	folders, err := lib.DB.Relocating(ctx)
	if err != nil {
		return err
	}
	for _, f := range folders {
		from := *f.RenamingFrom
		done, err := lib.relocateFolder(f.Dir, from)
		if err != nil {
			lib.Logf("storage: moving the files of %q into %q: %v", f.Name, f.Dir, err)
			continue
		}
		key := f.ID + "/" + from
		if !done {
			if !lib.leftOver[key] {
				where := "the storage folder"
				if from != "" {
					where = strconv.Quote(from)
				}
				lib.Logf("storage: some files of the folder %q are still in %s, because %q has files with the same names", f.Name, where, f.Dir)
				if lib.leftOver == nil {
					lib.leftOver = map[string]bool{}
				}
				lib.leftOver[key] = true
			}
			continue
		}
		delete(lib.leftOver, key)
		if err := lib.DB.FinishRelocation(ctx, f.ID, from); err != nil {
			return err
		}
	}
	return nil
}

// relocateFolder moves what lies under from into dir, and reports whether nothing is left.
func (lib *Library) relocateFolder(dir, from string) (bool, error) {
	if from != "" {
		return lib.merge(from, dir)
	}
	if err := lib.root.MkdirAll(dir, 0o755); err != nil {
		return false, err
	}
	entries, err := fs.ReadDir(lib.root.FS(), ".")
	if err != nil {
		return false, err
	}
	done := true
	for _, e := range entries {
		if !e.IsDir() || !dayName.MatchString(e.Name()) {
			continue
		}
		gone, err := lib.merge(e.Name(), path.Join(dir, e.Name()))
		if err != nil {
			return false, err
		}
		done = done && gone
	}
	return done, nil
}

// merge moves the directory src to dst: in one rename while dst doesn't exist, otherwise
// entry by entry, leaving whatever has a name that is taken in dst where it is. It reports
// whether src is gone.
func (lib *Library) merge(src, dst string) (bool, error) {
	if _, err := lib.root.Lstat(src); errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if _, err := lib.root.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
		if err := lib.root.MkdirAll(path.Dir(dst), 0o755); err != nil {
			return false, err
		}
		if err := lib.root.Rename(src, dst); err != nil {
			return false, err
		}
		return true, lib.syncDirs(path.Dir(dst), path.Dir(src))
	}
	entries, err := fs.ReadDir(lib.root.FS(), src)
	if err != nil {
		return false, err
	}
	gone := true
	for _, e := range entries {
		from, to := path.Join(src, e.Name()), path.Join(dst, e.Name())
		if e.IsDir() {
			moved, err := lib.merge(from, to)
			if err != nil {
				return false, err
			}
			gone = gone && moved
			continue
		}
		if _, err := lib.root.Lstat(to); err == nil {
			gone = false
			continue
		}
		if err := lib.root.Rename(from, to); err != nil {
			return false, err
		}
	}
	if err := lib.syncDirs(dst, src); err != nil {
		return false, err
	}
	if gone {
		if err := lib.root.Remove(src); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	return gone, nil
}

// syncDirs flushes folders to the drive, after renames between them.
func (lib *Library) syncDirs(dirs ...string) error {
	for _, dir := range dirs {
		if err := syncFile(lib.root, dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
