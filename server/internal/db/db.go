// Package db is the persistence layer: opening, migrations and every query. Rules about who
// may do what live in the packages that call it. The queries are written in SQL that SQLite
// and PostgreSQL both accept; the few places where they differ ask the DB's dialect.
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

	"github.com/jackc/pgx/v5/pgconn"
	"modernc.org/sqlite" // registers the pure-Go "sqlite" driver
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFiles embed.FS

// ErrNotFound is returned when a looked-up row doesn't exist.
var ErrNotFound = errors.New("not found")

// DB wraps the connection pool.
type DB struct {
	*sql.DB
	dialect dialect
}

// dialect is what differs between the databases Share runs on: SQLite, or PostgreSQL.
type dialect struct{ postgres bool }

// noCase compares col with the next argument, ignoring the case of ASCII letters, as the
// indexes of such columns do.
func (dl dialect) noCase(col string) string {
	if dl.postgres {
		return "lower(" + col + ") = lower(?)"
	}
	return col + " = ? COLLATE NOCASE"
}

// orderNoCase sorts by col, ignoring case.
func (dl dialect) orderNoCase(col string) string {
	if dl.postgres {
		return "lower(" + col + ")"
	}
	return col + " COLLATE NOCASE"
}

// like is a LIKE that ignores case.
func (dl dialect) like() string {
	if dl.postgres {
		return "ILIKE"
	}
	return "LIKE"
}

// unindexed keeps SQLite's planner from using an index on col; PostgreSQL's needs no hint.
func (dl dialect) unindexed(col string) string {
	if dl.postgres {
		return col
	}
	return "+" + col
}

// migrations is the folder of this database's migrations.
func (dl dialect) migrations() string {
	if dl.postgres {
		return "migrations/postgres"
	}
	return "migrations/sqlite"
}

// txOptions makes PostgreSQL's transactions serializable, which gives the transactions that
// read and then write the guarantee SQLite's single writer gives.
func (dl dialect) txOptions() *sql.TxOptions {
	if dl.postgres {
		return &sql.TxOptions{Isolation: sql.LevelSerializable}
	}
	return nil
}

// retry reports whether a transaction failed only because another one was in the way, so
// that running it again may work: SQLite's busy and locked, PostgreSQL's serialization
// failure and deadlock.
func (dl dialect) retry(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "40001" || pgErr.Code == "40P01"
	}
	var e *sqlite.Error
	if !errors.As(err, &e) {
		return false
	}
	code := e.Code() & 0xff
	return code == 5 || code == 6 // SQLITE_BUSY, SQLITE_LOCKED
}

// hasMeta is a query that counts the tables named meta in the database.
func (dl dialect) hasMeta() string {
	if dl.postgres {
		return "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = 'meta'"
	}
	return "SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'meta'"
}

// Open opens (or creates) the database at path. WAL keeps readers and the writer out of each
// other's way; synchronous=FULL makes every commit survive a power cut, which on a small
// machine at home is the usual way the process stops.
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
	return &DB{DB: sqldb}, nil
}

// escapePath keeps characters that mean something in a URI from being read as such.
func escapePath(p string) string {
	return strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(p)
}

// migration is one numbered file in a dialect's migrations folder.
type migration struct {
	version int
	name    string
	sql     string
}

// loadMigrations reads a folder of migrations. Their numbers follow each other; the first
// one may be higher than 1 when it makes the whole schema of that version at once.
func loadMigrations(dir string) ([]migration, error) {
	entries, err := fs.ReadDir(migrationFiles, dir)
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
		body, err := fs.ReadFile(migrationFiles, dir+"/"+e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{v, e.Name(), string(body)})
	}
	slices.SortFunc(out, func(a, b migration) int { return a.version - b.version })
	for i, m := range out {
		if i > 0 && m.version != out[0].version+i {
			return nil, fmt.Errorf("migration %s: expected number %d", m.name, out[0].version+i)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no migrations in %s", dir)
	}
	return out, nil
}

// Migrate brings the schema up to date. Before changing a database that already has data,
// it saves a copy in backupDir, so a bad migration never costs anything.
func (d *DB) Migrate(ctx context.Context, backupDir string) error {
	migrations, err := loadMigrations(d.dialect.migrations())
	if err != nil {
		return err
	}
	current, err := d.SchemaVersion(ctx)
	if err != nil {
		return err
	}
	first, latest := migrations[0].version, migrations[len(migrations)-1].version
	if current > latest {
		return fmt.Errorf("the database is at version %d but this program only knows %d; use a newer program", current, latest)
	}
	if current == latest {
		return nil
	}
	if current > 0 && current < first-1 {
		return fmt.Errorf("the database is at version %d, too old for this program", current)
	}
	if current > 0 && !d.dialect.postgres { // PostgreSQL's host keeps its own backups
		if err := os.MkdirAll(backupDir, 0o700); err != nil {
			return err
		}
		name := fmt.Sprintf("share-v%d-%s.db", current, time.Now().UTC().Format("20060102-150405"))
		if _, err := d.ExecContext(ctx, "VACUUM INTO ?", filepath.Join(backupDir, name)); err != nil {
			return fmt.Errorf("backup before migrating: %w", err)
		}
	}
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
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
// one. It is kept in the meta table; databases from before that keep it in SQLite's
// user_version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var tables int
	if err := d.QueryRowContext(ctx, d.dialect.hasMeta()).Scan(&tables); err != nil {
		return 0, err
	}
	if tables == 0 {
		return 0, nil
	}
	v, err := d.Meta(ctx, "schema_version")
	if errors.Is(err, ErrNotFound) && !d.dialect.postgres {
		var old int
		err := d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&old)
		return old, err
	}
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
		if err == nil || attempt == 5 || !d.dialect.retry(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(time.Duration(attempt*attempt) * 10 * time.Millisecond):
		}
	}
}

func (d *DB) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, d.dialect.txOptions())
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
