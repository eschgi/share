package storage

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// oldFiles sets up a library as it was before folders: files in day folders in the storage
// folder itself, without a folder in the database. It returns their ids by path.
func oldFiles(t *testing.T, files map[string]string, ids map[string]string) func(*db.DB, Layout) {
	return func(d *db.DB, l Layout) {
		for rel, content := range files {
			p := filepath.Join(l.StorageDir, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(rel, "20") {
				continue // not one of the library's files
			}
			id := newID()
			ids[rel] = id
			day, name, _ := strings.Cut(rel, "/")
			if _, err := d.Exec(`INSERT INTO files (id, state, name, size, received, mime, kind, rel_path, upload_day,
				created_at, updated_at, uploaded_at) VALUES (?, 'ready', ?, ?, ?, 'text/plain', 'document', ?, ?, 1, 1, 1)`,
				id, name, len(content), len(content), rel, day); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func newID() string { return ids.New() }

// read reads a library file the way downloads do.
func (fx *fixture) read(t *testing.T, id string) string {
	t.Helper()
	f, err := fx.db.FileByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	fh, err := fx.lib.Open(context.Background(), f)
	if err != nil {
		t.Fatalf("open %s: %v", f.RelPath, err)
	}
	defer fh.Close()
	b, _ := io.ReadAll(fh)
	return string(b)
}

func TestOldDayFoldersMoveIntoTheFirstFolder(t *testing.T) {
	ids := map[string]string{}
	fx := newFixtureWith(t, oldFiles(t, map[string]string{
		"2026-09-26/IMG_1.jpg":      "one",
		"2026-09-27/Menu.pdf":       "menu",
		"Photos from Oma/IMG_9.jpg": "not ours",
	}, ids))
	ctx := context.Background()
	if fx.folder.Dir != "Share" || fx.folder.RenamingFrom == nil || *fx.folder.RenamingFrom != "" {
		t.Fatalf("first folder: %+v", fx.folder)
	}
	// Before the move the files are read where they are.
	if got := fx.read(t, ids["2026-09-26/IMG_1.jpg"]); got != "one" {
		t.Fatalf("before the move: %q", got)
	}

	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]string{"2026-09-26/IMG_1.jpg": "one", "2026-09-27/Menu.pdf": "menu"} {
		if !fx.exists("Share/"+rel) || fx.exists(rel) {
			t.Errorf("%s didn't move into Share", rel)
		}
		if got := fx.read(t, ids[rel]); got != want {
			t.Errorf("%s after the move: %q", rel, got)
		}
	}
	for _, stays := range []string{"Photos from Oma/IMG_9.jpg", ".trash", ".uploads", MarkerName} {
		if !fx.exists(stays) {
			t.Errorf("%s moved, but isn't a day folder", stays)
		}
	}
	if f, _ := fx.db.FolderByID(ctx, fx.folder.ID); f.RenamingFrom != nil {
		t.Fatalf("the folder still notes an older directory: %+v", f)
	}
}

func TestMovingOldDayFoldersResumes(t *testing.T) {
	ids := map[string]string{}
	fx := newFixtureWith(t, oldFiles(t, map[string]string{
		"2026-09-25/IMG_0.jpg": "zero",
		"2026-09-26/IMG_1.jpg": "one",
		"2026-09-27/Menu.pdf":  "menu",
		"2026-09-27/notes.txt": "old notes", // a file someone put there by hand
	}, ids))
	ctx := context.Background()
	// What a move cut short left behind: one day moved already, and a file of the same name
	// as the hand-made one.
	if err := os.MkdirAll(fx.disk("2026-09-27"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(fx.layout.StorageDir, "2026-09-25"), fx.disk("2026-09-25")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fx.disk("2026-09-27/notes.txt"), []byte("new notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file that arrived after the upgrade, before the server got around to moving.
	id := fx.receiving(t, "Video.mp4", "video", 5)
	if err := fx.lib.Finalize(ctx, id); err != nil {
		t.Fatal(err)
	}

	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"Share/2026-09-26/IMG_1.jpg", "Share/2026-09-27/Menu.pdf", "Share/2026-09-27/Video.mp4", "Share/2026-09-25/IMG_0.jpg"} {
		if !fx.exists(rel) {
			t.Errorf("%s is missing", rel)
		}
	}
	// The hand-made file whose name is taken stays, and so does the note to try again.
	if !fx.exists("2026-09-27/notes.txt") || fx.exists("2026-09-27/Menu.pdf") {
		t.Fatal("the conflicting file should stay, the rest should go")
	}
	if f, _ := fx.db.FolderByID(ctx, fx.folder.ID); f.RenamingFrom == nil {
		t.Fatal("the folder forgot that files are left over")
	}
	if got := fx.read(t, ids["2026-09-27/Menu.pdf"]); got != "menu" {
		t.Fatalf("Menu.pdf: %q", got)
	}
	logs := len(fx.logs)
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if len(fx.logs) != logs {
		t.Errorf("the left-over file was logged again: %v", fx.logs[logs:])
	}

	// Once the name is free, the next run finishes.
	if err := os.Remove(fx.disk("2026-09-27/notes.txt")); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if fx.exists("2026-09-27") || !fx.exists("Share/2026-09-27/notes.txt") {
		t.Fatal("the day folder didn't move once the name was free")
	}
	if f, _ := fx.db.FolderByID(ctx, fx.folder.ID); f.RenamingFrom != nil {
		t.Fatal("the folder still notes an older directory")
	}
}

func TestUploadsWhileOldDaysWaitAvoidTheirNames(t *testing.T) {
	fx := newFixtureWith(t, oldFiles(t, map[string]string{
		"2026-09-27/IMG_1.jpg": "old",
		"2026-09-27/IMG_9.jpg": "by hand", // put there by hand: not in the database
	}, map[string]string{}))
	ctx := context.Background()
	if _, err := fx.db.Exec("DELETE FROM files WHERE name = 'IMG_9.jpg'"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, name := range []string{"IMG_1.jpg", "IMG_9.jpg"} {
		id := fx.receiving(t, name, "new", 3)
		if err := fx.lib.Finalize(ctx, id); err != nil {
			t.Fatal(err)
		}
		f, _ := fx.db.FileByID(ctx, id)
		got = append(got, f.RelPath)
	}
	if got[0] != "2026-09-27/IMG_1 (2).jpg" || got[1] != "2026-09-27/IMG_9 (2).jpg" {
		t.Fatalf("new files got %v", got)
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if fx.exists("2026-09-27") {
		t.Fatal("the old day folder is still there: the new files took its names")
	}
}

func TestATrashedFileStillAtTheOldPlaceIsNotForgotten(t *testing.T) {
	ids := map[string]string{}
	fx := newFixtureWith(t, oldFiles(t, map[string]string{"2026-09-26/old.jpg": "old"}, ids))
	ctx := context.Background()
	// Deleted just before the upgrade: the row says trashed, the bytes didn't move yet.
	if _, err := fx.db.Exec("UPDATE files SET state = 'trashed', deleted_at = 1 WHERE id = ?", ids["2026-09-26/old.jpg"]); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.reconcileTrash(ctx); err != nil {
		t.Fatal(err)
	}
	if !fx.exists(".trash/" + ids["2026-09-26/old.jpg"]) {
		t.Fatal("the trashed file's bytes didn't reach the trash")
	}
	if f, err := fx.db.FileByID(ctx, ids["2026-09-26/old.jpg"]); err != nil || f.State != db.StateTrashed {
		t.Fatalf("the trashed file was forgotten: %+v, %v", f, err)
	}
}

func TestFolderDirs(t *testing.T) {
	for _, tc := range []struct {
		name string
		n    int
		want string
	}{
		{"Wedding Anna & Marco", 1, "Wedding Anna & Marco"},
		{"Wedding Anna & Marco", 2, "Wedding Anna & Marco (2)"},
		{"Taxes/2026", 1, "Taxes_2026"},
		{`Taxes\2026`, 1, "Taxes_2026"},
		{"Taxes: 2026?", 1, "Taxes_ 2026_"},
		{"2026-09-26", 1, "2026-09-26 (2)"},
		{"2026-09-26", 3, "2026-09-26 (3)"},
		{".trash", 1, "_.trash"},
		{"CON", 1, "_CON"},
		{"Familie.", 1, "Familie"},
		{"...", 1, "Folder"},
		{strings.Repeat("ä", 200), 2, strings.Repeat("ä", 125) + " (2)"},
	} {
		if got := FolderDir(tc.name, tc.n); got != tc.want {
			t.Errorf("FolderDir(%q, %d) = %q, want %q", tc.name, tc.n, got, tc.want)
		}
	}

	fx := newFixture(t)
	ctx := context.Background()
	// Taken in the database (ignoring case), or on the drive: the next number.
	if err := os.MkdirAll(filepath.Join(fx.layout.StorageDir, "Family"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"share": "share (2)", "Family": "Family (2)", "Kindergarten": "Kindergarten"} {
		got, err := freeDir(ctx, fx.db, name, fx.exists)
		if err != nil || got != want {
			t.Errorf("freeDir(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
}

// readyIn puts a finished file into folder.
func (fx *fixture) readyIn(t *testing.T, folder db.Folder, name, content string) db.File {
	t.Helper()
	id := newID()
	f := db.File{ID: id, Name: name, Size: int64(len(content)), CreatedAt: fx.now, UpdatedAt: fx.now, FolderID: folder.ID}
	if err := fx.db.InsertReceiving(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.layout.UploadsDir(), id), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.Finalize(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	got, _ := fx.db.FileByID(context.Background(), id)
	return got
}

func TestRenameMovesTheDirectory(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	wedding, err := fx.lib.CreateFolder(ctx, "Wedding", "admin")
	if err != nil || !fx.exists("Wedding") {
		t.Fatalf("CreateFolder = %+v, %v", wedding, err)
	}
	f := fx.readyIn(t, wedding, "IMG_1.jpg", "one")

	renamed, err := fx.lib.RenameFolder(ctx, wedding.ID, "Hochzeit")
	if err != nil || renamed.Dir != "Hochzeit" || renamed.RenamingFrom != nil {
		t.Fatalf("RenameFolder = %+v, %v", renamed, err)
	}
	if fx.exists("Wedding") || !fx.exists("Hochzeit/2026-09-27/IMG_1.jpg") {
		t.Fatal("the directory didn't move")
	}
	if got := fx.read(t, f.ID); got != "one" { // a row read before the rename
		t.Fatalf("after the rename: %q", got)
	}
	// Only a change of case keeps the directory.
	if again, err := fx.lib.RenameFolder(ctx, wedding.ID, "HOCHZEIT"); err != nil || again.Dir != "Hochzeit" || again.Name != "HOCHZEIT" {
		t.Fatalf("a change of case: %+v, %v", again, err)
	}
	for _, bad := range []string{"", "  ", "2026-09-27"} {
		if _, err := fx.lib.RenameFolder(ctx, wedding.ID, bad); !errors.Is(err, ErrBadFolderName) {
			t.Errorf("RenameFolder(%q): %v", bad, err)
		}
	}
	if _, err := fx.lib.RenameFolder(ctx, wedding.ID, "share"); !errors.Is(err, db.ErrConflict) {
		t.Errorf("a name another folder has: %v", err)
	}
}

func TestRenameResumesAfterACrash(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	wedding, _ := fx.lib.CreateFolder(ctx, "Wedding", "admin")
	f := fx.readyIn(t, wedding, "IMG_1.jpg", "one")
	// The rename reached the database, but the server stopped before the directory moved.
	if _, err := fx.db.RenameFolder(ctx, wedding.ID, "Hochzeit", "Hochzeit"); err != nil {
		t.Fatal(err)
	}
	if got := fx.read(t, f.ID); got != "one" {
		t.Fatalf("before the move: %q", got)
	}
	if _, err := fx.lib.RenameFolder(ctx, wedding.ID, "Matrimonio"); !errors.Is(err, db.ErrBusy) {
		t.Fatalf("a second rename while the first is moving: %v", err)
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if fx.exists("Wedding") || !fx.exists("Hochzeit/2026-09-27/IMG_1.jpg") {
		t.Fatal("Reconcile didn't finish the rename")
	}
	if got, _ := fx.db.FolderByID(ctx, wedding.ID); got.RenamingFrom != nil {
		t.Fatal("the rename is still noted as moving")
	}
}

func TestDeletingAFolderTrashesItsFiles(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	wedding, _ := fx.lib.CreateFolder(ctx, "Wedding", "admin")
	a := fx.readyIn(t, wedding, "IMG_1.jpg", "one")
	arriving := fx.receiving(t, "IMG_2.jpg", "two", 1)
	if _, err := fx.db.Exec("UPDATE files SET folder_id = ? WHERE id = ?", wedding.ID, arriving); err != nil {
		t.Fatal(err)
	}

	trashed, err := fx.lib.DeleteFolder(ctx, wedding.ID, "admin")
	if err != nil || len(trashed) != 1 {
		t.Fatalf("DeleteFolder = %d files, %v", len(trashed), err)
	}
	if !fx.exists(".trash/"+a.ID) || fx.exists("Wedding") {
		t.Fatal("the files didn't go to the trash, or the empty directory stayed")
	}
	if _, err := fx.db.FileByID(ctx, arriving); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("the upload into the deleted folder is still there: %v", err)
	}
	if _, err := fx.lib.DeleteFolder(ctx, fx.folder.ID, "admin"); !errors.Is(err, db.ErrLastFolder) {
		t.Fatalf("deleting the last folder: %v", err)
	}

	// Restoring a file brings the folder back, with a number if its name is taken now.
	again, _ := fx.lib.CreateFolder(ctx, "wedding", "admin")
	if again.Dir != "wedding (2)" {
		t.Fatalf("the new folder's directory: %q; the deleted folder keeps its own", again.Dir)
	}
	if _, err := fx.lib.Restore(ctx, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	back, _ := fx.db.FolderByID(ctx, wedding.ID)
	if back.DeletedAt != nil || back.Name != "Wedding (2)" || !fx.exists("Wedding/2026-09-27/IMG_1.jpg") {
		t.Fatalf("the folder after the restore: %+v", back)
	}

	// Once its last file is purged, a deleted folder is gone.
	if _, err := fx.lib.DeleteFolder(ctx, wedding.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.lib.Purge(ctx, []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.db.FolderByID(ctx, wedding.ID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("the emptied folder is still there: %v", err)
	}
}

func TestReconcileFinishesADeletedFolder(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	wedding, _ := fx.lib.CreateFolder(ctx, "Wedding", "admin")
	a := fx.readyIn(t, wedding, "IMG_1.jpg", "one")
	// An upload that was finishing while the folder went.
	late := fx.receiving(t, "IMG_2.jpg", "two", 3)
	if _, err := fx.db.Exec("UPDATE files SET folder_id = ? WHERE id = ?", wedding.ID, late); err != nil {
		t.Fatal(err)
	}
	// The server stopped right after the folder was deleted in the database.
	if _, err := fx.db.DeleteFolder(ctx, wedding.ID, "admin", t0); err != nil {
		t.Fatal(err)
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a.ID, late} {
		f, _ := fx.db.FileByID(ctx, id)
		if f.State != db.StateTrashed || !fx.exists(".trash/"+id) {
			t.Errorf("%s after Reconcile: %s", f.Name, f.State)
		}
	}
}

func TestMoveFilesToAnotherFolder(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	family := fx.folder
	wedding, _ := fx.lib.CreateFolder(ctx, "Wedding", "admin")
	kindergarten, _ := fx.lib.CreateFolder(ctx, "Kindergarten", "admin")
	a := fx.readyIn(t, family, "IMG_1.jpg", "a")
	fx.readyIn(t, wedding, "IMG_1.jpg", "taken")
	b := fx.readyIn(t, kindergarten, "IMG_1.jpg", "b")
	c := fx.readyIn(t, family, "Menu.pdf", "c")
	before, _ := fx.db.LibraryVersion(ctx)

	moved, err := fx.lib.MoveFiles(ctx, []string{a.ID, b.ID, c.ID}, wedding.ID, nil)
	if err != nil || len(moved) != 3 {
		t.Fatalf("MoveFiles = %d files, %v", len(moved), err)
	}
	want := map[string]string{a.ID: "IMG_1 (2).jpg", b.ID: "IMG_1 (3).jpg", c.ID: "Menu.pdf"}
	for id, name := range want {
		f, _ := fx.db.FileByID(ctx, id)
		if f.FolderID != wedding.ID || f.RelPath != "2026-09-27/"+name || f.MovedFrom != "" || !f.UpdatedAt.Equal(a.UpdatedAt) {
			t.Errorf("%s after the move: %+v", name, f)
		}
		if !fx.exists("Wedding/2026-09-27/" + name) {
			t.Errorf("%s isn't in Wedding on the drive", name)
		}
	}
	if fx.exists("Share/2026-09-27") || fx.exists("Kindergarten/2026-09-27") {
		t.Error("an emptied day folder stayed")
	}
	if v, _ := fx.db.LibraryVersion(ctx); v == before {
		t.Error("the library version didn't change")
	}
	if again, err := fx.lib.MoveFiles(ctx, []string{a.ID}, wedding.ID, nil); err != nil || len(again) != 0 {
		t.Fatalf("moving into the folder it's in: %d, %v", len(again), err)
	}
	if _, err := fx.lib.MoveFiles(ctx, []string{a.ID}, newID(), nil); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("moving into no folder: %v", err)
	}
}

func TestMoveResumesAfterACrash(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	wedding, _ := fx.lib.CreateFolder(ctx, "Wedding", "admin")
	a := fx.readyIn(t, fx.folder, "IMG_1.jpg", "one")
	b := fx.readyIn(t, fx.folder, "IMG_2.jpg", "two")
	// The moves reached the database, but the server stopped before the bytes followed.
	if err := fx.db.MoveFiles(ctx, []db.Move{
		{ID: a.ID, FolderID: wedding.ID, RelPath: a.RelPath, From: fx.folder.ID + "/" + a.RelPath},
		{ID: b.ID, FolderID: wedding.ID, RelPath: b.RelPath, From: fx.folder.ID + "/" + b.RelPath},
	}); err != nil {
		t.Fatal(err)
	}
	if got := fx.read(t, a.ID); got != "one" {
		t.Fatalf("before the bytes moved: %q", got)
	}
	// Deleting one meanwhile takes its bytes from where they are.
	if _, err := fx.lib.Trash(ctx, []string{b.ID}, "admin"); err != nil || !fx.exists(".trash/"+b.ID) {
		t.Fatalf("trashing during the move: %v", err)
	}
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if !fx.exists("Wedding/2026-09-27/IMG_1.jpg") || fx.exists("Share/2026-09-27/IMG_1.jpg") {
		t.Fatal("Reconcile didn't finish the move")
	}
	for _, id := range []string{a.ID, b.ID} {
		if f, _ := fx.db.FileByID(ctx, id); f.MovedFrom != "" {
			t.Errorf("%s still notes a move", f.Name)
		}
	}
	if _, err := fx.lib.Restore(ctx, []string{b.ID}); err != nil || !fx.exists("Wedding/2026-09-27/IMG_2.jpg") {
		t.Fatalf("restoring the file deleted during the move: %v", err)
	}
}
