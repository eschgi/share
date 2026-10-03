package storage

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// ready puts a finished file into the library.
func (fx *fixture) ready(t *testing.T, name, content string) db.File {
	t.Helper()
	id := fx.receiving(t, name, content, len(content))
	if err := fx.lib.Finalize(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	f, err := fx.db.FileByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// exists reports whether a path in the storage folder exists.
func (fx *fixture) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(fx.layout.StorageDir, filepath.FromSlash(rel)))
	return err == nil
}

// inFolder is a path in the first folder's directory, as a path in the storage folder.
func (fx *fixture) inFolder(rel string) string { return path.Join(fx.folder.Dir, rel) }

// disk is where a path in the first folder is on the drive.
func (fx *fixture) disk(rel string) string {
	return filepath.Join(fx.layout.StorageDir, filepath.FromSlash(fx.inFolder(rel)))
}

func TestTrashAndRestore(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	a := fx.ready(t, "IMG_1.jpg", "one")
	b := fx.ready(t, "IMG_2.jpg", "two")
	before, _ := fx.db.LibraryVersion(ctx)

	trashed, err := fx.lib.Trash(ctx, []string{a.ID, b.ID, ids.New()}, "admin-1")
	if err != nil || len(trashed) != 2 {
		t.Fatalf("Trash: %d files, %v", len(trashed), err)
	}
	if fx.exists(fx.inFolder(a.RelPath)) || !fx.exists(".trash/"+a.ID) {
		t.Fatal("the bytes didn't move to the trash")
	}
	if fx.exists(fx.inFolder("2026-09-27")) {
		t.Error("the empty day folder is still there")
	}
	if v, _ := fx.db.LibraryVersion(ctx); v == before {
		t.Error("the library version didn't change")
	}
	f, _ := fx.db.FileByID(ctx, a.ID)
	if f.State != db.StateTrashed || f.DeletedBy != "admin-1" || f.DeletedAt == nil {
		t.Fatalf("row after Trash: %+v", f)
	}
	// Trashing again changes nothing.
	if again, err := fx.lib.Trash(ctx, []string{a.ID}, "admin-1"); err != nil || len(again) != 0 {
		t.Fatalf("second Trash: %d, %v", len(again), err)
	}

	restored, err := fx.lib.Restore(ctx, []string{a.ID})
	if err != nil || len(restored) != 1 || restored[0].RelPath != a.RelPath {
		t.Fatalf("Restore: %+v, %v", restored, err)
	}
	data, err := os.ReadFile(fx.disk(a.RelPath))
	if err != nil || string(data) != "one" {
		t.Fatalf("restored file: %q, %v", data, err)
	}
	if f, _ := fx.db.FileByID(ctx, a.ID); f.State != db.StateReady || f.DeletedAt != nil || f.DeletedBy != "" {
		t.Fatalf("row after Restore: %+v", f)
	}
}

func TestRestoreNumbersANameThatWasTakenMeanwhile(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	old := fx.ready(t, "Menu.pdf", "old")
	if _, err := fx.lib.Trash(ctx, []string{old.ID}, ""); err != nil {
		t.Fatal(err)
	}
	newer := fx.ready(t, "Menu.pdf", "new") // the same day and name
	if newer.RelPath != old.RelPath {
		t.Fatalf("the new file got %q", newer.RelPath)
	}
	restored, err := fx.lib.Restore(ctx, []string{old.ID})
	if err != nil || len(restored) != 1 || restored[0].RelPath != "2026-09-27/Menu (2).pdf" {
		t.Fatalf("Restore: %+v, %v", restored, err)
	}
	for rel, want := range map[string]string{newer.RelPath: "new", restored[0].RelPath: "old"} {
		if data, _ := os.ReadFile(fx.disk(rel)); string(data) != want {
			t.Errorf("%s = %q, want %q", rel, data, want)
		}
	}
}

func TestPurge(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	a := fx.ready(t, "a.jpg", "a")
	b := fx.ready(t, "b.jpg", "b")
	var purged []string
	fx.lib.OnPurged = func(id string) { purged = append(purged, id) }

	fx.lib.Trash(ctx, []string{a.ID}, "")
	fx.now = t0.Add(20 * 24 * time.Hour)
	fx.lib.Trash(ctx, []string{b.ID}, "")

	// Thirty days after a was deleted, only a goes.
	fx.now = t0.Add(30*24*time.Hour + time.Minute)
	n, err := fx.lib.PurgeOld(ctx, 30)
	if err != nil || n != 1 {
		t.Fatalf("PurgeOld: %d, %v", n, err)
	}
	if _, err := fx.db.FileByID(ctx, a.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("a still has a row: %v", err)
	}
	if fx.exists(".trash/"+a.ID) || !fx.exists(".trash/"+b.ID) {
		t.Error("the wrong bytes left the trash")
	}

	// Emptying by hand; files in the library can't be purged.
	c := fx.ready(t, "c.jpg", "c")
	gone, err := fx.lib.Purge(ctx, []string{b.ID, c.ID})
	if err != nil || len(gone) != 1 || gone[0].ID != b.ID {
		t.Fatalf("Purge: %+v, %v", gone, err)
	}
	if len(purged) != 2 || !fx.exists(fx.inFolder(c.RelPath)) {
		t.Fatalf("purged %v; c still there: %v", purged, fx.exists(fx.inFolder(c.RelPath)))
	}
}

func TestReconcileFinishesCutShortTrashRestoreAndPurge(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	// Trashed in the database, but the bytes never moved.
	a := fx.ready(t, "a.jpg", "a")
	if _, err := fx.db.TrashFiles(ctx, []string{a.ID}, "", t0); err != nil {
		t.Fatal(err)
	}
	// Restored in the database, but the bytes are still in the trash.
	b := fx.ready(t, "b.jpg", "b")
	fx.lib.Trash(ctx, []string{b.ID}, "")
	if ok, err := fx.db.RestoreFile(ctx, b.ID, b.RelPath, t0); !ok || err != nil {
		t.Fatal(ok, err)
	}
	// Purged in the database, but the bytes are still in the trash.
	c := fx.ready(t, "c.jpg", "c")
	fx.lib.Trash(ctx, []string{c.ID}, "")
	if ok, err := fx.db.PurgeFile(ctx, c.ID); !ok || err != nil {
		t.Fatal(ok, err)
	}
	// Trashed, and then the bytes were deleted by hand.
	d := fx.ready(t, "d.jpg", "d")
	fx.lib.Trash(ctx, []string{d.ID}, "")
	os.Remove(filepath.Join(fx.layout.TrashDir(), d.ID))

	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if fx.exists(fx.inFolder(a.RelPath)) || !fx.exists(".trash/"+a.ID) {
		t.Error("a's bytes weren't moved to the trash")
	}
	if !fx.exists(fx.inFolder(b.RelPath)) || fx.exists(".trash/"+b.ID) {
		t.Error("b's bytes weren't moved back")
	}
	if fx.exists(".trash/" + c.ID) {
		t.Error("c's bytes are still in the trash")
	}
	if _, err := fx.db.FileByID(ctx, d.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("d is still listed: %v", err)
	}
}
