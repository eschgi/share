package db

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/ids"
)

// The first start makes one folder; later starts find it and change nothing.
func TestEnsureFirstFolderOnce(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	first := Folder{ID: ids.New(), Name: "Share", Dir: "Share", CreatedBy: "first-start"}
	got, made, err := d.EnsureFirstFolder(ctx, first, t0)
	if err != nil || !made || got.ID != first.ID || !got.CreatedAt.Equal(t0) || got.RenamingFrom != nil {
		t.Fatalf("EnsureFirstFolder = %+v, %v, %v", got, made, err)
	}
	again, made, err := d.EnsureFirstFolder(ctx, Folder{ID: ids.New(), Name: "Other", Dir: "Other", CreatedBy: "first-start"}, t0)
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
	if _, err := d.UseInvite(ctx, in.ID, u, dv, []byte("device"), t0); err != nil {
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

func TestNoFoldersShowNothing(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	family := testFolder(t, d, "Family")
	f := File{ID: ids.New(), Name: "a.jpg", Size: 10, CreatedAt: t0, UpdatedAt: t0, FolderID: family}
	if err := d.InsertReceiving(ctx, f); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MarkFinalizing(ctx, f.ID, "2026-09-27/a.jpg", "2026-09-27", t0); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkReady(ctx, f.ID, "image/jpeg", KindPhoto, t0); err != nil {
		t.Fatal(err)
	}
	if days, _ := d.LibraryDays(ctx, LibraryFilter{}); len(days) != 0 {
		t.Fatalf("no folders showed %v", days)
	}
	if ids, _, _ := d.LibraryIDs(ctx, LibraryFilter{}); len(ids) != 0 {
		t.Fatalf("no folders showed %v", ids)
	}
	if days, _ := d.LibraryDays(ctx, LibraryFilter{Folders: []string{family}}); len(days) != 1 {
		t.Fatalf("the folder showed %v", days)
	}
}

// The queries that must stay quick use their indexes. The library's pages come newest first
// from an index, without sorting: files_by_folder or files_ready_by_time, whichever the
// statistics favour. The lookups that ignore case use the casefold() indexes.
func TestQueriesUseTheirIndexes(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	explain := func(query string, args ...any) string {
		t.Helper()
		var plan []string
		err := d.Tx(ctx, func(tx *sql.Tx) error {
			// Empty tables are cheapest to read whole and to sort, unless that is ruled out.
			if _, err := tx.ExecContext(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, "SET LOCAL enable_sort = off"); err != nil {
				return err
			}
			rows, err := tx.QueryContext(ctx, "EXPLAIN "+query, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var line string
				if err := rows.Scan(&line); err != nil {
					return err
				}
				plan = append(plan, line)
			}
			return rows.Err()
		})
		if err != nil {
			t.Fatal(err)
		}
		return strings.Join(plan, "\n")
	}
	page := func(folders ...string) (string, []any) {
		w, args := LibraryFilter{Folders: folders}.where()
		return "SELECT id FROM files WHERE " + w + " ORDER BY uploaded_at DESC, id LIMIT 10", args
	}
	one, oneArgs := page(ids.New())
	several, severalArgs := page(ids.New(), ids.New(), ids.New())
	for _, tc := range []struct {
		name, query string
		args        []any
		index       string
	}{
		{"a page of one folder", one, oneArgs, "Index Scan using files_"},
		{"a page of several folders", several, severalArgs, "Index Scan using files_"},
		{"UserByUsername", userByUsername, []any{"ÖTZI"}, "users_username"},
		{"RelPathTaken", relPathTaken, []any{ids.New(), "2026-09-27/ÜBER.PDF"}, "files_rel_path"},
		{"DirTaken", dirTaken, []any{"ÄRZTE"}, "folders_dir"},
	} {
		plan := explain(tc.query, tc.args...)
		if !strings.Contains(plan, tc.index) || strings.Contains(plan, "Sort") || strings.Contains(plan, "Seq Scan") {
			t.Errorf("%s doesn't use %s alone:\n%s", tc.name, tc.index, plan)
		}
	}
}
