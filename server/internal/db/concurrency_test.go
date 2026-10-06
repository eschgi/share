package db

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"

	"github.com/eschgi/share/server/internal/ids"
)

// Two admins demoted at the same time: one of them has to stay (serializable transactions, run
// again after a conflict).
func TestTheLastAdminStaysUnderConcurrency(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	a, b := ids.New(), ids.New()
	for _, id := range []string{a, b} {
		if err := d.InsertUser(ctx, User{ID: id, Name: "Admin " + id[:4], Role: RoleAdmin, CreatedAt: t0, CreatedBy: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	for round := range 10 {
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, id := range []string{a, b} {
			wg.Go(func() { errs[i] = d.SetRole(ctx, id, RoleMember) })
		}
		wg.Wait()
		var demoted, refused int
		for _, err := range errs {
			switch {
			case err == nil:
				demoted++
			case errors.Is(err, ErrLastAdmin):
				refused++
			default:
				t.Fatalf("round %d: %v", round, err)
			}
		}
		var admins int
		if err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&admins); err != nil {
			t.Fatal(err)
		}
		if demoted != 1 || refused != 1 || admins != 1 {
			t.Fatalf("round %d: %d demoted, %d refused, %d admins left", round, demoted, refused, admins)
		}
		for _, id := range []string{a, b} {
			if err := d.SetRole(ctx, id, RoleAdmin); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Two first admins at the same time, as from two setup pages: only one of them is.
func TestOneFirstUser(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Go(func() {
			u := User{ID: ids.New(), Name: "Admin", Role: RoleAdmin, CreatedAt: t0, CreatedBy: "setup"}
			dv := Device{ID: ids.New(), UserID: u.ID, Name: "Browser", Client: ClientWeb, CreatedAt: t0, LastSeenAt: t0}
			errs[i] = d.InsertFirstUser(ctx, u, dv, []byte(u.ID))
		})
	}
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) || !errors.Is(errs[0], ErrNotFirst) && !errors.Is(errs[1], ErrNotFirst) {
		t.Fatalf("errors %v", errs)
	}
	if n, err := d.UserCount(ctx); n != 1 || err != nil {
		t.Errorf("%d people, %v", n, err)
	}
}

// A transaction that collides with another one runs again and then goes through.
func TestTxRunsAgainAfterAConflict(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if err := d.SetMeta(ctx, "library_version", "40"); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		attempts++
		var v string
		if err := tx.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = 'library_version'").Scan(&v); err != nil {
			return err
		}
		if attempts == 1 {
			// Another transaction changes what this one read, and commits first.
			if err := d.SetMeta(ctx, "library_version", "41"); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE meta SET value = '42' WHERE key = 'library_version'")
		return err
	})
	if err != nil || attempts != 2 {
		t.Fatalf("after %d attempts: %v", attempts, err)
	}
	if v, _ := d.Meta(ctx, "library_version"); v != "42" {
		t.Errorf("the version is %s", v)
	}
}

func TestUsernamesIgnoreCase(t *testing.T) {
	d := openTest(t)
	ctx := context.Background()
	if err := d.InsertUser(ctx, User{ID: ids.New(), Name: "Stefan", Username: "Stefan", Role: RoleAdmin, CreatedAt: t0, CreatedBy: "test"}); err != nil {
		t.Fatal(err)
	}
	if u, err := d.UserByUsername(ctx, "STEFAN"); err != nil || u.Name != "Stefan" {
		t.Errorf("found %+v, %v", u, err)
	}
	if err := d.InsertUser(ctx, User{ID: ids.New(), Name: "Other", Username: "stefan", Role: RoleMember, CreatedAt: t0, CreatedBy: "test"}); !errors.Is(err, ErrConflict) {
		t.Errorf("the same username in other letters: %v", err)
	}
	if users, err := d.Users(ctx); err != nil || len(users) != 1 {
		t.Errorf("users %v, %v", users, err)
	}
}
