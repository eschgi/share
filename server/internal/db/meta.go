package db

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/eschgi/share/server/internal/ids"
)

// ErrConflict is returned when a unique value (a PIN code, a library path) is already taken.
var ErrConflict = errors.New("already exists")

// isUniqueViolation reports whether err comes from a UNIQUE constraint.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Meta returns a value from the meta table.
func (d *DB) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := d.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return v, err
}

// SetMeta stores a value in the meta table.
func (d *DB) SetMeta(ctx context.Context, key, value string) error {
	_, err := d.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// ServerID returns this server's permanent random id, creating it on first use. Apps compare
// it to make sure the local address reaches the same server as the public one.
func (d *DB) ServerID(ctx context.Context) (string, error) {
	if _, err := d.ExecContext(ctx, "INSERT OR IGNORE INTO meta (key, value) VALUES ('server_id', ?)", ids.New()); err != nil {
		return "", err
	}
	return d.Meta(ctx, "server_id")
}

// LibraryVersion is a counter that grows with every change to the library, so clients can
// ask "anything new?" cheaply.
func (d *DB) LibraryVersion(ctx context.Context) (int64, error) {
	v, err := d.Meta(ctx, "library_version")
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(v, 10, 64)
}

func bumpLibraryVersion(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('library_version', '1')
		ON CONFLICT (key) DO UPDATE SET value = CAST(CAST(value AS INTEGER) + 1 AS TEXT)`)
	return err
}
