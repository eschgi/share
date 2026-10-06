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
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM files WHERE id = $1 AND state = 'trashed'", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		purged = true
		for _, key := range keys {
			if _, err := tx.ExecContext(ctx, `INSERT INTO s3_garbage (key, created_at) VALUES ($1, $2)
				ON CONFLICT (key) DO UPDATE SET created_at = excluded.created_at`, key, at); err != nil {
				return err
			}
		}
		return nil
	})
	return purged, err
}

// S3Garbage returns up to limit objects still to be removed, the oldest first.
func (d *DB) S3Garbage(ctx context.Context, limit int) ([]string, error) {
	return queryAll(ctx, d.pool, func(row scanner) (string, error) {
		var key string
		err := row.Scan(&key)
		return key, err
	}, "SELECT key FROM s3_garbage ORDER BY created_at, key LIMIT $1", limit)
}

// ForgetS3Garbage notes that an object is removed.
func (d *DB) ForgetS3Garbage(ctx context.Context, key string) error {
	_, err := d.pool.ExecContext(ctx, "DELETE FROM s3_garbage WHERE key = $1", key)
	return err
}

// DropS3Garbage forgets every object still to be removed: they belong to a bucket this
// database doesn't use any more.
func (d *DB) DropS3Garbage(ctx context.Context) error {
	_, err := d.pool.ExecContext(ctx, "DELETE FROM s3_garbage")
	return err
}

// HasFiles reports whether there is any file at all: arriving, in the library or in the trash.
func (d *DB) HasFiles(ctx context.Context) (bool, error) {
	var found bool
	err := d.pool.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM files)").Scan(&found)
	return found, err
}
