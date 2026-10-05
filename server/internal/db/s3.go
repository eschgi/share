package db

import (
	"context"
	"database/sql"
	"time"
)

// PurgeToS3Garbage removes a trashed file's row and notes its objects (the file, its
// thumbnail) for removal, in one transaction: an object is never forgotten, even if the bucket
// can't be reached right now. It returns false if the file isn't in the trash.
func (d *DB) PurgeToS3Garbage(ctx context.Context, id string, at time.Time, keys ...string) (bool, error) {
	var purged bool
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM files WHERE id = ? AND state = 'trashed'", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		purged = true
		for _, key := range keys {
			if _, err := tx.ExecContext(ctx, `INSERT INTO s3_garbage (key, created_at) VALUES (?, ?)
				ON CONFLICT (key) DO UPDATE SET created_at = excluded.created_at`, key, ms(at)); err != nil {
				return err
			}
		}
		return nil
	})
	return purged, err
}

// S3Garbage returns up to limit objects still to be removed, the oldest first.
func (d *DB) S3Garbage(ctx context.Context, limit int) ([]string, error) {
	rows, err := d.QueryContext(ctx, "SELECT key FROM s3_garbage ORDER BY created_at, key LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// ForgetS3Garbage notes that an object is removed.
func (d *DB) ForgetS3Garbage(ctx context.Context, key string) error {
	_, err := d.ExecContext(ctx, "DELETE FROM s3_garbage WHERE key = ?", key)
	return err
}

// DropS3Garbage forgets every object still to be removed: they belong to a bucket this
// database doesn't use any more.
func (d *DB) DropS3Garbage(ctx context.Context) error {
	_, err := d.ExecContext(ctx, "DELETE FROM s3_garbage")
	return err
}

// HasFiles reports whether there is any file at all: arriving, in the library or in the trash.
func (d *DB) HasFiles(ctx context.Context) (bool, error) {
	var found bool
	err := d.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM files)").Scan(&found)
	return found, err
}
