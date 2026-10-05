package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/stdlib"
)

// OpenPostgres opens a PostgreSQL database, e.g. postgres://share:…@host/share. Errors name the
// host and the database, never the password.
func OpenPostgres(ctx context.Context, url string) (*DB, error) {
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
	if err := sqldb.PingContext(ctx); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("PostgreSQL at %s: %w", where, err)
	}
	return &DB{DB: sqldb, dialect: dialect{postgres: true}}, nil
}

// Unreachable reports whether err means the database didn't answer at all, rather than
// refusing the login or the database: waiting may help with the first, not with the second.
func Unreachable(err error) bool {
	var pgErr *pgconn.PgError
	return err != nil && !errors.As(err, &pgErr) && !errors.Is(err, context.Canceled)
}

// pgConnector hands out connections that understand SQLite's placeholders, so that every query
// is written once.
type pgConnector struct{ driver.Connector }

func (c pgConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return pgConn{conn.(*stdlib.Conn)}, nil
}

// pgConn passes everything on to pgx's connection, with the placeholders rewritten.
type pgConn struct{ c *stdlib.Conn }

func (p pgConn) Prepare(q string) (driver.Stmt, error) { return p.c.Prepare(rebind(q)) }
func (p pgConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return p.c.PrepareContext(ctx, rebind(q))
}
func (p pgConn) Close() error              { return p.c.Close() }
func (p pgConn) Begin() (driver.Tx, error) { return p.c.Begin() }
func (p pgConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return p.c.BeginTx(ctx, opts)
}
func (p pgConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return p.c.ExecContext(ctx, rebind(q), args)
}
func (p pgConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return p.c.QueryContext(ctx, rebind(q), args)
}
func (p pgConn) Ping(ctx context.Context) error             { return p.c.Ping(ctx) }
func (p pgConn) CheckNamedValue(v *driver.NamedValue) error { return p.c.CheckNamedValue(v) }
func (p pgConn) ResetSession(ctx context.Context) error     { return p.c.ResetSession(ctx) }

// rebind turns SQLite's placeholders, ? and ?NNN, into PostgreSQL's $1, $2…, outside quoted text
// and comments. A bare ? is one more than the highest number before it, as in SQLite.
func rebind(q string) string {
	if !strings.Contains(q, "?") {
		return q
	}
	var b strings.Builder
	b.Grow(len(q) + 16)
	largest := 0
	for i := 0; i < len(q); i++ {
		c := q[i]
		switch {
		case c == '\'' || c == '"':
			j := i + 1
			for j < len(q) && (q[j] != c || j+1 < len(q) && q[j+1] == c) {
				if q[j] == c {
					j++ // a doubled quote stays inside
				}
				j++
			}
			j = min(j, len(q)-1)
			b.WriteString(q[i : j+1])
			i = j
		case c == '-' && i+1 < len(q) && q[i+1] == '-':
			end := strings.IndexByte(q[i:], '\n')
			if end < 0 {
				end = len(q) - i - 1
			}
			b.WriteString(q[i : i+end+1])
			i += end
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			end := len(q) - 1
			if j := strings.Index(q[i+2:], "*/"); j >= 0 {
				end = i + 2 + j + 1
			}
			b.WriteString(q[i : end+1])
			i = end
		case c == '?':
			j := i + 1
			for j < len(q) && q[j] >= '0' && q[j] <= '9' {
				j++
			}
			n := largest + 1
			if j > i+1 {
				n, _ = strconv.Atoi(q[i+1 : j])
			}
			largest = max(largest, n)
			b.WriteString("$" + strconv.Itoa(n))
			i = j - 1
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
