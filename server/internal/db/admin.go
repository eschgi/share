package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SetRole makes a person an admin or a member. It returns ErrLastAdmin when that would leave
// no admin. An admin who becomes a member keeps seeing every folder, until someone switches
// some off.
func (d *DB) SetRole(ctx context.Context, id, role string) error {
	if role != RoleAdmin && role != RoleMember {
		return errors.New("db: unknown role " + role)
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		var current string
		err := tx.QueryRowContext(ctx, "SELECT role FROM users WHERE id = $1", id).Scan(&current)
		if noRow(err) {
			return ErrNotFound
		}
		if err != nil || current == role {
			return err
		}
		if current == RoleAdmin {
			var admins int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&admins); err != nil {
				return err
			}
			if admins <= 1 {
				return ErrLastAdmin
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE users SET role = $1 WHERE id = $2", role, id); err != nil {
			return err
		}
		if role != RoleMember {
			return nil
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO folder_people (folder_id, user_id)
			SELECT id, $1 FROM folders WHERE deleted_at IS NULL ON CONFLICT DO NOTHING`, id)
		return err
	})
}

// SignedInDevices lists everyone's phones that are still signed in, by person, the most
// recently used first.
func (d *DB) SignedInDevices(ctx context.Context) (map[string][]Device, error) {
	rows, err := d.QueryContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE revoked_at IS NULL ORDER BY last_seen_at DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Device{}
	for rows.Next() {
		dv, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out[dv.UserID] = append(out[dv.UserID], dv)
	}
	return out, rows.Err()
}

// DeviceByID returns one phone, signed in or not.
func (d *DB) DeviceByID(ctx context.Context, id string) (Device, error) {
	return scanDevice(d.QueryRowContext(ctx, "SELECT "+deviceColumns+" FROM devices WHERE id = $1", id))
}

// InviteByID returns one invite, whatever its state.
func (d *DB) InviteByID(ctx context.Context, id string) (Invite, error) {
	return scanInvite(d.QueryRowContext(ctx, "SELECT "+inviteColumns+" FROM invites WHERE id = $1", id))
}

// RevokeInvite withdraws an invite that hasn't been used. Withdrawing it again, or a used
// one, changes nothing.
func (d *DB) RevokeInvite(ctx context.Context, id string, at time.Time) error {
	if _, err := d.InviteByID(ctx, id); err != nil {
		return err
	}
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE invites SET revoked_at = $1, person_key = NULL WHERE id = $2 AND used_at IS NULL AND revoked_at IS NULL",
			at, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM invite_keys WHERE invite_id = $1", id) // locked for nobody now
		return err
	})
}

// PinStat says how much a PIN was used: the files sent with it that are in the library, and
// the browsers and phones that unlocked it.
type PinStat struct {
	Files  int
	Phones int
}

// PinStats counts the use of the PINs that haven't ended.
func (d *DB) PinStats(ctx context.Context) (map[string]PinStat, error) {
	rows, err := d.QueryContext(ctx, `SELECT p.id,
			(SELECT COUNT(*) FROM files f WHERE f.pin_id = p.id AND f.state = 'ready'),
			(SELECT COUNT(*) FROM pin_sessions s WHERE s.pin_id = p.id)
		FROM pins p WHERE p.ended_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]PinStat{}
	for rows.Next() {
		var id string
		var s PinStat
		if err := rows.Scan(&id, &s.Files, &s.Phones); err != nil {
			return nil, err
		}
		out[id] = s
	}
	return out, rows.Err()
}
