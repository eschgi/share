package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

func TestSanitizeName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"IMG_1234.jpg", "IMG_1234.jpg"},
		{"../../etc/passwd", "passwd"},
		{`C:\Users\maria\Desktop\Rezept.docx`, "Rezept.docx"},
		{"Invoice 12:30.pdf", "Invoice 12_30.pdf"},
		{"what?.txt", "what_.txt"},
		{"trailing. . ", "trailing"},
		{".hidden", "_.hidden"},
		{"CON.txt", "_CON.txt"},
		{"com1", "_com1"},
		{"console.txt", "console.txt"},
		{"tab\there\n.jpg", "tabhere.jpg"},
		{"", "file"},
		{"...", "file"},
		{"Omas Rezepte ä ö ü.pdf", "Omas Rezepte ä ö ü.pdf"},
	} {
		if got := SanitizeName(tc.in); got != tc.want {
			t.Errorf("SanitizeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	long := strings.Repeat("ä", 200) + ".jpeg"
	got := SanitizeName(long)
	if len(got) > maxNameBytes || !strings.HasSuffix(got, ".jpeg") {
		t.Errorf("long name: %d bytes, %q", len(got), got[len(got)-10:])
	}
}

func TestNumbered(t *testing.T) {
	if got := numbered("IMG_1.jpg", 2); got != "IMG_1 (2).jpg" {
		t.Errorf("numbered = %q", got)
	}
	if got := numbered("README", 3); got != "README (3)" {
		t.Errorf("numbered without extension = %q", got)
	}
	long := strings.Repeat("x", 251) + ".jpg"
	if got := numbered(long, 12); len(got) > maxNameBytes || !strings.HasSuffix(got, " (12).jpg") {
		t.Errorf("numbered(long) = %d bytes, %q", len(got), got)
	}
}

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name       string
		head       string
		mime, kind string
	}{
		{"a.JPG", "", "image/jpeg", db.KindPhoto},
		{"clip.mov", "", "video/quicktime", db.KindVideo},
		{"budget.xlsx", "PK", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", db.KindDocument},
		{"noext", "\x89PNG\r\n\x1a\n", "image/png", db.KindPhoto},
		{"page", "<!DOCTYPE html><html>", "application/octet-stream", db.KindDocument},
	} {
		mime, kind := Classify(tc.name, []byte(tc.head))
		if mime != tc.mime || kind != tc.kind {
			t.Errorf("Classify(%q) = %s, %s; want %s, %s", tc.name, mime, kind, tc.mime, tc.kind)
		}
	}
}

var t0 = time.Date(2026, 9, 27, 21, 30, 0, 0, time.UTC) // 23:30 in Rome: still the 27th there

type fixture struct {
	lib    *Library
	db     *db.DB
	layout Layout
	now    time.Time
	logs   []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	l := Layout{StorageDir: filepath.Join(dir, "storage"), DataDir: filepath.Join(dir, "data")}
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(l.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), l.BackupDir()); err != nil {
		t.Fatal(err)
	}
	fx := &fixture{db: d, layout: l, now: t0}
	rome, _ := time.LoadLocation("Europe/Rome")
	lib, err := OpenLibrary(d, l, rome, func() time.Time { return fx.now }, func(f string, a ...any) {
		fx.logs = append(fx.logs, f)
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	fx.lib = lib
	return fx
}

// receiving creates an upload row and writes `written` of its `content` into .uploads.
func (fx *fixture) receiving(t *testing.T, name, content string, written int) string {
	t.Helper()
	id := ids.New()
	f := db.File{ID: id, Name: name, Size: int64(len(content)), CreatedAt: fx.now, UpdatedAt: fx.now}
	if err := fx.db.InsertReceiving(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.layout.UploadsDir(), id), []byte(content[:written]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fx.layout.UploadsDir(), id+".info"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestFinalizeMovesIntoTheDayFolder(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	id := fx.receiving(t, "IMG_1.jpg", "jpegbytes", 9)
	var readied []string
	fx.lib.OnReady = func(id string) { readied = append(readied, id) }

	if err := fx.lib.Finalize(ctx, id); err != nil {
		t.Fatal(err)
	}
	got, _ := fx.db.FileByID(ctx, id)
	if got.State != db.StateReady || got.RelPath != "2026-09-27/IMG_1.jpg" || got.UploadDay != "2026-09-27" {
		t.Fatalf("after Finalize: %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(fx.layout.StorageDir, "2026-09-27", "IMG_1.jpg"))
	if err != nil || string(data) != "jpegbytes" {
		t.Fatalf("library file: %q, %v", data, err)
	}
	for _, leftover := range []string{id, id + ".info"} {
		if _, err := os.Stat(filepath.Join(fx.layout.UploadsDir(), leftover)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf(".uploads/%s is still there", leftover)
		}
	}
	if len(readied) != 1 {
		t.Errorf("OnReady called %d times", len(readied))
	}
	if err := fx.lib.Finalize(ctx, id); err != nil || len(readied) != 1 {
		t.Fatalf("second Finalize: %v, OnReady calls %d", err, len(readied))
	}
}

func TestFinalizeRefusesIncompleteUploads(t *testing.T) {
	fx := newFixture(t)
	id := fx.receiving(t, "big.mov", "0123456789", 4)
	if err := fx.lib.Finalize(context.Background(), id); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v, want ErrIncomplete", err)
	}
}

func TestFinalizeNumbersNameCollisionsIgnoringCase(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	a := fx.receiving(t, "IMG.jpg", "a", 1)
	b := fx.receiving(t, "img.JPG", "b", 1)
	if err := os.MkdirAll(filepath.Join(fx.layout.StorageDir, "2026-09-27"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Someone copied a file with the next free name into the folder by hand.
	if err := os.WriteFile(filepath.Join(fx.layout.StorageDir, "2026-09-27", "img (2).JPG"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b} {
		if err := fx.lib.Finalize(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	fb, _ := fx.db.FileByID(ctx, b)
	if fb.RelPath != "2026-09-27/img (3).JPG" {
		t.Fatalf("second file got %q", fb.RelPath)
	}
}

func TestFinalizeResumesAfterACrash(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()

	// Crash after the path was claimed but before the rename.
	a := fx.receiving(t, "a.pdf", "%PDF", 4)
	if ok, err := fx.db.MarkFinalizing(ctx, a, "2026-09-27/a.pdf", "2026-09-27", t0); !ok || err != nil {
		t.Fatal(ok, err)
	}
	// Crash after the rename but before the row became ready.
	b := fx.receiving(t, "b.pdf", "%PDF", 4)
	if ok, err := fx.db.MarkFinalizing(ctx, b, "2026-09-27/b.pdf", "2026-09-27", t0); !ok || err != nil {
		t.Fatal(ok, err)
	}
	os.MkdirAll(filepath.Join(fx.layout.StorageDir, "2026-09-27"), 0o755)
	if err := os.Rename(filepath.Join(fx.layout.UploadsDir(), b), filepath.Join(fx.layout.StorageDir, "2026-09-27", "b.pdf")); err != nil {
		t.Fatal(err)
	}

	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a, b} {
		f, _ := fx.db.FileByID(ctx, id)
		if f.State != db.StateReady || f.Mime != "application/pdf" {
			t.Errorf("%s after Reconcile: %+v", id, f)
		}
	}
}

func TestReconcileFinalizesCompleteAndDropsAbandonedUploads(t *testing.T) {
	fx := newFixture(t)
	ctx := context.Background()
	complete := fx.receiving(t, "done.jpg", "abc", 3) // the final response was lost
	idle := fx.receiving(t, "half.mov", "abcdef", 2)
	orphan := ids.New()
	os.WriteFile(filepath.Join(fx.layout.UploadsDir(), orphan), []byte("x"), 0o644)
	old := t0.Add(-3 * time.Hour)
	os.Chtimes(filepath.Join(fx.layout.UploadsDir(), orphan), old, old)

	fx.now = t0.Add(2 * time.Hour)
	if err := fx.lib.Reconcile(ctx, time.Hour); err != nil {
		t.Fatal(err)
	}
	if f, _ := fx.db.FileByID(ctx, complete); f.State != db.StateReady {
		t.Errorf("complete upload is %s", f.State)
	}
	if _, err := fx.db.FileByID(ctx, idle); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("idle upload still has a row: %v", err)
	}
	for _, name := range []string{idle, idle + ".info", orphan} {
		if _, err := os.Stat(filepath.Join(fx.layout.UploadsDir(), name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf(".uploads/%s survived Reconcile", name)
		}
	}
}

func TestCheck(t *testing.T) {
	dir := t.TempDir()
	l := Layout{StorageDir: filepath.Join(dir, "s"), DataDir: filepath.Join(dir, "d")}
	os.MkdirAll(l.StorageDir, 0o755)
	if r := Check(l); len(r.Problems) != 1 || !strings.Contains(r.Problems[0], "share init") {
		t.Fatalf("without marker: %+v", r)
	}
	if err := Init(l); err != nil {
		t.Fatal(err)
	}
	// Test folders may live on tmpfs, which Check rightly complains about; everything else
	// must be fine after init.
	for _, p := range Check(l).Problems {
		if !strings.Contains(p, "tmpfs") {
			t.Errorf("after init: %s", p)
		}
	}
}

func TestWaitForMarker(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := WaitForMarker(ctx, dir, func(string, ...any) {}); err == nil {
		t.Fatal("WaitForMarker returned without a marker")
	}
	os.WriteFile(filepath.Join(dir, MarkerName), nil, 0o644)
	if err := WaitForMarker(context.Background(), dir, func(string, ...any) {}); err != nil {
		t.Fatal(err)
	}
}
