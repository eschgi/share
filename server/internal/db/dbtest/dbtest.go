// Package dbtest opens a fresh, migrated database for a test: a schema of its own on the
// PostgreSQL server in SHARE_TEST_POSTGRES.
package dbtest

import (
	"context"
	"testing"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/db/internal/rawsql"
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

// Exec runs SQL of the test's own, to set up what no method of db.DB makes.
func Exec(t testing.TB, d *db.DB, query string, args ...any) {
	t.Helper()
	if _, err := rawsql.Pool(d).ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

// Count runs a query of the test's own that counts something.
func Count(t testing.TB, d *db.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := rawsql.Pool(d).QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
