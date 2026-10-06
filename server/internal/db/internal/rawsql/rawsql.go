// Package rawsql hands the pool of a *db.DB to the test helpers in internal/db/dbtest. Go lets
// only internal/db and the packages below it import it, so the rest of the server runs no SQL.
package rawsql

import "database/sql"

// Pool returns the pool of a *db.DB; package db sets it.
var Pool func(d any) *sql.DB
