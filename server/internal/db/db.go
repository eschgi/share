// Package db is the persistence layer: opening, migrations and every query, in PostgreSQL.
// Rules about who may do what live in the packages that call it. Text columns use the
// pg_c_utf8 collation: they sort by code point, as Go compares strings, and casefold() folds
// every letter. A lookup that ignores case compares casefold(col) with
// casefold(? COLLATE pg_c_utf8), since a bare parameter takes the database's own locale.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ErrNotFound is returned when a looked-up row doesn't exist.
var ErrNotFound = errors.New("not found")

// DB wraps the connection pool.
type DB struct {
	*sql.DB
}

// Open opens a PostgreSQL database, e.g. postgres://share:…@host/share, and checks that it can
// hold Share's schema. Errors name the host and the database, never the password.
func Open(ctx context.Context, url string) (*DB, error) {
	cfg, err := pgx.ParseConfig(url)
	if err != nil {
		return nil, errors.New("the PostgreSQL address can't be read") // its text may hold the password
	}
	where := cfg.Host + "/" + cfg.Database
	sqldb := sql.OpenDB(pgConnector{stdlib.GetConnector(*cfg)})
	sqldb.SetMaxOpenConns(8)
	sqldb.SetMaxIdleConns(4)
	// A database host may close idle connections, Neon when it pauses after 5 minutes.
	sqldb.SetConnMaxIdleTime(4 * time.Minute)
	var version int
	var encoding string
	err = sqldb.QueryRowContext(ctx, "SELECT current_setting('server_version_num')::int, current_setting('server_encoding')").Scan(&version, &encoding)
	if err == nil {
		err = usable(version, encoding)
	}
	if err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("PostgreSQL at %s: %w", where, err)
	}
	return &DB{DB: sqldb}, nil
}

// unusable is a server that answers but can't hold Share's schema.
type unusable string

func (u unusable) Error() string { return string(u) }

// usable says why a server can't hold Share's schema, whose text columns need pg_c_utf8 and
// casefold().
func usable(version int, encoding string) error {
	if version < 180000 {
		return unusable(fmt.Sprintf("it is PostgreSQL %d.%d; Share needs 18 or newer", version/10000, version%10000))
	}
	if encoding != "UTF8" {
		return unusable(fmt.Sprintf("the database's encoding is %s; Share needs UTF8", encoding))
	}
	return nil
}

// Unreachable reports whether err means the database didn't answer at all, rather than
// refusing the login or the database, or being one Share can't use: waiting may help with the
// first, not with the others.
func Unreachable(err error) bool {
	var pgErr *pgconn.PgError
	var u unusable
	return err != nil && !errors.As(err, &pgErr) && !errors.As(err, &u) && !errors.Is(err, context.Canceled)
}

// migration is one numbered file in the migrations folder.
type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads the migrations, numbered from 1 on.
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
	if len(out) == 0 {
		return nil, errors.New("no migrations")
	}
	return out, nil
}

// Migrate brings the schema up to date, each migration in a transaction of its own. The
// database's host keeps the backups.
func (d *DB) Migrate(ctx context.Context) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}
	current, err := d.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	if latest := len(migrations); current > latest {
		return fmt.Errorf("the database is at version %d, but this Share knows only up to %d", current, latest)
	}
	for _, m := range migrations[current:] {
		err := d.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.sql); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO meta (key, value) VALUES ('schema_version', ?)
				ON CONFLICT (key) DO UPDATE SET value = excluded.value`, strconv.Itoa(m.version))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.name, err)
		}
	}
	return nil
}

// SchemaVersion is the number of the last migration the database went through, 0 for a new
// one. It is kept in the meta table.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var tables int
	if err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = 'meta'`).Scan(&tables); err != nil {
		return 0, err
	}
	if tables == 0 {
		return 0, nil
	}
	v, err := d.Meta(ctx, "schema_version")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(v)
}

// Tx runs fn in one transaction and commits it if fn returns nil. When another transaction
// was in the way, fn runs again, a few times, so it must change nothing outside the
// transaction.
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	for attempt := 1; ; attempt++ {
		err := d.tx(ctx, fn)
		if err == nil || attempt == 5 || !retryable(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt*attempt) * 10 * time.Millisecond):
		}
	}
}

// tx runs fn once, in a serializable transaction: the transactions that read and then write
// get the guarantee a single writer would give, and Tx runs them again when they collide.
func (d *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// retryable reports whether a transaction failed only because another one was in the way, so
// that running it again may work: a serialization failure or a deadlock.
func retryable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && (pgErr.Code == "40001" || pgErr.Code == "40P01")
}

// isUniqueViolation reports whether err comes from a UNIQUE constraint.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
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
