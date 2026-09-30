package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/ids"
)

var t0 = time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

func openTest(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "share.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestMigrateIsIdempotentAndRefusesNewerDatabases(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "share.db")
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Migrate(ctx, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(ctx, filepath.Join(dir, "backups")); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, "backups")); len(entries) != 0 {
		t.Errorf("a no-op migration made %d backups", len(entries))
	}
	if _, err := d.ExecContext(ctx, "PRAGMA user_version = 99"); err != nil {
		t.Fatal(err)
	}
	if err := d.Migrate(ctx, filepath.Join(dir, "backups")); err == nil {
		t.Fatal("Migrate accepted a database newer than the program")
	}
}

func TestServerIDIsStable(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	a, err := d.ServerID(ctx)
	if err != nil || !ids.Valid(a) {
		t.Fatalf("ServerID() = %q, %v", a, err)
	}
	b, _ := d.ServerID(ctx)
	if a != b {
		t.Fatalf("ServerID changed from %q to %q", a, b)
	}
}

func TestPinCodesAreNeverReused(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	p := Pin{ID: ids.New(), Code: "K7M2Q", Kind: PinPermanent, CreatedBy: "cli", CreatedAt: t0}
	if err := d.InsertPin(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := d.EndPin(ctx, p.ID, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	again := Pin{ID: ids.New(), Code: "K7M2Q", Kind: PinDay, CreatedBy: "cli", CreatedAt: t0}
	if err := d.InsertPin(ctx, again); !errors.Is(err, ErrConflict) {
		t.Fatalf("reusing an ended code: err = %v, want ErrConflict", err)
	}
	got, err := d.PinByCode(ctx, "K7M2Q")
	if err != nil || got.EndedAt == nil || got.LiveAt(t0.Add(2*time.Hour)) {
		t.Fatalf("PinByCode = %+v, %v", got, err)
	}
}

func TestEndExpiredPins(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	exp := t0.Add(24 * time.Hour)
	p := Pin{ID: ids.New(), Code: "4HX9T", Kind: PinDay, CreatedBy: "cli", CreatedAt: t0, ExpiresAt: &exp}
	if err := d.InsertPin(ctx, p); err != nil {
		t.Fatal(err)
	}
	if n, _ := d.EndExpiredPins(ctx, exp.Add(-time.Second)); n != 0 {
		t.Fatalf("ended %d PINs before they expired", n)
	}
	if n, _ := d.EndExpiredPins(ctx, exp); n != 1 {
		t.Fatalf("ended %d PINs, want 1", n)
	}
	got, _ := d.PinByID(ctx, p.ID)
	if got.EndedAt == nil || !got.EndedAt.Equal(exp) {
		t.Fatalf("EndedAt = %v, want %v", got.EndedAt, exp)
	}
}

func TestSessionsAndMovingUploads(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	oldPin := Pin{ID: ids.New(), Code: "AAAAA", Kind: PinPermanent, CreatedBy: "cli", CreatedAt: t0}
	newPin := Pin{ID: ids.New(), Code: "BBBBB", Kind: PinPermanent, CreatedBy: "cli", CreatedAt: t0}
	for _, p := range []Pin{oldPin, newPin} {
		if err := d.InsertPin(ctx, p); err != nil {
			t.Fatal(err)
		}
	}
	oldS := PinSession{ID: ids.New(), PinID: oldPin.ID, Client: "web", CreatedAt: t0, LastSeenAt: t0}
	newS := PinSession{ID: ids.New(), PinID: newPin.ID, Client: "web", CreatedAt: t0, LastSeenAt: t0}
	if err := d.InsertPinSession(ctx, oldS, []byte("old-hash")); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertPinSession(ctx, newS, []byte("new-hash")); err != nil {
		t.Fatal(err)
	}
	got, err := d.PinSessionByToken(ctx, []byte("old-hash"))
	if err != nil || got.ID != oldS.ID || got.Pin.Code != "AAAAA" {
		t.Fatalf("PinSessionByToken = %+v, %v", got, err)
	}
	if _, err := d.PinSessionByToken(ctx, []byte("nope")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown token: err = %v", err)
	}

	f := File{ID: ids.New(), Name: "a.jpg", Size: 10, CreatedAt: t0, UpdatedAt: t0, PinID: oldPin.ID, PinSessionID: oldS.ID}
	if err := d.InsertReceiving(ctx, f); err != nil {
		t.Fatal(err)
	}
	if n, _ := d.UnfinishedCount(ctx, oldS.ID, ""); n != 1 {
		t.Fatalf("UnfinishedCount = %d", n)
	}
	moved, err := d.MoveReceivingUploads(ctx, oldS.ID, newS.ID, newPin.ID, t0)
	if err != nil || moved != 1 {
		t.Fatalf("MoveReceivingUploads = %d, %v", moved, err)
	}
	after, _ := d.FileByID(ctx, f.ID)
	if after.PinSessionID != newS.ID || after.PinID != newPin.ID {
		t.Fatalf("file after move: %+v", after)
	}

	// The old session has nothing receiving any more, so once revoked long enough it goes.
	if err := d.RevokePinSession(ctx, oldS.ID, t0); err != nil {
		t.Fatal(err)
	}
	if n, _ := d.DeleteStalePinSessions(ctx, t0.Add(time.Hour)); n != 1 {
		t.Fatalf("DeleteStalePinSessions = %d, want 1", n)
	}
}

func TestFinalizeStatesAndCaseInsensitivePaths(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	a := File{ID: ids.New(), Name: "IMG.jpg", Size: 10, CreatedAt: t0, UpdatedAt: t0}
	b := File{ID: ids.New(), Name: "img.JPG", Size: 5, CreatedAt: t0, UpdatedAt: t0}
	for _, f := range []File{a, b} {
		if err := d.InsertReceiving(ctx, f); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := d.OutstandingBytes(ctx); n != 15 {
		t.Fatalf("OutstandingBytes = %d, want 15", n)
	}
	ok, err := d.MarkFinalizing(ctx, a.ID, "2026-09-27/IMG.jpg", "2026-09-27", t0)
	if err != nil || !ok {
		t.Fatalf("MarkFinalizing(a) = %v, %v", ok, err)
	}
	if taken, _ := d.RelPathTaken(ctx, "2026-09-27/img.jpg"); !taken {
		t.Fatal("RelPathTaken ignores case differences")
	}
	if _, err := d.MarkFinalizing(ctx, b.ID, "2026-09-27/img.JPG", "2026-09-27", t0); !errors.Is(err, ErrConflict) {
		t.Fatalf("MarkFinalizing(b) with a case-only difference: err = %v, want ErrConflict", err)
	}
	if again, _ := d.MarkFinalizing(ctx, a.ID, "2026-09-27/other.jpg", "2026-09-27", t0); again {
		t.Fatal("MarkFinalizing claimed a second path for the same upload")
	}

	v0, _ := d.LibraryVersion(ctx)
	if err := d.MarkReady(ctx, a.ID, "image/jpeg", KindPhoto, t0); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkReady(ctx, a.ID, "image/jpeg", KindPhoto, t0); err != nil {
		t.Fatalf("MarkReady twice: %v", err)
	}
	if v1, _ := d.LibraryVersion(ctx); v1 != v0+1 {
		t.Fatalf("library version %d → %d, want one step", v0, v1)
	}
	got, _ := d.FileByID(ctx, a.ID)
	if got.State != StateReady || got.Received != 10 || got.UploadedAt == nil || got.Kind != KindPhoto {
		t.Fatalf("ready file: %+v", got)
	}
	unfinished, _ := d.FilesInStates(ctx, StateReceiving, StateFinalizing)
	if len(unfinished) != 1 || unfinished[0].ID != b.ID {
		t.Fatalf("FilesInStates = %+v", unfinished)
	}
}
