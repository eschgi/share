package db

import (
	"context"
	"database/sql"
	"time"
)

// Thumbnail sources, in the files.thumb column.
const (
	ThumbNone   = "none"
	ThumbClient = "client" // sent by the browser or phone that uploaded the file
	ThumbServer = "server" // made by the server's thumbnail worker
	// ThumbFailed: the worker couldn't make one. It also marks the photo the worker is busy
	// with, so a photo that takes the process down while decoding isn't tried again.
	ThumbFailed = "failed"
)

// ThumbCandidates lists plain photos in the library without a thumbnail that arrived before
// before, oldest first: the server can't read encrypted ones.
func (d *DB) ThumbCandidates(ctx context.Context, before time.Time, limit int) ([]File, error) {
	return queryFiles(ctx, d, "SELECT "+fileColumns+` FROM files
		WHERE state = 'ready' AND thumb = 'none' AND kind = 'photo' AND enc_version IS NULL AND uploaded_at <= ?
		ORDER BY uploaded_at, id LIMIT ?`, ms(before), limit)
}

// SetClientThumb records a thumbnail from the uploader, which replaces any other, together
// with the dimensions the uploader measured. It returns false if the file isn't in the
// library.
func (d *DB) SetClientThumb(ctx context.Context, id string, width, height, durationMS *int64, at time.Time) (bool, error) {
	return d.setThumb(ctx, `UPDATE files SET thumb = 'client', width = COALESCE(?, width), height = COALESCE(?, height),
			duration_ms = COALESCE(?, duration_ms), updated_at = ?
		WHERE id = ? AND state = 'ready'`,
		nullInt(width), nullInt(height), nullInt(durationMS), ms(at), id)
}

// ClaimThumb marks a photo as tried before the worker decodes it (see ThumbFailed). It
// returns false if the photo has a thumbnail by now.
func (d *DB) ClaimThumb(ctx context.Context, id string, at time.Time) (bool, error) {
	res, err := d.ExecContext(ctx, "UPDATE files SET thumb = 'failed', updated_at = ? WHERE id = ? AND thumb = 'none'",
		ms(at), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReleaseThumb hands a claimed photo back, to be tried again later.
func (d *DB) ReleaseThumb(ctx context.Context, id string, at time.Time) error {
	_, err := d.ExecContext(ctx, "UPDATE files SET thumb = 'none', updated_at = ? WHERE id = ? AND thumb = 'failed'",
		ms(at), id)
	return err
}

// SetServerThumb records a thumbnail the worker made of a photo it claimed, unless the
// uploader's arrived in the meantime. Dimensions are only filled in where the uploader didn't
// send any.
func (d *DB) SetServerThumb(ctx context.Context, id string, width, height int64, at time.Time) (bool, error) {
	return d.setThumb(ctx, `UPDATE files SET thumb = 'server', width = COALESCE(width, ?), height = COALESCE(height, ?),
			updated_at = ?
		WHERE id = ? AND state = 'ready' AND thumb = 'failed'`,
		width, height, ms(at), id)
}

// setThumb runs a thumbnail update and bumps the library version if it changed a row, so
// the app knows to fetch the new picture.
func (d *DB) setThumb(ctx context.Context, query string, args ...any) (bool, error) {
	changed := false
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		changed = true
		return bumpLibraryVersion(ctx, tx)
	})
	return changed, err
}

func nullInt(v *int64) any {
	if v == nil {
		return nil
	}
	return *v
}
