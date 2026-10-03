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
	"strings"
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

// ErrBadFolderName means a folder's name is empty, or looks like a day: 2026-09-27.
var ErrBadFolderName = errors.New("a folder needs a name that isn't a date")

// cleanFolderName checks a folder's name as typed and returns it cleaned.
func cleanFolderName(name string) (string, error) {
	name = FolderName(name)
	if name == "" || dayName.MatchString(name) {
		return "", ErrBadFolderName
	}
	return name, nil
}

// CreateFolder makes a new folder and its directory. Nobody but the admins sees it until
// they are given it. It returns ErrBadFolderName, or db.ErrConflict if a folder has the name.
func (lib *Library) CreateFolder(ctx context.Context, name, by string) (db.Folder, error) {
	name, err := cleanFolderName(name)
	if err != nil {
		return db.Folder{}, err
	}
	lib.mu.Lock()
	defer lib.mu.Unlock()
	dir, err := freeDir(ctx, lib.DB, name, lib.exists)
	if err != nil {
		return db.Folder{}, err
	}
	f := db.Folder{ID: ids.New(), Name: name, Dir: dir, CreatedBy: by, CreatedAt: lib.Now()}
	if err := lib.DB.InsertFolder(ctx, f); err != nil {
		return db.Folder{}, err
	}
	if err := lib.root.MkdirAll(dir, 0o755); err != nil {
		lib.Logf("storage: making the directory %q: %v", dir, err) // the first file makes it too
	}
	return f, nil
}

// RenameFolder gives a folder a new name, and its directory the same. The files move with
// the directory in one rename; where that fails (on Windows, while a file in it is open),
// Reconcile finishes it later, and files are read from wherever they are meanwhile. It
// returns ErrBadFolderName, db.ErrNotFound, db.ErrConflict for a name that is taken, and
// db.ErrBusy while the files of an earlier rename are still being moved.
func (lib *Library) RenameFolder(ctx context.Context, id, name string) (db.Folder, error) {
	name, err := cleanFolderName(name)
	if err != nil {
		return db.Folder{}, err
	}
	lib.mu.Lock()
	defer lib.mu.Unlock()
	f, err := lib.DB.FolderByID(ctx, id)
	if err == nil && f.DeletedAt != nil {
		err = db.ErrNotFound
	}
	if err != nil {
		return db.Folder{}, err
	}
	dir := f.Dir
	// Only a new name gets a new directory: a change of case keeps it, since drives that ignore
	// case can't tell the two apart.
	if !strings.EqualFold(FolderDir(name, 1), f.Dir) {
		if dir, err = freeDir(ctx, lib.DB, name, lib.exists); err != nil {
			return db.Folder{}, err
		}
	}
	renamed, err := lib.DB.RenameFolder(ctx, id, name, dir)
	if err != nil || renamed.RenamingFrom == nil {
		return renamed, err
	}
	from := *renamed.RenamingFrom
	done, err := lib.relocateFolder(dir, from)
	switch {
	case err != nil:
		lib.Logf("storage: moving %q to %q: %v; trying again later", from, dir, err)
	case done:
		if err := lib.DB.FinishRelocation(ctx, id, from); err != nil {
			return renamed, err
		}
		renamed.RenamingFrom = nil
	}
	return renamed, nil
}

// DeleteFolder deletes a folder: its files go to the trash, where they wait like any deleted
// file, its PINs end and its unfinished uploads are dropped. Restoring one of its files
// brings it back. It returns the files it trashed; db.ErrLastFolder for the last folder.
func (lib *Library) DeleteFolder(ctx context.Context, id, by string) ([]db.File, error) {
	lib.mu.Lock()
	files, err := lib.DB.DeleteFolder(ctx, id, by, lib.Now())
	lib.mu.Unlock()
	if err != nil {
		return nil, err
	}
	receiving, err := lib.DB.FilesInStates(ctx, db.StateReceiving)
	if err != nil {
		return files, err
	}
	for _, f := range receiving {
		if f.FolderID == id {
			if err := lib.Terminate(ctx, f.ID); err != nil {
				lib.Logf("storage: dropping %s: %v", f.ID, err)
			}
		}
	}
	// The bytes go a few hundred at a time, so uploads can finish in between.
	for start := 0; start < len(files); start += 200 {
		lib.mu.Lock()
		for _, f := range files[start:min(start+200, len(files))] {
			if err := lib.moveToTrash(ctx, f); err != nil {
				lib.Logf("storage: trashing %s: %v", f.ID, err) // the reconciler tries again
			}
		}
		lib.mu.Unlock()
	}
	if folder, err := lib.DB.FolderByID(ctx, id); err == nil && folder.Dir != "" {
		lib.removeEmptyDirs(folder.Dir) // restoring a file makes it again
	}
	return files, nil
}

// sweepDeletedFolders trashes what reached a folder after it was deleted (an upload that was
// finishing), and removes the directories of deleted folders that have nothing left.
func (lib *Library) sweepDeletedFolders(ctx context.Context) error {
	late, err := lib.DB.ReadyInDeletedFolders(ctx)
	if err != nil {
		return err
	}
	for _, f := range late {
		folder, err := lib.DB.FolderByID(ctx, f.FolderID)
		if err != nil {
			return err
		}
		if _, err := lib.Trash(ctx, []string{f.ID}, folder.DeletedBy); err != nil {
			return err
		}
	}
	return nil
}

// dropEmptyFolders forgets deleted folders whose last file was purged, and removes their
// directories if nothing else is in them.
func (lib *Library) dropEmptyFolders(ctx context.Context) error {
	dropped, err := lib.DB.DropEmptyDeletedFolders(ctx)
	if err != nil {
		return err
	}
	for _, f := range dropped {
		for _, dir := range []string{f.Dir, ptrOr(f.RenamingFrom, f.Dir)} {
			if dir != "" {
				lib.removeEmptyDirs(dir)
			}
		}
	}
	return nil
}

// removeEmptyDirs removes a folder's directory if only empty day folders are left in it.
func (lib *Library) removeEmptyDirs(dir string) {
	entries, err := fs.ReadDir(lib.root.FS(), dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			lib.root.Remove(path.Join(dir, e.Name())) // fails while it holds something
		}
	}
	lib.root.Remove(dir)
}

func ptrOr(p *string, or string) string {
	if p == nil {
		return or
	}
	return *p
}

// MoveFiles moves files of the library to another folder: on the drive, into the same day's
// folder in the other folder's directory, with a number added where a name is taken. Who sees
// them changes with the folder. The database changes first and the bytes follow; Reconcile
// finishes what a crash cut short. It returns the files it moved; db.ErrNotFound for no such
// folder.
func (lib *Library) MoveFiles(ctx context.Context, fileIDs []string, folderID string) ([]db.File, error) {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	to, err := lib.DB.FolderByID(ctx, folderID)
	if err == nil && to.DeletedAt != nil {
		err = db.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	files, err := lib.DB.ReadyFiles(ctx, fileIDs)
	if err != nil {
		return nil, err
	}
	var moves []db.Move
	var moved []db.File
	var from []string
	claimed := map[string]bool{}
	for _, f := range files {
		if f.FolderID == to.ID {
			continue
		}
		src, err := lib.locate(ctx, f)
		if err != nil {
			return nil, err
		}
		rel, err := lib.freePath(ctx, to, f.UploadDay, f.Name, claimed)
		if err != nil {
			return nil, err
		}
		claimed[strings.ToLower(rel)] = true
		moves = append(moves, db.Move{ID: f.ID, FolderID: to.ID, RelPath: rel, From: f.FolderID + "/" + f.RelPath})
		from = append(from, src)
		f.FolderID, f.RelPath, f.MovedFrom = to.ID, rel, f.FolderID+"/"+f.RelPath
		moved = append(moved, f)
	}
	if len(moves) == 0 {
		return nil, nil
	}
	if err := lib.DB.MoveFiles(ctx, moves); err != nil {
		return nil, err
	}
	var done []string
	dirs := map[string]bool{}
	for i, f := range moved {
		dst := inFolder(to, f.RelPath)
		if err := lib.root.MkdirAll(path.Dir(dst), 0o755); err != nil {
			lib.Logf("storage: moving %s: %v", f.ID, err) // the reconciler tries again
			continue
		}
		if err := lib.root.Rename(from[i], dst); err != nil {
			lib.Logf("storage: moving %s: %v", f.ID, err)
			continue
		}
		dirs[path.Dir(dst)], dirs[path.Dir(from[i])] = true, true
		done = append(done, f.ID)
	}
	for dir := range dirs {
		if err := syncFile(lib.root, dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			lib.Logf("storage: syncing %s: %v", dir, err)
		}
		lib.root.Remove(dir) // a day's folder goes with its last file
	}
	if err := lib.DB.FinishMoves(ctx, done); err != nil {
		return moved, err
	}
	return moved, nil
}

// finishMoves moves the bytes of files whose move to another folder was cut short.
func (lib *Library) finishMoves(ctx context.Context) error {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	files, err := lib.DB.Moving(ctx)
	if err != nil {
		return err
	}
	var done []string
	for _, f := range files {
		folder, err := lib.folderOf(ctx, f)
		if err != nil {
			return err
		}
		dst, src := inFolder(folder, f.RelPath), lib.movedFrom(ctx, f)
		if !lib.exists(dst) && src != "" && lib.exists(src) {
			if err := lib.root.MkdirAll(path.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := lib.root.Rename(src, dst); err != nil {
				lib.Logf("storage: moving %s: %v", f.ID, err)
				continue
			}
			lib.syncDirs(path.Dir(dst), path.Dir(src))
			lib.root.Remove(path.Dir(src))
		}
		// Moved now, or before a crash; or its bytes are in the trash already.
		done = append(done, f.ID)
	}
	return lib.DB.FinishMoves(ctx, done)
}
