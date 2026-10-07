package db

// The keys of end-to-end encryption (docs/e2ee-plan.md): public keys, and private keys sealed
// for someone or locked with a secret or a password. Nothing here opens a file; the database
// only checks who may give which key to whom.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"time"
)

// ErrNoKey means that a key something needs isn't there: a version of a folder's key, or the
// public key of a person or a device.
var ErrNoKey = errors.New("db: no such key")

// FolderKey is a version of an encrypted folder's key pair.
type FolderKey struct {
	FolderID       string
	Version        int
	PublicKey      []byte
	RecoverySealed []byte // its private key sealed for the recovery key; nil until it is
	CreatedBy      string
	CreatedAt      time.Time
}

// SealedFolderKey is a version of a folder's key, with its private key sealed for one person,
// or nil while it isn't yet.
type SealedFolderKey struct {
	FolderKey
	Sealed []byte
}

// Keys is what a phone or browser needs to open what its person may read.
type Keys struct {
	DevicePublic []byte
	PersonPublic []byte
	PersonSealed []byte // the person's private key, sealed for this device
	PasswordLock []byte // the person's private key, locked with their password
	HeldBy       int    // how many of the person's signed-in devices have it sealed for them
	Folders      []SealedFolderKey
}

// sees is an SQL condition: the person u sees the folder whose id is folder (an expression).
func sees(folder string) string {
	return "(u.role = 'admin' OR EXISTS (SELECT 1 FROM folder_people fp WHERE fp.folder_id = " + folder + " AND fp.user_id = u.id))"
}

// KeysOf gathers the keys of a person and one of their devices: every version of the keys of
// the folders they see, sealed for them or not yet.
func (d *DB) KeysOf(ctx context.Context, userID, deviceID string) (Keys, error) {
	var k Keys
	err := d.pool.QueryRowContext(ctx, `SELECT u.public_key, u.password_lock, dv.public_key, pk.sealed,
			(SELECT COUNT(*) FROM person_keys p JOIN devices o ON o.id = p.device_id WHERE o.user_id = u.id AND o.revoked_at IS NULL)
		FROM users u JOIN devices dv ON dv.user_id = u.id LEFT JOIN person_keys pk ON pk.device_id = dv.id
		WHERE u.id = $1 AND dv.id = $2`, userID, deviceID).Scan(&k.PersonPublic, &k.PasswordLock, &k.DevicePublic, &k.PersonSealed, &k.HeldBy)
	if noRow(err) {
		return k, ErrNotFound
	}
	if err != nil {
		return k, err
	}
	k.Folders, err = queryAll(ctx, d.pool, func(row scanner) (SealedFolderKey, error) {
		var f SealedFolderKey
		err := row.Scan(&f.FolderID, &f.Version, &f.PublicKey, &f.RecoverySealed, &f.CreatedBy, &f.CreatedAt, &f.Sealed)
		return f, err
	}, `SELECT k.folder_id, k.version, k.public_key, k.recovery_sealed, k.created_by, k.created_at, g.sealed
		FROM folder_keys k JOIN users u ON u.id = $1
		LEFT JOIN folder_grants g ON g.folder_id = k.folder_id AND g.version = k.version AND g.user_id = u.id
		WHERE `+sees("k.folder_id")+` ORDER BY k.folder_id, k.version`, userID)
	return k, err
}

// Todo is what a person's device can seal for others with the keys the person holds.
type Todo struct {
	Devices  []DeviceKey     // the person's devices that lack the person's key
	People   []PersonNeed    // people who see a folder and lack a version of its key; for admins only
	Recovery []FolderVersion // versions of folder keys not yet sealed for the recovery key
	Rekey    []string        // folders whose key needs a new version
	Pins     []PinNeed       // PIN links that show a folder and lack a version of its key
}

// DeviceKey is a device of the person that waits for their key: what a check asks about.
type DeviceKey struct {
	ID        string
	PublicKey []byte
	Name      string
	Client    string
	CreatedAt time.Time // when it signed in
	Active    bool      // it was seen within CheckLife, so it can answer a check
}

type PersonNeed struct {
	FolderID  string
	Version   int
	UserID    string
	Name      string
	PublicKey []byte
	Active    bool // a device of theirs that holds their key was seen within CheckLife
}

type FolderVersion struct {
	FolderID string
	Version  int
}

// PinNeed is a version of a folder's key to lock for a PIN link: its secret is sealed for the
// folder key's version SecretVersion.
type PinNeed struct {
	PinID         string
	FolderID      string
	Version       int
	SecretVersion int
	SecretSealed  []byte
}

// TodoOf lists what a person can seal for others, given the versions of folder keys sealed
// for them: only those can they seal on. Only admins pass folder keys on to other people, after
// a check, so only they get the people who lack them. recovery says whether there is a recovery
// key.
func (d *DB) TodoOf(ctx context.Context, userID string, admin, recovery bool, now time.Time) (Todo, error) {
	var t Todo
	// Seen lately: a device is touched every 10 minutes while it is used (auth's touchEvery).
	seen := now.Add(-CheckLife)
	err := eachRow(ctx, d.pool, `SELECT id, public_key, name, client, created_at, last_seen_at > $2 FROM devices WHERE user_id = $1
		AND revoked_at IS NULL AND public_key IS NOT NULL AND id NOT IN (SELECT device_id FROM person_keys)
		ORDER BY created_at, id`, []any{userID, seen}, func(row scanner) error {
		var dk DeviceKey
		if err := row.Scan(&dk.ID, &dk.PublicKey, &dk.Name, &dk.Client, &dk.CreatedAt, &dk.Active); err != nil {
			return err
		}
		t.Devices = append(t.Devices, dk)
		return nil
	})
	if err != nil {
		return t, err
	}
	if admin {
		err = eachRow(ctx, d.pool, `SELECT g.folder_id, g.version, u.id, u.name, u.public_key, EXISTS (SELECT 1 FROM devices v
				JOIN person_keys k ON k.device_id = v.id WHERE v.user_id = u.id AND v.revoked_at IS NULL AND v.last_seen_at > $2)
			FROM folder_grants g JOIN users u ON u.public_key IS NOT NULL AND `+sees("g.folder_id")+`
			WHERE g.user_id = $1 AND NOT EXISTS (SELECT 1 FROM folder_grants o
				WHERE o.folder_id = g.folder_id AND o.version = g.version AND o.user_id = u.id)
			ORDER BY g.folder_id, g.version, u.id`, []any{userID, seen}, func(row scanner) error {
			var n PersonNeed
			if err := row.Scan(&n.FolderID, &n.Version, &n.UserID, &n.Name, &n.PublicKey, &n.Active); err != nil {
				return err
			}
			t.People = append(t.People, n)
			return nil
		})
		if err != nil {
			return t, err
		}
	}
	if recovery {
		err = eachRow(ctx, d.pool, `SELECT k.folder_id, k.version FROM folder_keys k
			JOIN folder_grants g ON g.folder_id = k.folder_id AND g.version = k.version AND g.user_id = $1
			WHERE k.recovery_sealed IS NULL ORDER BY k.folder_id, k.version`, []any{userID}, func(row scanner) error {
			var v FolderVersion
			if err := row.Scan(&v.FolderID, &v.Version); err != nil {
				return err
			}
			t.Recovery = append(t.Recovery, v)
			return nil
		})
		if err != nil {
			return t, err
		}
	}
	err = eachRow(ctx, d.pool, `SELECT f.id FROM folders f WHERE f.rekey = $1 AND f.deleted_at IS NULL AND EXISTS (
			SELECT 1 FROM folder_grants g WHERE g.folder_id = f.id AND g.user_id = $2
			AND g.version = (SELECT MAX(version) FROM folder_keys WHERE folder_id = f.id))
		ORDER BY f.created_at, f.id`, []any{true, userID}, func(row scanner) error {
		var id string
		if err := row.Scan(&id); err != nil {
			return err
		}
		t.Rekey = append(t.Rekey, id)
		return nil
	})
	if err != nil {
		return t, err
	}
	err = eachRow(ctx, d.pool, `SELECT p.id, p.folder_id, g.version, p.secret_version, p.secret_sealed
		FROM pins p JOIN folder_grants g ON g.folder_id = p.folder_id AND g.user_id = $1
		WHERE p.secret_sealed IS NOT NULL AND p.ended_at IS NULL AND (p.expires_at IS NULL OR p.expires_at > $2)
		AND NOT EXISTS (SELECT 1 FROM pin_keys k WHERE k.pin_id = p.id AND k.version = g.version)
		AND EXISTS (SELECT 1 FROM folder_grants s WHERE s.folder_id = p.folder_id AND s.version = p.secret_version AND s.user_id = g.user_id)
		ORDER BY p.id, g.version`, []any{userID, now}, func(row scanner) error {
		var n PinNeed
		if err := row.Scan(&n.PinID, &n.FolderID, &n.Version, &n.SecretVersion, &n.SecretSealed); err != nil {
			return err
		}
		t.Pins = append(t.Pins, n)
		return nil
	})
	return t, err
}

// SetDeviceKey records a device's public key. A new one, from a browser whose storage was
// cleared, drops the person's key sealed for the old one.
func (d *DB) SetDeviceKey(ctx context.Context, deviceID string, public []byte) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var old []byte
		err := tx.QueryRowContext(ctx, "SELECT public_key FROM devices WHERE id = $1 AND revoked_at IS NULL", deviceID).Scan(&old)
		if noRow(err) {
			return ErrNotFound
		}
		if err != nil || string(old) == string(public) {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM person_keys WHERE device_id = $1", deviceID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE devices SET public_key = $1 WHERE id = $2", public, deviceID)
		return err
	})
}

// SetPersonKey records a person's key, made on one of their devices and sealed for it. A
// person who has one already gets ErrConflict, unless they start over: then everything sealed
// for the old one goes, and their other devices and folders wait for someone to seal again.
func (d *DB) SetPersonKey(ctx context.Context, userID, deviceID string, public, sealed []byte, startOver bool, now time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var old, device []byte
		err := tx.QueryRowContext(ctx, `SELECT u.public_key, dv.public_key FROM users u JOIN devices dv ON dv.user_id = u.id
			WHERE u.id = $1 AND dv.id = $2 AND dv.revoked_at IS NULL`, userID, deviceID).Scan(&old, &device)
		if noRow(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if device == nil {
			return ErrNoKey
		}
		if old != nil && !startOver {
			return ErrConflict
		}
		for _, q := range []string{
			"DELETE FROM folder_grants WHERE user_id = $1",
			"DELETE FROM person_keys WHERE device_id IN (SELECT id FROM devices WHERE user_id = $1)",
			"UPDATE users SET password_lock = NULL WHERE id = $1",
		} {
			if _, err := tx.ExecContext(ctx, q, userID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE users SET public_key = $1 WHERE id = $2", public, userID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO person_keys (device_id, sealed, created_at) VALUES ($1, $2, $3)", deviceID, sealed, now)
		return err
	})
}

// SetPasswordLock keeps a person's key locked with their password; nil drops it, as when the
// password changes without a new lock. ErrNoKey for a person without a key.
func (d *DB) SetPasswordLock(ctx context.Context, userID string, lock []byte) error {
	if lock == nil {
		_, err := d.pool.ExecContext(ctx, "UPDATE users SET password_lock = NULL WHERE id = $1", userID)
		return err
	}
	res, err := d.pool.ExecContext(ctx, "UPDATE users SET password_lock = $1 WHERE id = $2 AND public_key IS NOT NULL", lock, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoKey
	}
	return nil
}

// grant checks with check that a key may be given, then keeps it unless one is there.
func (d *DB) grant(ctx context.Context, check string, checkArgs []any, insert string, insertArgs ...any) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var ok int
		if err := tx.QueryRowContext(ctx, check, checkArgs...).Scan(&ok); err != nil {
			return err
		}
		if ok == 0 {
			return ErrNotFound
		}
		_, err := tx.ExecContext(ctx, insert+" ON CONFLICT DO NOTHING", insertArgs...)
		return err
	})
}

// GrantDevice keeps the person's key sealed for one of their devices that lacks it. It
// returns ErrNotFound for a device that isn't the person's, is signed out, or has no key.
func (d *DB) GrantDevice(ctx context.Context, userID, deviceID string, sealed []byte, now time.Time) error {
	return d.grant(ctx, `SELECT COUNT(*) FROM devices WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL AND public_key IS NOT NULL`,
		[]any{deviceID, userID},
		"INSERT INTO person_keys (device_id, sealed, created_at) VALUES ($1, $2, $3)", deviceID, sealed, now)
}

// GrantFolder keeps a version of a folder's key sealed for a person, given by themselves (from an
// invite's link or the recovery code) or by an admin. Both must see the folder, and the person
// must have a key; else ErrNotFound. A key already sealed for them stays.
func (d *DB) GrantFolder(ctx context.Context, giverID, folderID string, version int, userID string, sealed []byte, now time.Time) error {
	return d.grant(ctx, `SELECT COUNT(*) FROM folder_keys k WHERE k.folder_id = $1 AND k.version = $2
		AND EXISTS (SELECT 1 FROM users u WHERE u.id = $3 AND u.public_key IS NOT NULL AND `+sees("k.folder_id")+`)
		AND EXISTS (SELECT 1 FROM users u WHERE u.id = $4 AND `+sees("k.folder_id")+` AND (u.id = $3 OR u.role = 'admin'))`,
		[]any{folderID, version, userID, giverID},
		"INSERT INTO folder_grants (folder_id, version, user_id, sealed, created_at) VALUES ($1, $2, $3, $4, $5)",
		folderID, version, userID, sealed, now)
}

// GrantRecovery keeps a version of a folder's key sealed for the recovery key, if it isn't
// yet, given by someone who holds that version; else ErrNotFound.
func (d *DB) GrantRecovery(ctx context.Context, giverID, folderID string, version int, sealed []byte) error {
	res, err := d.pool.ExecContext(ctx, `UPDATE folder_keys SET recovery_sealed = COALESCE(recovery_sealed, $1) WHERE folder_id = $2 AND version = $3
		AND EXISTS (SELECT 1 FROM folder_grants g WHERE g.folder_id = folder_keys.folder_id AND g.version = folder_keys.version AND g.user_id = $4)`,
		sealed, folderID, version, giverID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// HoldsFolderKey reports whether a version of a folder's key is sealed for a person.
func (d *DB) HoldsFolderKey(ctx context.Context, userID, folderID string, version int) (bool, error) {
	var n int
	err := d.pool.QueryRowContext(ctx, "SELECT COUNT(*) FROM folder_grants WHERE folder_id = $1 AND version = $2 AND user_id = $3",
		folderID, version, userID).Scan(&n)
	return n > 0, err
}

// GrantPin keeps a version of a folder's key locked for the link of a PIN that shows it, if it
// isn't yet, given by someone who holds that version. It returns ErrNotFound unless the PIN
// has a secret and the giver holds the version.
func (d *DB) GrantPin(ctx context.Context, giverID, pinID string, version int, locked []byte) error {
	return d.grant(ctx, `SELECT COUNT(*) FROM pins p JOIN folder_grants g ON g.folder_id = p.folder_id AND g.version = $1 AND g.user_id = $2
		WHERE p.id = $3 AND p.secret_sealed IS NOT NULL`, []any{version, giverID, pinID},
		"INSERT INTO pin_keys (pin_id, version, locked) VALUES ($1, $2, $3)", pinID, version, locked)
}

// NewFolderKey is a new version of a folder's key, made on someone's device, with its private
// key sealed for them and, when there is a recovery key, for that.
type NewFolderKey struct {
	FolderID       string
	Version        int
	PublicKey      []byte
	Sealed         []byte // for its maker
	RecoverySealed []byte
	By             string // its maker's user id
}

func insertFolderKey(ctx context.Context, tx *sql.Tx, k NewFolderKey, now time.Time) error {
	var newest int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM folder_keys WHERE folder_id = $1", k.FolderID).Scan(&newest); err != nil {
		return err
	}
	if k.Version != newest+1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO folder_keys (folder_id, version, public_key, recovery_sealed, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`, k.FolderID, k.Version, k.PublicKey, k.RecoverySealed, k.By, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO folder_grants (folder_id, version, user_id, sealed, created_at) VALUES ($1, $2, $3, $4, $5)",
		k.FolderID, k.Version, k.By, k.Sealed, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, "UPDATE folders SET rekey = $1 WHERE id = $2", false, k.FolderID)
	return err
}

// EncryptFolder makes a folder's new files encrypted, or plain again. The first time it is
// encrypted it needs its key's first version; ErrConflict if that is missing, or comes when
// the folder has a key already.
func (d *DB) EncryptFolder(ctx context.Context, folderID string, on bool, key *NewFolderKey, now time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var newest int
		err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT MAX(version) FROM folder_keys WHERE folder_id = f.id), 0)
			FROM folders f WHERE f.id = $1 AND f.deleted_at IS NULL`, folderID).Scan(&newest)
		if noRow(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		switch {
		case on && newest == 0 && key == nil, key != nil && (newest > 0 || !on):
			return ErrConflict
		case key != nil:
			if err := insertFolderKey(ctx, tx, *key, now); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, "UPDATE folders SET encrypted = $1 WHERE id = $2", on, folderID)
		return err
	})
}

// AddFolderKey records the next version of a folder's key; ErrConflict unless it is the next
// one.
func (d *DB) AddFolderKey(ctx context.Context, key NewFolderKey, now time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error { return insertFolderKey(ctx, tx, key, now) })
}

// FolderKeyOf returns a version of a folder's key.
func (d *DB) FolderKeyOf(ctx context.Context, folderID string, version int) (FolderKey, error) {
	k := FolderKey{FolderID: folderID, Version: version}
	err := d.pool.QueryRowContext(ctx, "SELECT public_key, recovery_sealed, created_by, created_at FROM folder_keys WHERE folder_id = $1 AND version = $2",
		folderID, version).Scan(&k.PublicKey, &k.RecoverySealed, &k.CreatedBy, &k.CreatedAt)
	if noRow(err) {
		return k, ErrNoKey
	}
	return k, err
}

// RecoverableKeys lists every version of every folder's key that is sealed for the recovery
// key.
func (d *DB) RecoverableKeys(ctx context.Context) ([]FolderKey, error) {
	var out []FolderKey
	err := eachRow(ctx, d.pool, `SELECT folder_id, version, public_key, recovery_sealed, created_by, created_at FROM folder_keys
		WHERE recovery_sealed IS NOT NULL ORDER BY folder_id, version`, nil, func(row scanner) error {
		var k FolderKey
		if err := row.Scan(&k.FolderID, &k.Version, &k.PublicKey, &k.RecoverySealed, &k.CreatedBy, &k.CreatedAt); err != nil {
			return err
		}
		out = append(out, k)
		return nil
	})
	return out, err
}

// The recovery key and the setting for new folders live in meta, as base64url and "1".
const (
	metaRecoveryPublic = "e2ee_recovery_public"
	metaRecoveryLocked = "e2ee_recovery_locked"
	metaNewEncrypted   = "e2ee_new_folders_encrypted"
)

var b64 = base64.RawURLEncoding

// RecoveryKey returns the recovery key's public key and its private key locked with the
// recovery code; nil without one.
func (d *DB) RecoveryKey(ctx context.Context) (public, locked []byte, err error) {
	p, err := d.Meta(ctx, metaRecoveryPublic)
	if errors.Is(err, ErrNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	l, err := d.Meta(ctx, metaRecoveryLocked)
	if err != nil {
		return nil, nil, err
	}
	if public, err = b64.DecodeString(p); err != nil {
		return nil, nil, err
	}
	locked, err = b64.DecodeString(l)
	return public, locked, err
}

// SetRecoveryKey records a new recovery key. Whatever was sealed for an older one is dropped:
// the admins' devices seal the folder keys for the new one.
func (d *DB) SetRecoveryKey(ctx context.Context, public, locked []byte) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		for k, v := range map[string]string{metaRecoveryPublic: b64.EncodeToString(public), metaRecoveryLocked: b64.EncodeToString(locked)} {
			if err := setMeta(ctx, tx, k, v); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE folder_keys SET recovery_sealed = NULL")
		return err
	})
}

// NewFoldersEncrypted is the admins' setting: whether a new folder starts encrypted.
func (d *DB) NewFoldersEncrypted(ctx context.Context) (bool, error) {
	v, err := d.Meta(ctx, metaNewEncrypted)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return v == "1", err
}

func (d *DB) SetNewFoldersEncrypted(ctx context.Context, on bool) error {
	v := "0"
	if on {
		v = "1"
	}
	return d.SetMeta(ctx, metaNewEncrypted, v)
}

// PinKey is a version of a folder's key, locked for a PIN link's secret.
type PinKey struct {
	Version int
	Locked  []byte
}

// PinKeys lists the folder keys locked for a PIN's link.
func (d *DB) PinKeys(ctx context.Context, pinID string) ([]PinKey, error) {
	var out []PinKey
	err := eachRow(ctx, d.pool, "SELECT version, locked FROM pin_keys WHERE pin_id = $1 ORDER BY version", []any{pinID}, func(row scanner) error {
		var k PinKey
		if err := row.Scan(&k.Version, &k.Locked); err != nil {
			return err
		}
		out = append(out, k)
		return nil
	})
	return out, err
}

// PinSecretOf returns what the link of a PIN brings for an encrypted folder; nil without it.
func (d *DB) PinSecretOf(ctx context.Context, pinID string) (*PinSecret, error) {
	var s PinSecret
	var version sql.NullInt64
	err := d.pool.QueryRowContext(ctx, "SELECT secret_sealed, secret_version FROM pins WHERE id = $1", pinID).Scan(&s.Sealed, &version)
	if noRow(err) {
		return nil, ErrNotFound
	}
	if err != nil || s.Sealed == nil {
		return nil, err
	}
	s.Version = int(version.Int64)
	if s.Keys, err = d.PinKeys(ctx, pinID); err != nil {
		return nil, err
	}
	return &s, nil
}
