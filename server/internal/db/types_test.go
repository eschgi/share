package db

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/ids"
)

// Ids, times and days come back as they went in: an id as its UUID's text, a time in UTC to
// the millisecond, a day as YYYY-MM-DD.
func TestTypesRoundTrip(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	at := time.Date(2026, 10, 6, 14, 30, 15, 123_600_000, time.FixedZone("CEST", 2*60*60))
	u := User{ID: ids.New(), Name: "Ötzi", Role: RoleMember, CreatedAt: at, CreatedBy: "test"}
	if err := d.InsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := d.UserByID(ctx, u.ID)
	if err != nil || got.ID != u.ID {
		t.Fatalf("UserByID(%s) = %+v, %v", u.ID, got, err)
	}
	if want := at.Round(time.Millisecond); !got.CreatedAt.Equal(want) || got.CreatedAt.Location() != time.UTC {
		t.Errorf("created at %v, want %v in UTC", got.CreatedAt, want)
	}
	if _, err := d.UserByID(ctx, "u7ld5x2k7mbqz4bwdbyj6qsqxa"); !errors.Is(err, ErrNotFound) {
		t.Errorf("looking up something that isn't an id: %v, want ErrNotFound", err)
	}

	folder := testFolder(t, d, "Family")
	f := File{ID: ids.New(), Name: "a.jpg", Size: 1, CreatedAt: at, UpdatedAt: at, FolderID: folder}
	if err := d.InsertReceiving(ctx, f); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MarkFinalizing(ctx, f.ID, "2026-10-06/a.jpg", "2026-10-06", at); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkReady(ctx, f.ID, "image/jpeg", KindPhoto, at); err != nil {
		t.Fatal(err)
	}
	if got, err := d.FileByID(ctx, f.ID); err != nil || got.UploadDay != "2026-10-06" || got.FolderID != folder || got.DeletedAt != nil {
		t.Errorf("FileByID = %+v, %v", got, err)
	}
	if days, err := d.LibraryDays(ctx, LibraryFilter{Folders: []string{folder}}); err != nil || len(days) != 1 || days[0].Day != "2026-10-06" {
		t.Errorf("LibraryDays = %+v, %v", days, err)
	}
	if err := d.SetJobRun(ctx, "trash", at); err != nil {
		t.Fatal(err)
	}
	if ran, err := d.JobRun(ctx, "trash"); err != nil || !ran.Equal(at.Round(time.Millisecond)) {
		t.Errorf("JobRun = %v, %v", ran, err)
	}
	if never, err := d.JobRun(ctx, "thumbs"); err != nil || !never.IsZero() {
		t.Errorf("a job that never ran: %v, %v", never, err)
	}
}

// An invite for a new phone of someone who has an account carries the person's own key, with
// no folder; it goes out once, with the folders' keys.
func TestInviteKeysGoOutOnce(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	u := User{ID: ids.New(), Name: "Maria", Role: RoleMember, CreatedAt: t0, CreatedBy: "test"}
	if err := d.InsertUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	folder := testFolder(t, d, "Family")
	person, folderKey := []byte("the person's key"), []byte("the folder's key")
	in := Invite{ID: ids.New(), Name: "Maria", Role: RoleMember, UserID: u.ID, CreatedBy: "test", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour),
		Keys: []InviteKey{{Locked: person}, {FolderID: folder, Version: 1, Locked: folderKey}}}
	if err := d.InsertInvite(ctx, in, []byte("invite")); err != nil {
		t.Fatal(err)
	}
	dv := Device{ID: ids.New(), UserID: u.ID, Name: "Pixel", CreatedAt: t0, LastSeenAt: t0}
	keys, err := d.UseInvite(ctx, in.ID, u, dv, []byte("device"), t0)
	if err != nil || len(keys) != 2 || keys[0].FolderID != "" || !bytes.Equal(keys[0].Locked, person) ||
		keys[1].FolderID != folder || keys[1].Version != 1 || !bytes.Equal(keys[1].Locked, folderKey) {
		t.Fatalf("UseInvite = %+v, %v", keys, err)
	}
	var left int
	if err := d.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM invite_keys) + (SELECT COUNT(*) FROM invites WHERE person_key IS NOT NULL)").Scan(&left); err != nil || left != 0 {
		t.Errorf("%d keys left after the invite was used, %v", left, err)
	}
}
