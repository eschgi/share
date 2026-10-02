package db

import (
	"context"
	"database/sql"
	"errors"
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
	_, err := d.ExecContext(ctx,
		"INSERT INTO pin_sessions (id, pin_id, token_hash, client, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)",
		s.ID, s.PinID, tokenHash, s.Client, ms(s.CreatedAt), ms(s.LastSeenAt))
	return err
}

// PinSessionByToken finds the session for a token hash, together with its PIN. It doesn't
// judge validity; callers check RevokedAt and Pin.LiveAt.
func (d *DB) PinSessionByToken(ctx context.Context, tokenHash []byte) (PinSession, error) {
	var s PinSession
	var created, seen int64
	var revoked sql.NullInt64
	row := d.QueryRowContext(ctx, `SELECT s.id, s.pin_id, s.client, s.created_at, s.last_seen_at, s.revoked_at,
			p.id, p.code, p.kind, p.created_by, p.created_at, p.expires_at, p.ended_at
		FROM pin_sessions s JOIN pins p ON p.id = s.pin_id WHERE s.token_hash = ?`, tokenHash)
	var pCreated int64
	var pExpires, pEnded sql.NullInt64
	err := row.Scan(&s.ID, &s.PinID, &s.Client, &created, &seen, &revoked,
		&s.Pin.ID, &s.Pin.Code, &s.Pin.Kind, &s.Pin.CreatedBy, &pCreated, &pExpires, &pEnded)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.CreatedAt, s.LastSeenAt, s.RevokedAt = fromMS(created), fromMS(seen), optTime(revoked)
	s.Pin.CreatedAt, s.Pin.ExpiresAt, s.Pin.EndedAt = fromMS(pCreated), optTime(pExpires), optTime(pEnded)
	return s, nil
}

// TouchPinSession records that the session was used.
func (d *DB) TouchPinSession(ctx context.Context, id string, at time.Time) error {
	_, err := d.ExecContext(ctx, "UPDATE pin_sessions SET last_seen_at = ? WHERE id = ?", ms(at), id)
	return err
}

// RevokePinSession ends one session.
func (d *DB) RevokePinSession(ctx context.Context, id string, at time.Time) error {
	_, err := d.ExecContext(ctx, "UPDATE pin_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", ms(at), id)
	return err
}

// MoveReceivingUploads hands the unfinished uploads of an old session to a new one. That
// happens when a PIN ended mid-upload and the person unlocked again with a new PIN: the
// uploads continue instead of starting over.
func (d *DB) MoveReceivingUploads(ctx context.Context, fromSession, toSession, toPin string, at time.Time) (int64, error) {
	res, err := d.ExecContext(ctx,
		"UPDATE files SET pin_session_id = ?, pin_id = ?, updated_at = ? WHERE pin_session_id = ? AND state = 'receiving'",
		toSession, toPin, ms(at), fromSession)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// MoveUploadsToPerson hands the unfinished uploads of a browser's PIN session to the person
// who just signed in in that browser, so they continue instead of starting over.
func (d *DB) MoveUploadsToPerson(ctx context.Context, fromSession, userID, deviceID string, at time.Time) (int64, error) {
	res, err := d.ExecContext(ctx, `UPDATE files SET pin_session_id = NULL, pin_id = NULL, user_id = ?, device_id = ?, updated_at = ?
		WHERE pin_session_id = ? AND state = 'receiving'`, userID, deviceID, ms(at), fromSession)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteStalePinSessions removes sessions that were revoked, or whose PIN ended, before
// the given time and that no unfinished upload still belongs to.
func (d *DB) DeleteStalePinSessions(ctx context.Context, before time.Time) (int64, error) {
	res, err := d.ExecContext(ctx, `DELETE FROM pin_sessions WHERE id IN (
			SELECT s.id FROM pin_sessions s JOIN pins p ON p.id = s.pin_id
			WHERE (s.revoked_at IS NOT NULL AND s.revoked_at < ?1)
			   OR (p.ended_at IS NOT NULL AND p.ended_at < ?1)
			   OR (p.expires_at IS NOT NULL AND p.expires_at < ?1)
		) AND NOT EXISTS (SELECT 1 FROM files f WHERE f.pin_session_id = pin_sessions.id AND f.state = 'receiving')`,
		ms(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
