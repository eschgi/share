package db

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"time"

	"github.com/eschgi/share/server/internal/ids"
)

// ErrConflict is returned when a unique value (a PIN code, a library path) is already taken.
var ErrConflict = errors.New("already exists")

// Meta returns a value from the meta table.
func (d *DB) Meta(ctx context.Context, key string) (string, error) {
	var v string
	err := d.pool.QueryRowContext(ctx, "SELECT value FROM meta WHERE key = $1", key).Scan(&v)
	if noRow(err) {
		return "", ErrNotFound
	}
	return v, err
}

// SetMeta stores a value in the meta table.
func (d *DB) SetMeta(ctx context.Context, key, value string) error {
	_, err := d.pool.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

func setMeta(ctx context.Context, tx *sql.Tx, key, value string) error {
	_, err := tx.ExecContext(ctx,
		"INSERT INTO meta (key, value) VALUES ($1, $2) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
		key, value)
	return err
}

// JobRun is when the periodic job of that name last ran without an error; the zero time if
// it never did.
func (d *DB) JobRun(ctx context.Context, name string) (time.Time, error) {
	var at time.Time
	err := d.pool.QueryRowContext(ctx, "SELECT ran_at FROM job_runs WHERE name = $1", name).Scan(&at)
	if noRow(err) {
		return time.Time{}, nil
	}
	return at, err
}

// SetJobRun records a run of the periodic job of that name.
func (d *DB) SetJobRun(ctx context.Context, name string, at time.Time) error {
	_, err := d.pool.ExecContext(ctx, `INSERT INTO job_runs (name, ran_at) VALUES ($1, $2)
		ON CONFLICT (name) DO UPDATE SET ran_at = excluded.ran_at`, name, at)
	return err
}

// ServerID returns this server's permanent random id, creating it on first use. Apps compare
// it to make sure the local address reaches the same server as the public one.
func (d *DB) ServerID(ctx context.Context) (string, error) {
	if _, err := d.pool.ExecContext(ctx, "INSERT INTO meta (key, value) VALUES ('server_id', $1) ON CONFLICT DO NOTHING", ids.New()); err != nil {
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
		ON CONFLICT (key) DO UPDATE SET value = CAST(CAST(meta.value AS INTEGER) + 1 AS TEXT)`)
	return err
}
