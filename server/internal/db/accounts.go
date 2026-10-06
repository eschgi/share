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

// ErrNotFirst means someone was to be the first with an account, but someone else is.
var ErrNotFirst = errors.New("someone has an account already")

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
	err := row.Scan(&u.ID, &u.Name, &username, &hash, &u.Role, &u.CreatedAt, &u.CreatedBy)
	if noRow(err) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	u.Username, u.PasswordHash = username.String, hash.String
	return u, nil
}

func insertUser(ctx context.Context, tx *sql.Tx, u User) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO users ("+userColumns+") VALUES ($1, $2, $3, $4, $5, $6, $7)",
		u.ID, u.Name, nullString(u.Username), nullString(u.PasswordHash), u.Role, u.CreatedAt, u.CreatedBy)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// InsertUser adds a person. It returns ErrConflict if the username is taken.
func (d *DB) InsertUser(ctx context.Context, u User) error {
	return d.inTx(ctx, func(tx *sql.Tx) error { return insertUser(ctx, tx, u) })
}

// InsertFirstUser adds the first person with an account and signs in their phone or browser;
// ErrNotFirst if someone has an account already.
func (d *DB) InsertFirstUser(ctx context.Context, u User, dv Device, tokenHash []byte) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrNotFirst
		}
		if err := insertUser(ctx, tx, u); err != nil {
			return err
		}
		return insertDevice(ctx, tx, dv, tokenHash)
	})
}

// UserByID returns one person.
func (d *DB) UserByID(ctx context.Context, id string) (User, error) {
	return scanUser(d.pool.QueryRowContext(ctx, "SELECT "+userColumns+" FROM users WHERE id = $1", id))
}

// userByUsername finds a person on users_username.
const userByUsername = "SELECT " + userColumns + " FROM users WHERE casefold(username) = casefold($1 COLLATE pg_c_utf8)"

// UserByUsername finds a person by username, ignoring case.
func (d *DB) UserByUsername(ctx context.Context, username string) (User, error) {
	return scanUser(d.pool.QueryRowContext(ctx, userByUsername, username))
}

// Users lists everyone, admins first, then by name.
func (d *DB) Users(ctx context.Context) ([]User, error) {
	return queryAll(ctx, d.pool, scanUser, "SELECT "+userColumns+" FROM users ORDER BY role = 'member', casefold(name), id")
}

// UserCount counts the people with an account.
func (d *DB) UserCount(ctx context.Context) (int, error) {
	var n int
	err := d.pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

// SetLogin gives a person a username and password hash. It returns ErrConflict if the
// username belongs to someone else, and ErrNotFound if there is no such person.
func (d *DB) SetLogin(ctx context.Context, userID, username, passwordHash string) error {
	res, err := d.pool.ExecContext(ctx, "UPDATE users SET username = $1, password_hash = $2 WHERE id = $3",
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
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var role string
		err := tx.QueryRowContext(ctx, "SELECT role FROM users WHERE id = $1", id).Scan(&role)
		if noRow(err) {
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
		if err := loseFolders(ctx, tx, id, ""); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM users WHERE id = $1", id)
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
	err := row.Scan(&dv.ID, &dv.UserID, &dv.Name, &dv.Client, &dv.HomeOnly, &dv.CreatedAt, &dv.LastSeenAt, &dv.RevokedAt)
	if noRow(err) {
		return dv, ErrNotFound
	}
	return dv, err
}

func insertDevice(ctx context.Context, tx *sql.Tx, dv Device, tokenHash []byte) error {
	if dv.Client == "" {
		dv.Client = ClientApp
	}
	_, err := tx.ExecContext(ctx,
		"INSERT INTO devices (id, user_id, token_hash, name, client, home_only, created_at, last_seen_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)",
		dv.ID, dv.UserID, tokenHash, dv.Name, dv.Client, dv.HomeOnly, dv.CreatedAt, dv.LastSeenAt)
	return err
}

// InsertDevice signs a phone or browser in for a person; only the token's hash is kept.
func (d *DB) InsertDevice(ctx context.Context, dv Device, tokenHash []byte) error {
	return d.inTx(ctx, func(tx *sql.Tx) error { return insertDevice(ctx, tx, dv, tokenHash) })
}

// DeviceByToken finds a phone or browser and its person by token hash. It doesn't judge
// validity; callers check RevokedAt.
func (d *DB) DeviceByToken(ctx context.Context, tokenHash []byte) (Device, User, error) {
	row := d.pool.QueryRowContext(ctx, `SELECT d.id, d.user_id, d.name, d.client, d.home_only, d.created_at, d.last_seen_at, d.revoked_at,
			u.id, u.name, u.username, u.password_hash, u.role, u.created_at, u.created_by
		FROM devices d JOIN users u ON u.id = d.user_id WHERE d.token_hash = $1`, tokenHash)
	var dv Device
	var u User
	var username, hash sql.NullString
	err := row.Scan(&dv.ID, &dv.UserID, &dv.Name, &dv.Client, &dv.HomeOnly, &dv.CreatedAt, &dv.LastSeenAt, &dv.RevokedAt,
		&u.ID, &u.Name, &username, &hash, &u.Role, &u.CreatedAt, &u.CreatedBy)
	if noRow(err) {
		return dv, u, ErrNotFound
	}
	if err != nil {
		return dv, u, err
	}
	u.Username, u.PasswordHash = username.String, hash.String
	return dv, u, nil
}

// DeviceTokenHash is the stored hash of a signed-in phone's key, for the proof that a server
// at home is that phone's own (auth.HomeProof). ErrNotFound for unknown or signed-out phones.
func (d *DB) DeviceTokenHash(ctx context.Context, id string) ([]byte, error) {
	var hash []byte
	err := d.pool.QueryRowContext(ctx, "SELECT token_hash FROM devices WHERE id = $1 AND revoked_at IS NULL", id).Scan(&hash)
	if noRow(err) {
		return nil, ErrNotFound
	}
	return hash, err
}

// DevicesOf lists a person's phones and browsers that are still signed in, most recently used
// first.
func (d *DB) DevicesOf(ctx context.Context, userID string) ([]Device, error) {
	return queryAll(ctx, d.pool, scanDevice, "SELECT "+deviceColumns+` FROM devices
		WHERE user_id = $1 AND revoked_at IS NULL ORDER BY last_seen_at DESC, id`, userID)
}

// TouchDevice records that a phone was just used.
func (d *DB) TouchDevice(ctx context.Context, id string, at time.Time) error {
	_, err := d.pool.ExecContext(ctx, "UPDATE devices SET last_seen_at = $1 WHERE id = $2", at, id)
	return err
}

// RevokeDevice signs a phone or browser out; its token stops working.
func (d *DB) RevokeDevice(ctx context.Context, id string, at time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE devices SET revoked_at = $1 WHERE id = $2 AND revoked_at IS NULL", at, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM person_keys WHERE device_id = $1", id)
		return err
	})
}

// DeleteEndedDevices forgets phones and browsers that were signed out before revokedBefore,
// and browsers nobody used since idleBefore; their keys can't work any more. Nothing refers to
// a device by foreign key: files and invites keep the id as plain text.
func (d *DB) DeleteEndedDevices(ctx context.Context, revokedBefore, idleBefore time.Time) (int64, error) {
	res, err := d.pool.ExecContext(ctx, `DELETE FROM devices WHERE (revoked_at IS NOT NULL AND revoked_at < $1)
		OR (client = 'web' AND last_seen_at < $2)`, revokedBefore, idleBefore)
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
	Folders   []string    // the folders a new member gets; only InsertInvite reads it
	Keys      []InviteKey // keys locked with the link's secret; only InsertInvite reads it
}

// InviteKey is a key locked with the secret of an invite's link: a version of a folder's key,
// or the person's own key ("" and 0) for a new phone or browser.
type InviteKey struct {
	FolderID string
	Version  int
	Locked   []byte
}

const inviteColumns = "id, name, role, user_id, created_by, created_at, expires_at, used_at, device_id, revoked_at"

func scanInvite(row interface{ Scan(...any) error }) (Invite, error) {
	var in Invite
	var userID, deviceID sql.NullString
	err := row.Scan(&in.ID, &in.Name, &in.Role, &userID, &in.CreatedBy, &in.CreatedAt, &in.ExpiresAt, &in.UsedAt, &deviceID, &in.RevokedAt)
	if noRow(err) {
		return in, ErrNotFound
	}
	if err != nil {
		return in, err
	}
	in.UserID, in.DeviceID = userID.String, deviceID.String
	return in, nil
}

// InsertInvite stores a new invite with the folders it gives; only the token's hash is kept.
func (d *DB) InsertInvite(ctx context.Context, in Invite, tokenHash []byte) error {
	var personKey []byte // the person's own key, without a folder, is kept with the invite
	for _, k := range in.Keys {
		if k.FolderID == "" {
			personKey = k.Locked
		}
	}
	return d.inTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO invites (id, token_hash, name, role, user_id, created_by, created_at, expires_at, person_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			in.ID, tokenHash, in.Name, in.Role, nullString(in.UserID), in.CreatedBy, in.CreatedAt, in.ExpiresAt, personKey)
		if err != nil {
			return err
		}
		for _, f := range in.Folders {
			if _, err := tx.ExecContext(ctx, "INSERT INTO invite_folders (invite_id, folder_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", in.ID, f); err != nil {
				return err
			}
		}
		for _, k := range in.Keys {
			if k.FolderID == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO invite_keys (invite_id, folder_id, version, locked) VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING",
				in.ID, k.FolderID, k.Version, k.Locked); err != nil {
				return err
			}
		}
		return nil
	})
}

// InviteByToken finds an invite by token hash, whatever its state.
func (d *DB) InviteByToken(ctx context.Context, tokenHash []byte) (Invite, error) {
	return scanInvite(d.pool.QueryRowContext(ctx, "SELECT "+inviteColumns+" FROM invites WHERE token_hash = $1", tokenHash))
}

// OpenInvites lists invites that can still be used, newest first.
func (d *DB) OpenInvites(ctx context.Context, now time.Time) ([]Invite, error) {
	return queryAll(ctx, d.pool, scanInvite, "SELECT "+inviteColumns+` FROM invites
		WHERE used_at IS NULL AND revoked_at IS NULL AND expires_at > $1 ORDER BY created_at DESC, id`, now)
}

// UseInvite accepts an invite in one transaction: it creates the person (unless the invite
// adds a phone for someone) with the invite's folders, signs the phone in and marks the
// invite used. It returns ErrConflict if the invite was used, revoked or expired in the
// meantime.
func (d *DB) UseInvite(ctx context.Context, inviteID string, u User, dv Device, tokenHash []byte, now time.Time) ([]InviteKey, error) {
	var keys []InviteKey
	err := d.inTx(ctx, func(tx *sql.Tx) error {
		keys = nil
		// The person's own key, locked with the link's secret, goes to the new device once.
		var personKey []byte
		err := tx.QueryRowContext(ctx, `UPDATE invites SET used_at = $1, device_id = $2, person_key = NULL
			WHERE id = $3 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > $4 RETURNING old.person_key`,
			now, dv.ID, inviteID, now).Scan(&personKey)
		if noRow(err) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if personKey != nil {
			keys = append(keys, InviteKey{Locked: personKey})
		}
		if u.ID != dv.UserID {
			return errors.New("db: the phone must belong to the person the invite is for")
		}
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE id = $1", u.ID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 { // a new person
			if err := insertUser(ctx, tx, u); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO folder_people (folder_id, user_id)
				SELECT i.folder_id, $1 FROM invite_folders i JOIN folders f ON f.id = i.folder_id
				WHERE i.invite_id = $2 AND f.deleted_at IS NULL ON CONFLICT DO NOTHING`, u.ID, inviteID); err != nil {
				return err
			}
		}
		// So do the folders' keys locked with it.
		err = eachRow(ctx, tx, "SELECT folder_id, version, locked FROM invite_keys WHERE invite_id = $1 ORDER BY folder_id, version", []any{inviteID},
			func(row scanner) error {
				var k InviteKey
				if err := row.Scan(&k.FolderID, &k.Version, &k.Locked); err != nil {
					return err
				}
				keys = append(keys, k)
				return nil
			})
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM invite_keys WHERE invite_id = $1", inviteID); err != nil {
			return err
		}
		return insertDevice(ctx, tx, dv, tokenHash)
	})
	return keys, err
}

// DeleteOldInvites drops invites that ended before before.
func (d *DB) DeleteOldInvites(ctx context.Context, before time.Time) (int64, error) {
	res, err := d.pool.ExecContext(ctx, `DELETE FROM invites WHERE expires_at < $1 OR used_at < $2 OR revoked_at < $3`,
		before, before, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
