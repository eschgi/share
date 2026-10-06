package config

import (
	"net/netip"
	"net/url"
	"strings"

	"github.com/eschgi/share/server/internal/homenet"
)

// Database is where Share keeps its records (the files' names, folders and days, the people,
// the PINs): a PostgreSQL server, next to Share in Docker, or e.g. a free one at Neon.
type Database struct {
	// Postgres is the server's address, e.g. postgres://share:…@host/share?sslmode=require.
	Postgres string `json:"postgres"`
}

// String names the database without its password, so it never ends up in a log.
func (d Database) String() string {
	u, err := url.Parse(d.Postgres)
	if err != nil {
		return "PostgreSQL"
	}
	return "PostgreSQL at " + u.Host + u.Path
}

// GoString is String for %#v.
func (d Database) GoString() string { return d.String() }

// complete checks the address. The messages never repeat it: it holds the password.
func (d *Database) complete(bad func(field, format string, args ...any)) {
	const like = "an address like postgres://share:PASSWORD@HOST/share?sslmode=require"
	if d.Postgres == "" {
		bad("database.postgres", "is required: Share keeps its records in PostgreSQL, at %s", like)
		return
	}
	u, err := url.Parse(d.Postgres)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Hostname() == "" {
		bad("database.postgres", "must be %s", like)
		return
	}
	if strings.Trim(u.Path, "/") == "" {
		bad("database.postgres", "names no database: put it after the host, as in %s", like)
	}
	switch u.Query().Get("sslmode") {
	case "require", "verify-ca", "verify-full":
	default:
		if !nearby(u.Hostname()) {
			bad("database.postgres", "needs sslmode=require (or verify-full) for a database outside the home network, since its traffic crosses the internet")
		}
	}
}

// nearby reports whether host is on a home network or this machine, or a name without a dot: a
// service next to Share, such as the database of the Docker setups ("db"), which public DNS
// can't answer.
func nearby(host string) bool {
	if homenet.Host(host) {
		return true
	}
	_, err := netip.ParseAddr(host)
	return err != nil && !strings.Contains(host, ".")
}
