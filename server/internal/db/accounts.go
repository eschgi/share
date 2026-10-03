package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Roles.
const (
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// ErrLastAdmin means the change would leave nobody who can manage the server.
var ErrLastAdmin = errors.New("the last admin can't go")

// User is a person with an account.
type User struct {
	ID           string
	Name         string
	Username     string // empty if the person never set one
	PasswordHash string // empty without a password
	Role         string
	CreatedAt    time.Time
	CreatedBy    string
}

const userColumns = "id, name, username, password_hash, role, created_at, created_by"

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var username, hash sql.NullString
	var created int64
	err := row.Scan(&u.ID, &u.Name, &username, &hash, &u.Role, &created, &u.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	u.Username, u.PasswordHash, u.CreatedAt = username.String, hash.String, fromMS(created)
	return u, nil
}

func insertUser(ctx context.Context, tx *sql.Tx, u User) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO users ("+userColumns+") VALUES (?, ?, ?, ?, ?, ?, ?)",
		u.ID, u.Name, nullString(u.Username), nullString(u.PasswordHash), u.Role, ms(u.CreatedAt), u.CreatedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// InsertUser adds a person. It returns ErrConflict if the username is taken.
func (d *DB) InsertUser(ctx context.Context, u User) error {
	return d.Tx(ctx, func(tx *sql.Tx) error { return insertUser(ctx, tx, u) })
}

// UserByID returns one person.
func (d *DB) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(d.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = ?", id))
}

// UserByUsername finds a person by username, ignoring case.
func (d *DB) UserByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(d.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE username = ?", username))
}

// Users lists everyone, admins first, then by name.
func (d *DB) Users(ctx context.Context) ([]User, error) {
	rows, err := d.QueryContext(ctx, "SELECT "+userColumns+" FROM users ORDER BY role = 'member', name COLLATE NOCASE, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UserCount counts the people with an account.
func (d *DB) UserCount(ctx context.Context) (int, error) {
	var n int
	err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

// SetLogin gives a person a username and password hash. It returns ErrConflict if the
// username belongs to someone else, and ErrNotFound if there is no such person.
func (d *DB) SetLogin(ctx context.Context, userID, username, passwordHash string) error {
	res, err := d.ExecContext(ctx, "UPDATE users SET username = ?, password_hash = ? WHERE id = ?",
		nullString(username), nullString(passwordHash), userID)
	if isUniqueViolation(err) {
		return ErrConflict
	}
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

// DeleteUser removes a person together with their phones and open invites. Files they sent
// stay in the library. It returns ErrLastAdmin for the last admin.
func (d *DB) DeleteUser(ctx context.Context, id string) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		var role string
		err := tx.QueryRowContext(ctx, "SELECT role FROM users WHERE id = ?", id).Scan(&role)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if role == RoleAdmin {
			var admins int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&admins); err != nil {
				return err
			}
			if admins <= 1 {
				return ErrLastAdmin
			}
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
		return err
	})
}

// Device clients: the app on a phone, or a browser with the key in a cookie.
const (
	ClientApp = "app"
	ClientWeb = "web"
)

// Device is a signed-in phone or browser.
type Device struct {
	ID         string
	UserID     string
	Name       string
	Client     string // ClientApp or ClientWeb
	HomeOnly   bool   // a browser that signed in at home; it works only there
	CreatedAt  time.Time
	LastSeenAt time.Time
	RevokedAt  *time.Time
}

const deviceColumns = "id, user_id, name, client, home_only, created_at, last_seen_at, revoked_at"

func scanDevice(row interface{ Scan(...any) error }) (Device, error) {
	var dv Device
	var created, seen int64
	var revoked sql.NullInt64
	err := row.Scan(&dv.ID, &dv.UserID, &dv.Name, &dv.Client, &dv.HomeOnly, &created, &seen, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return dv, ErrNotFound
	}
	if err != nil {
		return dv, err
	}
	dv.CreatedAt, dv.LastSeenAt, dv.RevokedAt = fromMS(created), fromMS(seen), optTime(revoked)
	return dv, nil
}

func insertDevice(ctx context.Context, tx *sql.Tx, dv Device, tokenHash []byte) error {
	if dv.Client == "" {
		dv.Client = ClientApp
	}
	_, err := tx.ExecContext(ctx,
		"INSERT INTO devices (id, user_id, token_hash, name, client, home_only, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		dv.ID, dv.UserID, tokenHash, dv.Name, dv.Client, dv.HomeOnly, ms(dv.CreatedAt), ms(dv.LastSeenAt))
	return err
}

// InsertDevice signs a phone or browser in for a person; only the token's hash is kept.
func (d *DB) InsertDevice(ctx context.Context, dv Device, tokenHash []byte) error {
	return d.Tx(ctx, func(tx *sql.Tx) error { return insertDevice(ctx, tx, dv, tokenHash) })
}

// DeviceByToken finds a phone or browser and its person by token hash. It doesn't judge
// validity; callers check RevokedAt.
func (d *DB) DeviceByToken(ctx context.Context, tokenHash []byte) (Device, User, error) {
	row := d.QueryRowContext(ctx, `SELECT d.id, d.user_id, d.name, d.client, d.home_only, d.created_at, d.last_seen_at, d.revoked_at,
			u.id, u.name, u.username, u.password_hash, u.role, u.created_at, u.created_by
		FROM devices d JOIN users u ON u.id = d.user_id WHERE d.token_hash = ?`, tokenHash)
	var dv Device
	var u User
	var created, seen, uCreated int64
	var revoked sql.NullInt64
	var username, hash sql.NullString
	err := row.Scan(&dv.ID, &dv.UserID, &dv.Name, &dv.Client, &dv.HomeOnly, &created, &seen, &revoked,
		&u.ID, &u.Name, &username, &hash, &u.Role, &uCreated, &u.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return dv, u, ErrNotFound
	}
	if err != nil {
		return dv, u, err
	}
	dv.CreatedAt, dv.LastSeenAt, dv.RevokedAt = fromMS(created), fromMS(seen), optTime(revoked)
	u.Username, u.PasswordHash, u.CreatedAt = username.String, hash.String, fromMS(uCreated)
	return dv, u, nil
}

// DeviceTokenHash is the stored hash of a signed-in phone's key, for the proof that a server
// at home is that phone's own (auth.HomeProof). ErrNotFound for unknown or signed-out phones.
func (d *DB) DeviceTokenHash(ctx context.Context, id string) ([]byte, error) {
	var hash []byte
	err := d.QueryRowContext(ctx, "SELECT token_hash FROM devices WHERE id = ? AND revoked_at IS NULL", id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return hash, err
}

// DevicesOf lists a person's phones and browsers that are still signed in, most recently used
// first.
func (d *DB) DevicesOf(ctx context.Context, userID string) ([]Device, error) {
	rows, err := d.QueryContext(ctx, "SELECT "+deviceColumns+` FROM devices
		WHERE user_id = ? AND revoked_at IS NULL ORDER BY last_seen_at DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		dv, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, dv)
	}
	return out, rows.Err()
}

// TouchDevice records that a phone was just used.
func (d *DB) TouchDevice(ctx context.Context, id string, at time.Time) error {
	_, err := d.ExecContext(ctx, "UPDATE devices SET last_seen_at = ? WHERE id = ?", ms(at), id)
	return err
}

// RevokeDevice signs a phone or browser out; its token stops working.
func (d *DB) RevokeDevice(ctx context.Context, id string, at time.Time) error {
	_, err := d.ExecContext(ctx, "UPDATE devices SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", ms(at), id)
	return err
}

// DeleteEndedDevices forgets phones and browsers that were signed out before revokedBefore,
// and browsers nobody used since idleBefore; their keys can't work any more. Nothing refers to
// a device by foreign key: files and invites keep the id as plain text.
func (d *DB) DeleteEndedDevices(ctx context.Context, revokedBefore, idleBefore time.Time) (int64, error) {
	res, err := d.ExecContext(ctx, `DELETE FROM devices WHERE (revoked_at IS NOT NULL AND revoked_at < ?)
		OR (client = 'web' AND last_seen_at < ?)`, ms(revokedBefore), ms(idleBefore))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Invite lets someone sign a phone in without a password.
type Invite struct {
	ID        string
	Name      string
	Role      string
	UserID    string // set: adds a phone for this person
	CreatedBy string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	DeviceID  string
	RevokedAt *time.Time
	Folders   []string // the folders a new member gets; only InsertInvite reads it
}

const inviteColumns = "id, name, role, user_id, created_by, created_at, expires_at, used_at, device_id, revoked_at"

func scanInvite(row interface{ Scan(...any) error }) (Invite, error) {
	var in Invite
	var userID, deviceID sql.NullString
	var created, expires int64
	var used, revoked sql.NullInt64
	err := row.Scan(&in.ID, &in.Name, &in.Role, &userID, &in.CreatedBy, &created, &expires, &used, &deviceID, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return in, ErrNotFound
	}
	if err != nil {
		return in, err
	}
	in.UserID, in.DeviceID = userID.String, deviceID.String
	in.CreatedAt, in.ExpiresAt, in.UsedAt, in.RevokedAt = fromMS(created), fromMS(expires), optTime(used), optTime(revoked)
	return in, nil
}

// InsertInvite stores a new invite with the folders it gives; only the token's hash is kept.
func (d *DB) InsertInvite(ctx context.Context, in Invite, tokenHash []byte) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO invites (id, token_hash, name, role, user_id, created_by, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			in.ID, tokenHash, in.Name, in.Role, nullString(in.UserID), in.CreatedBy, ms(in.CreatedAt), ms(in.ExpiresAt))
		if err != nil {
			return err
		}
		for _, f := range in.Folders {
			if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO invite_folders (invite_id, folder_id) VALUES (?, ?)", in.ID, f); err != nil {
				return err
			}
		}
		return nil
	})
}

// InviteByToken finds an invite by token hash, whatever its state.
func (d *DB) InviteByToken(ctx context.Context, tokenHash []byte) (Invite, error) {
	return scanInvite(d.QueryRowContext(ctx, "SELECT "+inviteColumns+" FROM invites WHERE token_hash = ?", tokenHash))
}

// OpenInvites lists invites that can still be used, newest first.
func (d *DB) OpenInvites(ctx context.Context, now time.Time) ([]Invite, error) {
	rows, err := d.QueryContext(ctx, "SELECT "+inviteColumns+` FROM invites
		WHERE used_at IS NULL AND revoked_at IS NULL AND expires_at > ? ORDER BY created_at DESC, id`, ms(now))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		in, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// RevokeInvitesBy ends the open invites made by createdBy, e.g. an older first-start invite.
func (d *DB) RevokeInvitesBy(ctx context.Context, createdBy string, at time.Time) error {
	_, err := d.ExecContext(ctx, "UPDATE invites SET revoked_at = ? WHERE created_by = ? AND used_at IS NULL AND revoked_at IS NULL",
		ms(at), createdBy)
	return err
}

// UseInvite accepts an invite in one transaction: it creates the person (unless the invite
// adds a phone for someone) with the invite's folders, signs the phone in and marks the
// invite used. It returns ErrConflict if the invite was used, revoked or expired in the
// meantime.
func (d *DB) UseInvite(ctx context.Context, inviteID string, u User, dv Device, tokenHash []byte, now time.Time) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE invites SET used_at = ?, device_id = ?
			WHERE id = ? AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`, ms(now), dv.ID, inviteID, ms(now))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrConflict
		}
		if u.ID != dv.UserID {
			return errors.New("db: the phone must belong to the person the invite is for")
		}
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id = ?", u.ID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 { // a new person
			if err := insertUser(ctx, tx, u); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO folder_people (folder_id, user_id)
				SELECT i.folder_id, ? FROM invite_folders i JOIN folders f ON f.id = i.folder_id
				WHERE i.invite_id = ? AND f.deleted_at IS NULL`, u.ID, inviteID); err != nil {
				return err
			}
		}
		return insertDevice(ctx, tx, dv, tokenHash)
	})
}

// DeleteOldInvites drops invites that ended before before.
func (d *DB) DeleteOldInvites(ctx context.Context, before time.Time) (int64, error) {
	res, err := d.ExecContext(ctx, `DELETE FROM invites WHERE expires_at < ? OR used_at < ? OR revoked_at < ?`,
		ms(before), ms(before), ms(before))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
