package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// File states.
const (
	StateReceiving  = "receiving"
	StateFinalizing = "finalizing"
	StateReady      = "ready"
	StateTrashed    = "trashed"
)

// File kinds, used for the photos/videos/documents filters.
const (
	KindPhoto    = "photo"
	KindVideo    = "video"
	KindDocument = "document"
)

// File is one upload, from the first byte until it is in the library (and later the trash).
type File struct {
	ID               string
	State            string
	Name             string
	Size             int64
	Received         int64
	Mime             string
	Kind             string
	FolderID         string // the folder it lies in
	RelPath          string // path inside its folder's directory, e.g. 2026-09-27/IMG_1.jpg
	UploadDay        string // YYYY-MM-DD in the configured time zone
	CreatedAt        time.Time
	UpdatedAt        time.Time
	UploadedAt       *time.Time
	ClientModifiedAt *time.Time
	Width, Height    *int64
	DurationMS       *int64
	Thumb            string
	PinID            string
	PinSessionID     string
	UserID           string
	DeviceID         string
	DeletedAt        *time.Time
	DeletedBy        string
	CRC32            *uint32 // known once the checksum worker or a download worked it out
	MovedFrom        string  // a move to another folder in progress: "<folder id>/<rel_path>" of the bytes
	S3UploadID       string  // in a bucket: the multipart upload of a receiving file
	S3PartSize       int64   // in a bucket: the size of every part but the last
	Enc              *Enc    // how it is encrypted; nil for a plain file
}

// Enc is how an encrypted file is encrypted (docs/e2ee-plan.md). Its Size is that of the stored
// bytes; the devices show PlainSize.
type Enc struct {
	Version   int    // the version of its folder's key that the file key is sealed for
	Key       []byte // the file key, sealed for that version
	Header    []byte // the header the stored bytes start with
	PlainSize int64
}

const fileColumns = `id, state, name, size, received, mime, kind, rel_path, upload_day, created_at, updated_at,
	uploaded_at, client_modified_at, width, height, duration_ms, thumb, pin_id, pin_session_id, user_id,
	device_id, deleted_at, deleted_by, crc32, folder_id, moved_from, s3_upload_id, s3_part_size, enc_version,
	enc_key, enc_header, plain_size`

func scanFile(row interface{ Scan(...any) error }) (File, error) {
	var f File
	var relPath, pinID, sessionID, userID, deviceID, deletedBy, folderID, movedFrom, s3Upload sql.NullString
	var day sql.NullTime
	var width, height, duration, crc, s3PartSize, encVersion, plainSize sql.NullInt64
	var encKey, encHeader []byte
	err := row.Scan(&f.ID, &f.State, &f.Name, &f.Size, &f.Received, &f.Mime, &f.Kind, &relPath, &day,
		&f.CreatedAt, &f.UpdatedAt, &f.UploadedAt, &f.ClientModifiedAt, &width, &height, &duration, &f.Thumb,
		&pinID, &sessionID, &userID, &deviceID, &f.DeletedAt, &deletedBy, &crc, &folderID, &movedFrom, &s3Upload, &s3PartSize,
		&encVersion, &encKey, &encHeader, &plainSize)
	if noRow(err) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	f.FolderID, f.RelPath, f.MovedFrom = folderID.String, relPath.String, movedFrom.String
	if day.Valid {
		f.UploadDay = day.Time.Format(time.DateOnly)
	}
	f.S3UploadID, f.S3PartSize = s3Upload.String, s3PartSize.Int64
	f.Width, f.Height, f.DurationMS = optInt(width), optInt(height), optInt(duration)
	f.PinID, f.PinSessionID, f.UserID, f.DeviceID, f.DeletedBy =
		pinID.String, sessionID.String, userID.String, deviceID.String, deletedBy.String
	if crc.Valid {
		v := uint32(crc.Int64)
		f.CRC32 = &v
	}
	if encVersion.Valid {
		f.Enc = &Enc{Version: int(encVersion.Int64), Key: encKey, Header: encHeader, PlainSize: plainSize.Int64}
	}
	return f, nil
}

func optInt(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

func queryFiles(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) ([]File, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []File
	for rows.Next() {
		f, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// InsertReceiving records a new upload: before tus creates its files, or once the bucket has
// started its multipart upload. The kind is only a guess from the name until finalize looks
// at the content. Every file needs its folder.
func (d *DB) InsertReceiving(ctx context.Context, f File) error {
	if f.FolderID == "" {
		return errors.New("db: a file needs a folder")
	}
	if f.Kind == "" {
		f.Kind = KindDocument
	}
	var partSize, encVersion, plainSize sql.NullInt64
	if f.S3PartSize > 0 {
		partSize = sql.NullInt64{Int64: f.S3PartSize, Valid: true}
	}
	var encKey, encHeader []byte
	if e := f.Enc; e != nil {
		encVersion, plainSize = sql.NullInt64{Int64: int64(e.Version), Valid: true}, sql.NullInt64{Int64: e.PlainSize, Valid: true}
		encKey, encHeader = e.Key, e.Header
	}
	_, err := d.ExecContext(ctx, `INSERT INTO files (id, state, name, size, received, mime, kind, created_at, updated_at,
			client_modified_at, pin_id, pin_session_id, user_id, device_id, folder_id, s3_upload_id, s3_part_size,
			enc_version, enc_key, enc_header, plain_size)
		VALUES (?, 'receiving', ?, ?, 0, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.Name, f.Size, f.Mime, f.Kind, f.CreatedAt, f.UpdatedAt, f.ClientModifiedAt,
		nullString(f.PinID), nullString(f.PinSessionID), nullString(f.UserID), nullString(f.DeviceID), f.FolderID,
		nullString(f.S3UploadID), partSize, encVersion, encKey, encHeader, plainSize)
	return err
}

// FileByID returns one file row.
func (d *DB) FileByID(ctx context.Context, id string) (File, error) {
	return scanFile(d.QueryRowContext(ctx, "SELECT "+fileColumns+" FROM files WHERE id = ?", id))
}

// SetReceived records how many bytes of a receiving upload have arrived.
func (d *DB) SetReceived(ctx context.Context, id string, received int64, at time.Time) error {
	_, err := d.ExecContext(ctx,
		"UPDATE files SET received = ?, updated_at = ? WHERE id = ? AND state = 'receiving'", received, at, id)
	return err
}

// UnfinishedCount counts the receiving uploads of one PIN session or one user.
func (d *DB) UnfinishedCount(ctx context.Context, pinSessionID, userID string) (int, error) {
	var n int
	err := d.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE state = 'receiving'
		AND (pin_session_id = ? OR user_id = ?)`, nullString(pinSessionID), nullString(userID)).Scan(&n)
	return n, err
}

// OutstandingBytes is how much disk space unfinished uploads will still need.
func (d *DB) OutstandingBytes(ctx context.Context) (int64, error) {
	var n sql.NullInt64
	err := d.QueryRowContext(ctx, "SELECT SUM(size - received) FROM files WHERE state = 'receiving'").Scan(&n)
	return n.Int64, err
}

// relPathTaken counts the files on a path on files_rel_path, whose WHERE it repeats.
const relPathTaken = `SELECT COUNT(*) FROM files
	WHERE folder_id = ? AND casefold(rel_path) = casefold(? COLLATE pg_c_utf8) AND state IN ('finalizing', 'ready')`

// RelPathTaken reports whether a path in a folder is in use, ignoring case, because exFAT
// and NTFS drives treat IMG.jpg and img.jpg as the same file.
func (d *DB) RelPathTaken(ctx context.Context, folderID, relPath string) (bool, error) {
	var n int
	err := d.QueryRowContext(ctx, relPathTaken, folderID, relPath).Scan(&n)
	return n > 0, err
}

// MarkFinalizing claims a path in its folder for a complete upload. It returns false if the
// row isn't receiving any more, and ErrConflict if the path was just taken by another upload.
func (d *DB) MarkFinalizing(ctx context.Context, id, relPath, day string, at time.Time) (bool, error) {
	res, err := d.ExecContext(ctx, `UPDATE files SET state = 'finalizing', rel_path = ?, upload_day = ?, updated_at = ?
		WHERE id = ? AND state = 'receiving'`, relPath, day, at, id)
	if isUniqueViolation(err) {
		return false, ErrConflict
	}
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// MarkReady puts a finalized file into the library and bumps the library version.
func (d *DB) MarkReady(ctx context.Context, id, mime, kind string, at time.Time) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE files SET state = 'ready', mime = ?, kind = ?, received = size,
				uploaded_at = ?, updated_at = ?
			WHERE id = ? AND state = 'finalizing'`, mime, kind, at, at, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // already ready: finalize is idempotent
		}
		return bumpLibraryVersion(ctx, tx)
	})
}

// DeleteFileRow removes a row. Used for uploads that are terminated or abandoned before
// they reach the library.
func (d *DB) DeleteFileRow(ctx context.Context, id string) error {
	_, err := d.ExecContext(ctx, "DELETE FROM files WHERE id = ? AND state IN ('receiving', 'finalizing')", id)
	return err
}

// FilesInStates lists the files in any of the given states, oldest change first.
func (d *DB) FilesInStates(ctx context.Context, states ...string) ([]File, error) {
	if len(states) == 0 {
		return nil, nil
	}
	args := make([]any, len(states))
	for i, s := range states {
		args[i] = s
	}
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE state IN (?"+strings.Repeat(", ?", len(states)-1)+
		") ORDER BY updated_at, id", args...)
}

// IdleReceiving lists receiving uploads that haven't changed since before.
func (d *DB) IdleReceiving(ctx context.Context, before time.Time) ([]File, error) {
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE state = 'receiving' AND updated_at < ? ORDER BY updated_at",
		before)
}

// Move is one file's move to another folder: where it goes, and where its bytes come from
// ("<folder id>/<rel_path>").
type Move struct {
	ID       string
	FolderID string
	RelPath  string
	From     string
	Enc      *Enc // an encrypted file's key, sealed for the new folder's key; Header and PlainSize stay
}

// MoveFiles records moves to other folders in one transaction, before the bytes follow; each
// file notes where its bytes come from until FinishMoves. Files that aren't in the library
// any more are left alone. updated_at stays: the file and its thumbnail didn't change.
func (d *DB) MoveFiles(ctx context.Context, moves []Move) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		changed := false
		for _, m := range moves {
			q, args := "UPDATE files SET folder_id = ?, rel_path = ?, moved_from = ?", []any{m.FolderID, m.RelPath, m.From}
			if m.Enc != nil {
				q, args = q+", enc_version = ?, enc_key = ?", append(args, m.Enc.Version, m.Enc.Key)
			}
			res, err := tx.ExecContext(ctx, q+" WHERE id = ? AND state = 'ready'", append(args, m.ID)...)
			if isUniqueViolation(err) {
				return ErrConflict
			}
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n > 0 {
				changed = true
			}
		}
		if !changed {
			return nil
		}
		return bumpLibraryVersion(ctx, tx)
	})
}

// FinishMoves notes that the bytes of moved files are in their new place.
func (d *DB) FinishMoves(ctx context.Context, ids []string) error {
	for start := 0; start < len(ids); start += 500 {
		part := ids[start:min(start+500, len(ids))]
		args := make([]any, len(part))
		for i, id := range part {
			args[i] = id
		}
		if _, err := d.ExecContext(ctx, "UPDATE files SET moved_from = NULL WHERE id IN (?"+strings.Repeat(", ?", len(part)-1)+")", args...); err != nil {
			return err
		}
	}
	return nil
}

// Moving lists the files whose bytes may still be at their place before a move.
func (d *DB) Moving(ctx context.Context) ([]File, error) {
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE moved_from IS NOT NULL")
}
