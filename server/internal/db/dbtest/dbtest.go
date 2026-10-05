// Package dbtest opens a fresh, migrated database for a test: SQLite in dir, or a schema of its
// own on the PostgreSQL server in SHARE_TEST_POSTGRES.
package dbtest

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/db/pgtest"
)

// Open opens and migrates the test's database and closes it when the test ends.
func Open(t testing.TB, dir string) *db.DB {
	t.Helper()
	ctx := context.Background()
	var d *db.DB
	var err error
	if url := pgtest.URL(t); url != "" {
		d, err = db.OpenPostgres(ctx, url)
	} else {
		d, err = db.Open(filepath.Join(dir, "share.db"))
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(ctx, filepath.Join(dir, "backups")); err != nil {
		t.Fatal(err)
	}
	return d
}
