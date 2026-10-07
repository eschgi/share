package db

import (
	"context"
	"database/sql"
)

// CRCCandidates lists plain files in the library whose CRC-32 isn't known yet, the newest
// first: those are the likeliest to be downloaded soon. Encrypted files go into no ZIP.
func (d *DB) CRCCandidates(ctx context.Context, limit int) ([]File, error) {
	return queryAll(ctx, d.pool, scanFile, "SELECT "+fileColumns+` FROM files WHERE state = 'ready' AND crc32 IS NULL AND enc_version IS NULL
		ORDER BY uploaded_at DESC, id LIMIT $1`, limit)
}

// FileCRC32 returns a file's CRC-32, and whether it is known yet.
func (d *DB) FileCRC32(ctx context.Context, id string) (uint32, bool, error) {
	var v sql.NullInt64
	err := d.pool.QueryRowContext(ctx, "SELECT crc32 FROM files WHERE id = $1", id).Scan(&v)
	if noRow(err) {
		return 0, false, ErrNotFound
	}
	return uint32(v.Int64), v.Valid, err
}

// SetCRC32 records a file's CRC-32. A file's bytes never change, so one that is known stays.
// It doesn't count as a change of the file: thumbnails and the library's version stay.
func (d *DB) SetCRC32(ctx context.Context, id string, crc uint32) error {
	_, err := d.pool.ExecContext(ctx, "UPDATE files SET crc32 = $1 WHERE id = $2 AND crc32 IS NULL", int64(crc), id)
	return err
}

// ReadyFiles returns the files among ids that are in the library, once each, in the order of
// ids.
func (d *DB) ReadyFiles(ctx context.Context, ids []string) ([]File, error) {
	files, err := queryAll(ctx, d.pool, scanFile, "SELECT "+fileColumns+" FROM files WHERE state = 'ready' AND id = ANY($1)", ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]File, len(files))
	for _, f := range files {
		byID[f.ID] = f
	}
	out := make([]File, 0, len(byID))
	for _, id := range ids {
		if f, ok := byID[id]; ok {
			out = append(out, f)
			delete(byID, id)
		}
	}
	return out, nil
}
