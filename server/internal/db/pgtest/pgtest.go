// Package pgtest gives every test a schema of its own on the PostgreSQL server in
// SHARE_TEST_POSTGRES, dropped when the test ends.
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

// URL makes a fresh schema for the test and returns an address that uses it. Without
// SHARE_TEST_POSTGRES the test fails: the server's tests need a PostgreSQL server.
func URL(t testing.TB) string {
	t.Helper()
	base := os.Getenv("SHARE_TEST_POSTGRES")
	if base == "" {
		t.Fatal("SHARE_TEST_POSTGRES isn't set: the server's tests need a PostgreSQL server to make schemas in (README, Development)")
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
