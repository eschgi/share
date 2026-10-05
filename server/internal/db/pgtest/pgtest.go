// Package pgtest lets the tests run against PostgreSQL: with SHARE_TEST_POSTGRES set to a
// server's address, every test gets a schema of its own there, dropped when it ends.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Enabled reports whether the tests run against PostgreSQL rather than SQLite.
func Enabled() bool { return os.Getenv("SHARE_TEST_POSTGRES") != "" }

// URL makes a fresh schema for the test and returns an address that uses it; "" when the tests
// run against SQLite.
func URL(t testing.TB) string {
	t.Helper()
	base := os.Getenv("SHARE_TEST_POSTGRES")
	if base == "" {
		return ""
	}
	b := make([]byte, 8)
	rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	exec := func(sql string) error {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, base)
		if err != nil {
			return err
		}
		defer conn.Close(ctx)
		_, err = conn.Exec(ctx, sql)
		return err
	}
	if err := exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatalf("SHARE_TEST_POSTGRES: %v", err)
	}
	t.Cleanup(func() {
		if err := exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Logf("dropping %s: %v", schema, err)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}
