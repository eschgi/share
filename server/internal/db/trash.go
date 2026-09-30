package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// TrashFiles takes ready files out of the library: from now on only Recently deleted shows
// them. It returns the files it trashed; ids that aren't ready are left alone. The bytes are
// moved to the trash folder afterwards (storage.Library.Trash).
func (d *DB) TrashFiles(ctx context.Context, ids []string, by string, at time.Time) ([]File, error) {
	var out []File
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		out = nil
		for _, id := range ids {
			f, err := scanFile(tx.QueryRowContext(ctx, "SELECT "+fileColumns+" FROM files WHERE id = ? AND state = 'ready'", id))
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE files SET state = 'trashed', deleted_at = ?, deleted_by = ?, updated_at = ?
				WHERE id = ?`, ms(at), nullString(by), ms(at), id); err != nil {
				return err
			}
			f.State, f.DeletedAt, f.DeletedBy, f.UpdatedAt = StateTrashed, &at, by, at
			out = append(out, f)
		}
		if len(out) == 0 {
			return nil
		}
		return bumpLibraryVersion(ctx, tx)
	})
	return out, err
}

// Trashed lists the files in the trash, the most recently deleted first.
func (d *DB) Trashed(ctx context.Context) ([]File, error) {
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE state = 'trashed' ORDER BY deleted_at DESC, id")
}

// TrashedBefore lists the files deleted before t, which the daily purge removes for good.
func (d *DB) TrashedBefore(ctx context.Context, t time.Time) ([]File, error) {
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE state = 'trashed' AND deleted_at < ? ORDER BY deleted_at",
		ms(t))
}

// TrashedByID returns the trashed files among ids.
func (d *DB) TrashedByID(ctx context.Context, ids []string) ([]File, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE state = 'trashed' AND id IN (?"+
		strings.Repeat(", ?", len(ids)-1)+") ORDER BY deleted_at DESC, id", args...)
}

// TrashStats counts the trash and its bytes.
func (d *DB) TrashStats(ctx context.Context) (files int, bytes int64, err error) {
	var n sql.NullInt64
	err = d.QueryRowContext(ctx, "SELECT COUNT(*), SUM(size) FROM files WHERE state = 'trashed'").Scan(&files, &n)
	return files, n.Int64, err
}

// RestoreFile puts a trashed file back into the library, at relPath (its old path, or a
// numbered one if that was taken meanwhile). It returns false if the file isn't in the trash,
// and ErrConflict if relPath was just taken.
func (d *DB) RestoreFile(ctx context.Context, id, relPath string, at time.Time) (bool, error) {
	var restored bool
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE files SET state = 'ready', rel_path = ?, deleted_at = NULL, deleted_by = NULL,
				updated_at = ?
			WHERE id = ? AND state = 'trashed'`, relPath, ms(at), id)
		if isUniqueViolation(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		restored = true
		return bumpLibraryVersion(ctx, tx)
	})
	return restored, err
}

// PurgeFile forgets a trashed file for good. It returns false if it wasn't in the trash.
func (d *DB) PurgeFile(ctx context.Context, id string) (bool, error) {
	res, err := d.ExecContext(ctx, "DELETE FROM files WHERE id = ? AND state = 'trashed'", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// LibraryStats counts the files in the library and their bytes.
func (d *DB) LibraryStats(ctx context.Context) (files int, bytes int64, err error) {
	var n sql.NullInt64
	err = d.QueryRowContext(ctx, "SELECT COUNT(*), SUM(size) FROM files WHERE state = 'ready'").Scan(&files, &n)
	return files, n.Int64, err
}
