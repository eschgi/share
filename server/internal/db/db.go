// Package db is the SQLite persistence layer: opening, migrations and every query. Rules
// about who may do what live in the packages that call it.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrNotFound is returned when a looked-up row doesn't exist.
var ErrNotFound = errors.New("not found")

// DB wraps the connection pool.
type DB struct {
	*sql.DB
}

// Open opens (or creates) the database at path. WAL keeps readers and the writer out of each
// other's way; synchronous=FULL makes every commit survive a power cut, which on a router is
// the usual way the process stops.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + escapePath(path) + "?" + strings.Join([]string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(FULL)",
		"_pragma=foreign_keys(1)",
		"_pragma=busy_timeout(5000)",
		"_txlock=immediate",
	}, "&")
	sqldb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(8)
	sqldb.SetMaxIdleConns(4)
	if err := sqldb.Ping(); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return &DB{sqldb}, nil
}

// escapePath keeps characters that mean something in a URI from being read as such.
func escapePath(p string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(p)
}

// migration is one numbered file in migrations/.
type migration struct {
	version int
	name    string
	sql     string
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		num, _, ok := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(num)
		if !ok || err != nil {
			return nil, fmt.Errorf("migration %s: name must start with a number and an underscore", e.Name())
		}
		body, err := fs.ReadFile(migrationFiles, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{v, e.Name(), string(body)})
	}
	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	for i, m := range out {
		if m.version != i+1 {
			return nil, fmt.Errorf("migration %s: expected number %d", m.name, i+1)
		}
	}
	return out, nil
}

// Migrate brings the schema up to date. Before changing a database that already has data,
// it saves a copy in backupDir, so a bad migration never costs anything.
func (d *DB) Migrate(ctx context.Context, backupDir string) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	var current int
	if err := d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	latest := len(migrations)
	if current > latest {
		return fmt.Errorf("the database is at version %d but this program only knows %d; use a newer program", current, latest)
	}
	if current == latest {
		return nil
	}
	if current > 0 {
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return err
		}
		name := fmt.Sprintf("share-v%d-%s.db", current, time.Now().UTC().Format("20060102-150405"))
		if _, err := d.ExecContext(ctx, "VACUUM INTO ?", filepath.Join(backupDir, name)); err != nil {
			return fmt.Errorf("backup before migrating: %w", err)
		}
	}
	for _, m := range migrations[current:] {
		tx, err := d.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, m.sql); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

// Tx runs fn in one transaction and commits it if fn returns nil.
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ms converts a time to the Unix milliseconds stored in the database.
func ms(t time.Time) int64 { return t.UnixMilli() }

// fromMS converts stored Unix milliseconds back to a UTC time.
func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

// nullMS stores an optional time; nil becomes NULL.
func nullMS(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UnixMilli()
}

// optTime reads an optional time.
func optTime(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := fromMS(v.Int64)
	return &t
}

// nullString stores an optional string; "" becomes NULL.
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
