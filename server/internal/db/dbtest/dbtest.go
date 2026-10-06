// Package dbtest opens a fresh, migrated database for a test: a schema of its own on the
// PostgreSQL server in SHARE_TEST_POSTGRES.
package dbtest

import (
	"context"
	"testing"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/db/pgtest"
)

// Open opens and migrates the test's database and closes it when the test ends.
func Open(t testing.TB) *db.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return d
}
