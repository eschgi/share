package db

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/ids"
)

// migrateTo brings a new database to version n only, as an older program would have.
func migrateTo(t *testing.T, d *DB, n int) {
	t.Helper()
	migrations, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:n] {
		if _, err := d.Exec(m.sql); err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		if _, err := d.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationPutsEverythingIntoTheFirstFolder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := Open(filepath.Join(dir, "share.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	migrateTo(t, d, 5)

	// A server from before folders: an admin, a member, invites in every state, two PINs and
	// files that are ready, trashed and still arriving.
	for _, q := range []string{
		`INSERT INTO users (id, name, role, created_at, created_by) VALUES ('admin', 'Stefan', 'admin', 1, 'cli'), ('maria', 'Maria', 'member', 1, 'cli')`,
		`INSERT INTO invites (id, token_hash, name, role, user_id, created_by, created_at, expires_at, used_at, revoked_at) VALUES
			('open', x'01', 'Oma Rosa', 'member', NULL, 'admin', 1, 9999999999999, NULL, NULL),
			('openadmin', x'02', 'Peter', 'admin', NULL, 'admin', 1, 9999999999999, NULL, NULL),
			('used', x'03', 'Anna', 'member', NULL, 'admin', 1, 9999999999999, 2, NULL),
			('phone', x'04', 'Maria', 'member', 'maria', 'admin', 1, 9999999999999, NULL, NULL),
			('expired', x'05', 'Marco', 'member', NULL, 'admin', 1, 2, NULL, NULL)`,
		`INSERT INTO pins (id, code, kind, created_by, created_at, expires_at, ended_at) VALUES
			('live', 'K7M2Q', 'permanent', 'admin', 1, NULL, NULL), ('ended', '4HX9T', 'day', 'admin', 1, 2, 2)`,
		`INSERT INTO files (id, state, name, size, received, rel_path, upload_day, created_at, updated_at) VALUES
			('ready', 'ready', 'IMG_1.jpg', 10, 10, '2026-09-26/IMG_1.jpg', '2026-09-26', 1758844800000, 1),
			('trashed', 'trashed', 'IMG_2.jpg', 10, 10, '2026-09-26/IMG_2.jpg', '2026-09-26', 1758931200000, 1),
			('arriving', 'receiving', 'IMG_3.jpg', 10, 4, NULL, NULL, 1759017600000, 1)`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.Migrate(ctx, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}

	first := Folder{ID: ids.New(), Name: "Share", Dir: "", CreatedBy: "first-start"}
	got, made, err := d.EnsureFirstFolder(ctx, first, t0)
	if err != nil || !made || got.ID != first.ID || got.Name != "Share" {
		t.Fatalf("EnsureFirstFolder = %+v, %v, %v", got, made, err)
	}
	if want := time.UnixMilli(1758844800000).UTC(); !got.CreatedAt.Equal(want) {
		t.Errorf("the folder dates from %v, want the oldest file's %v", got.CreatedAt, want)
	}
	for _, id := range []string{"ready", "trashed", "arriving"} {
		if f, _ := d.FileByID(ctx, id); f.FolderID != first.ID {
			t.Errorf("file %s is in folder %q", id, f.FolderID)
		}
	}
	for _, id := range []string{"live", "ended"} {
		if p, _ := d.PinByID(ctx, id); p.FolderID != first.ID {
			t.Errorf("PIN %s sends into %q", id, p.FolderID)
		}
	}
	if folders, _ := d.FoldersOf(ctx, "maria"); len(folders) != 1 || folders[0].ID != first.ID {
		t.Errorf("Maria sees %+v", folders)
	}
	if folders, _ := d.FoldersOf(ctx, "admin"); len(folders) != 0 {
		t.Errorf("the admin got rows %+v; admins see every folder without them", folders)
	}
	invites := map[string]bool{}
	rows, err := d.Query("SELECT invite_id FROM invite_folders WHERE folder_id = ?", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		invites[id] = true
	}
	rows.Close()
	if len(invites) != 1 || !invites["open"] {
		t.Errorf("invites that give the folder: %v, want only the open one for a new member", invites)
	}

	// Once there is a folder, nothing changes any more.
	again, made, err := d.EnsureFirstFolder(ctx, Folder{ID: ids.New(), Name: "Other", CreatedBy: "first-start"}, t0)
	if err != nil || made || again.ID != first.ID {
		t.Fatalf("second EnsureFirstFolder = %+v, %v, %v", again, made, err)
	}
	if live, _ := d.LiveFolders(ctx); len(live) != 1 {
		t.Fatalf("%d folders after a second EnsureFirstFolder", len(live))
	}
}

func TestNewMembersGetTheirFolders(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	family, kindergarten := testFolder(t, d, "Family"), testFolder(t, d, "Kindergarten")
	in := Invite{ID: ids.New(), Name: "Oma Rosa", Role: RoleMember, CreatedBy: "cli", CreatedAt: t0, ExpiresAt: t0.Add(time.Hour),
		Folders: []string{kindergarten}}
	if err := d.InsertInvite(ctx, in, []byte("invite")); err != nil {
		t.Fatal(err)
	}
	u := User{ID: ids.New(), Name: "Oma Rosa", Role: RoleMember, CreatedAt: t0, CreatedBy: "cli"}
	dv := Device{ID: ids.New(), UserID: u.ID, Name: "Pixel", CreatedAt: t0, LastSeenAt: t0}
	if err := d.UseInvite(ctx, in.ID, u, dv, []byte("device"), t0); err != nil {
		t.Fatal(err)
	}
	if folders, _ := d.FoldersOf(ctx, u.ID); len(folders) != 1 || folders[0].ID != kindergarten {
		t.Fatalf("after the invite Oma Rosa sees %+v", folders)
	}

	// An admin who becomes a member keeps seeing every folder.
	admin := User{ID: ids.New(), Name: "Peter", Role: RoleAdmin, CreatedAt: t0, CreatedBy: "cli"}
	other := User{ID: ids.New(), Name: "Stefan", Role: RoleAdmin, CreatedAt: t0, CreatedBy: "cli"}
	for _, x := range []User{admin, other} {
		if err := d.InsertUser(ctx, x); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.SetRole(ctx, admin.ID, RoleMember); err != nil {
		t.Fatal(err)
	}
	var seen []string
	folders, _ := d.FoldersOf(ctx, admin.ID)
	for _, f := range folders {
		seen = append(seen, f.ID)
	}
	if len(seen) != 2 || !slices.Contains(seen, family) || !slices.Contains(seen, kindergarten) {
		t.Fatalf("the former admin sees %v", seen)
	}
}
