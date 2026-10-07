package db

import (
	"context"
	"database/sql"
	"time"
)

// PinSession is what unlocking with a PIN creates: a website cookie or an app token.
type PinSession struct {
	ID         string
	PinID      string
	Client     string // "web" or "app"
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
	Pin        Pin
}

// InsertPinSession stores a new session; only the token's hash is kept.
func (d *DB) InsertPinSession(ctx context.Context, s PinSession, tokenHash []byte) error {
	_, err := d.pool.ExecContext(ctx,
		"INSERT INTO pin_sessions (id, pin_id, token_hash, client, created_at, last_seen_at) VALUES ($1, $2, $3, $4, $5, $6)",
		s.ID, s.PinID, tokenHash, s.Client, s.CreatedAt, s.LastSeenAt)
	return err
}

// PinSessionByToken finds the session for a token hash, together with its PIN. It doesn't
// judge validity; callers check RevokedAt and Pin.LiveAt.
func (d *DB) PinSessionByToken(ctx context.Context, tokenHash []byte) (PinSession, error) {
	var s PinSession
	row := d.pool.QueryRowContext(ctx, `SELECT s.id, s.pin_id, s.client, s.created_at, s.last_seen_at, s.revoked_at,
			p.id, p.code, p.kind, p.created_by, p.created_at, p.expires_at, p.ended_at, p.folder_id, p.shows_folder
		FROM pin_sessions s JOIN pins p ON p.id = s.pin_id WHERE s.token_hash = $1`, tokenHash)
	var pFolder sql.NullString
	err := row.Scan(&s.ID, &s.PinID, &s.Client, &s.CreatedAt, &s.LastSeenAt, &s.RevokedAt,
		&s.Pin.ID, &s.Pin.Code, &s.Pin.Kind, &s.Pin.CreatedBy, &s.Pin.CreatedAt, &s.Pin.ExpiresAt, &s.Pin.EndedAt, &pFolder, &s.Pin.ShowsFolder)
	if noRow(err) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.Pin.FolderID = pFolder.String
	return s, nil
}

// TouchPinSession records that the session was used.
func (d *DB) TouchPinSession(ctx context.Context, id string, at time.Time) error {
	_, err := d.pool.ExecContext(ctx, "UPDATE pin_sessions SET last_seen_at = $1 WHERE id = $2", at, id)
	return err
}

// RevokePinSession ends one session.
func (d *DB) RevokePinSession(ctx context.Context, id string, at time.Time) error {
	_, err := d.pool.ExecContext(ctx, "UPDATE pin_sessions SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL", at, id)
	return err
}

// MoveReceivingUploads hands the unfinished uploads of an old session to a new one. That
// happens when a PIN ended mid-upload and the person unlocked again with a new PIN: the
// uploads continue instead of starting over, into the new PIN's folder. An encrypted upload
// continues only into the same folder, whose key its file key is sealed for.
func (d *DB) MoveReceivingUploads(ctx context.Context, fromSession, toSession, toPin, toFolder string, at time.Time) (int64, error) {
	res, err := d.pool.ExecContext(ctx, `UPDATE files SET pin_session_id = $1, pin_id = $2, folder_id = $3, updated_at = $4
		WHERE pin_session_id = $5 AND state = 'receiving' AND (enc_version IS NULL OR folder_id = $6)`,
		toSession, toPin, toFolder, at, fromSession, toFolder)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MoveUploadsToPerson hands the unfinished uploads of a browser's PIN session to the person
// who just signed in in that browser, so they continue instead of starting over.
func (d *DB) MoveUploadsToPerson(ctx context.Context, fromSession, userID, deviceID string, at time.Time) (int64, error) {
	res, err := d.pool.ExecContext(ctx, `UPDATE files SET pin_session_id = NULL, pin_id = NULL, user_id = $1, device_id = $2, updated_at = $3
		WHERE pin_session_id = $4 AND state = 'receiving'`, userID, deviceID, at, fromSession)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteStalePinSessions removes sessions that were revoked, or whose PIN ended, before
// the given time and that no unfinished upload still belongs to.
func (d *DB) DeleteStalePinSessions(ctx context.Context, before time.Time) (int64, error) {
	res, err := d.pool.ExecContext(ctx, `DELETE FROM pin_sessions WHERE id IN (
			SELECT s.id FROM pin_sessions s JOIN pins p ON p.id = s.pin_id
			WHERE (s.revoked_at IS NOT NULL AND s.revoked_at < $1)
			   OR (p.ended_at IS NOT NULL AND p.ended_at < $1)
			   OR (p.expires_at IS NOT NULL AND p.expires_at < $1)
		) AND NOT EXISTS (SELECT 1 FROM files f WHERE f.pin_session_id = pin_sessions.id AND f.state = 'receiving')`,
		before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
