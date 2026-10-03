package storage

import (
	"context"
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
