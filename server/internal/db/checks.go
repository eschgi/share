package db

import (
	"context"
	"database/sql"
	"time"
)

// CheckLife is how long a check stays open.
const CheckLife = 15 * time.Minute

// A Check comes before keys are passed on (docs/e2ee-plan.md): a device that has them asks the
// device or person that would get them to show the same code. The server relays a commitment,
// two one-time keys and the confirmation, in that order.
type Check struct {
	ID           string
	Asker        string // the asking device
	AskerName    string // its name, which the waiting side shows with the code
	DeviceID     string // a device of the asker's person that waits for the person's key; "" for a person
	UserID       string // another person, who waits for folder keys; "" for a device
	Commitment   []byte
	Answer       []byte // the waiting side's one-time key; nil until it answers
	AnsweredBy   string // the device that answered; "" until one did
	Reveal       []byte // the asker's one-time key; nil until it is revealed
	Confirmation []byte // what the asker hands on after Allow; nil until then
	CreatedAt    time.Time
}

// NewCheck opens a check from askerDevice, a device of askerUser that holds the person's key,
// for deviceID, a device of the same person that lacks it, or for userID, a person who lacks a
// version of a folder's key that askerUser holds, or an admin who lacks the root's private key
// that askerUser holds. It replaces the asker's earlier check for the same one; ErrNotFound when
// the asker or the one it asks doesn't qualify.
func (d *DB) NewCheck(ctx context.Context, id, askerDevice, askerUser, deviceID, userID string, commitment []byte, now time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM key_checks WHERE created_at <= $1", now.Add(-CheckLife)); err != nil {
			return err
		}
		var ok bool
		err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM person_keys WHERE device_id = $1) AND CASE
			WHEN $2::uuid IS NOT NULL THEN EXISTS (SELECT 1 FROM devices WHERE id = $2 AND user_id = $4 AND revoked_at IS NULL
				AND public_key IS NOT NULL AND id NOT IN (SELECT device_id FROM person_keys))
			ELSE EXISTS (SELECT 1 FROM folder_grants g JOIN users u ON u.id = $3 AND u.id <> $4 AND u.public_key IS NOT NULL AND `+sees("g.folder_id")+`
				WHERE g.user_id = $4 AND NOT EXISTS (SELECT 1 FROM folder_grants o
					WHERE o.folder_id = g.folder_id AND o.version = g.version AND o.user_id = u.id))
			OR EXISTS (SELECT 1 FROM users u WHERE u.id = $3 AND u.id <> $4 AND u.role = 'admin' AND u.public_key IS NOT NULL
				AND EXISTS (SELECT 1 FROM root_grants WHERE user_id = $4) AND NOT EXISTS (SELECT 1 FROM root_grants WHERE user_id = u.id))
			END`, askerDevice, nullString(deviceID), nullString(userID), askerUser).Scan(&ok)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM key_checks WHERE asker = $1 AND (device_id = $2 OR user_id = $3)",
			askerDevice, nullString(deviceID), nullString(userID)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO key_checks (id, asker, device_id, user_id, commitment, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`, id, askerDevice, nullString(deviceID), nullString(userID), commitment, now)
		return err
	})
}

// AnswerCheck keeps the waiting side's one-time key: from the device the check is for, or from a
// device of the person it is for that holds their key. Until the asker revealed its key a device
// may answer again, e.g. after a reload; ErrNotFound for anyone else, or a check that is closed.
func (d *DB) AnswerCheck(ctx context.Context, id, deviceID, userID string, key []byte, now time.Time) error {
	res, err := d.pool.ExecContext(ctx, `UPDATE key_checks SET answer = $1, answered_by = $2
		WHERE id = $3 AND reveal IS NULL AND created_at > $5 AND (device_id = $2 OR (user_id = $4
			AND (answered_by IS NULL OR answered_by = $2) AND EXISTS (SELECT 1 FROM person_keys WHERE device_id = $2)))`,
		key, deviceID, id, userID, now.Add(-CheckLife))
	return oneRow(res, err)
}

// RevealCheck keeps the asker's one-time key, for the answer it saw, which is the one the code is
// made from: ErrNotFound for another device, a check that isn't waiting for it, or one whose
// answer changed meanwhile. The caller checks the key against the commitment.
func (d *DB) RevealCheck(ctx context.Context, id, askerDevice string, key, answer []byte, now time.Time) error {
	res, err := d.pool.ExecContext(ctx, `UPDATE key_checks SET reveal = $1
		WHERE id = $2 AND asker = $3 AND answer = $4 AND reveal IS NULL AND created_at > $5`,
		key, id, askerDevice, answer, now.Add(-CheckLife))
	return oneRow(res, err)
}

// ConfirmCheck keeps what the asker hands on after Allow, once it revealed its key:
// ErrNotFound for another device, or a check that isn't revealed, confirmed already or closed.
func (d *DB) ConfirmCheck(ctx context.Context, id, askerDevice string, confirmation []byte, now time.Time) error {
	res, err := d.pool.ExecContext(ctx, `UPDATE key_checks SET confirmation = $1
		WHERE id = $2 AND asker = $3 AND reveal IS NOT NULL AND confirmation IS NULL AND created_at > $4`,
		confirmation, id, askerDevice, now.Add(-CheckLife))
	return oneRow(res, err)
}

// DeleteCheck closes a check, for the asker or the side it is for; ErrNotFound otherwise.
func (d *DB) DeleteCheck(ctx context.Context, id, deviceID, userID string) error {
	res, err := d.pool.ExecContext(ctx, "DELETE FROM key_checks WHERE id = $1 AND (asker = $2 OR device_id = $2 OR user_id = $3)",
		id, deviceID, userID)
	return oneRow(res, err)
}

// CheckByID returns an open check.
func (d *DB) CheckByID(ctx context.Context, id string, now time.Time) (Check, error) {
	c, err := scanCheck(d.pool.QueryRowContext(ctx, checkSelect+" WHERE c.id = $1 AND c.created_at > $2", id, now.Add(-CheckLife)))
	if noRow(err) {
		return c, ErrNotFound
	}
	return c, err
}

// ChecksOf lists the open checks a device takes part in: those it asks, those for it, and
// those for its person while it holds their key and no other device of theirs answered.
func (d *DB) ChecksOf(ctx context.Context, deviceID, userID string, now time.Time) ([]Check, error) {
	return queryAll(ctx, d.pool, scanCheck, checkSelect+` WHERE c.created_at > $3 AND (c.asker = $1 OR c.device_id = $1
		OR (c.user_id = $2 AND (c.answered_by IS NULL OR c.answered_by = $1) AND EXISTS (SELECT 1 FROM person_keys WHERE device_id = $1)))
		ORDER BY c.created_at, c.id`, deviceID, userID, now.Add(-CheckLife))
}

const checkSelect = `SELECT c.id, c.asker, a.name, c.device_id, c.user_id, c.commitment, c.answer, c.answered_by, c.reveal, c.confirmation, c.created_at
	FROM key_checks c JOIN devices a ON a.id = c.asker`

func scanCheck(row scanner) (Check, error) {
	var c Check
	var deviceID, userID, answeredBy sql.NullString
	err := row.Scan(&c.ID, &c.Asker, &c.AskerName, &deviceID, &userID, &c.Commitment, &c.Answer, &answeredBy, &c.Reveal, &c.Confirmation, &c.CreatedAt)
	c.DeviceID, c.UserID, c.AnsweredBy = deviceID.String, userID.String, answeredBy.String
	return c, err
}

// oneRow turns an update that changed nothing into ErrNotFound.
func oneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	return nil
}
