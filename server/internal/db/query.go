package db

import (
	"context"
	"database/sql"
)

// querier runs a query: the pool, or a transaction.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// scanner is a row to read: one that QueryRowContext found, or each row of a query in turn.
type scanner = interface{ Scan(dest ...any) error }

// queryAll runs query and reads every row it finds with scan.
func queryAll[T any](ctx context.Context, q querier, scan func(scanner) (T, error), query string, args ...any) ([]T, error) {
	var out []T
	err := eachRow(ctx, q, query, args, func(row scanner) error {
		v, err := scan(row)
		if err != nil {
			return err
		}
		out = append(out, v)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// eachRow runs query and hands each row it finds to row; the only loop over rows in the
// package.
func eachRow(ctx context.Context, q querier, query string, args []any, row func(scanner) error) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := row(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
