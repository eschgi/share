package db

// The keys of end-to-end encryption (docs/e2ee-plan.md): public keys, and private keys sealed
// for someone or locked with a secret or a password. Nothing here opens a file; the database
// only checks who may give which key to whom, and that the root signed what it must.

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/eschgi/share/server/internal/e2ee"
)

// ErrNoKey means that a key something needs isn't there: a version of a folder's key, the
// public key of a person or a device, or the root.
var ErrNoKey = errors.New("db: no such key")

// ErrBadSignature means that a signature the root must have made doesn't verify under it.
var ErrBadSignature = errors.New("db: the signature doesn't verify under the root")

// FolderKey is a version of an encrypted folder's key pair, signed by the root.
type FolderKey struct {
	FolderID       string
	Version        int
	PublicKey      []byte
	Signature      []byte
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
	Note         []byte // the person's note, locked with a key from their private key
	HeldBy       int    // how many of the person's signed-in devices have it sealed for them
	RootSealed   []byte // the newest root's private key, sealed for the person
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
	err := d.pool.QueryRowContext(ctx, `SELECT u.public_key, u.password_lock, u.note, dv.public_key, pk.sealed,
			(SELECT COUNT(*) FROM person_keys p JOIN devices o ON o.id = p.device_id WHERE o.user_id = u.id AND o.revoked_at IS NULL),
			(SELECT sealed FROM root_grants WHERE user_id = u.id)
		FROM users u JOIN devices dv ON dv.user_id = u.id LEFT JOIN person_keys pk ON pk.device_id = dv.id
		WHERE u.id = $1 AND dv.id = $2`, userID, deviceID).Scan(&k.PersonPublic, &k.PasswordLock, &k.Note, &k.DevicePublic, &k.PersonSealed, &k.HeldBy, &k.RootSealed)
	if noRow(err) {
		return k, ErrNotFound
	}
	if err != nil {
		return k, err
	}
	k.Folders, err = queryAll(ctx, d.pool, func(row scanner) (SealedFolderKey, error) {
		var f SealedFolderKey
		err := row.Scan(&f.FolderID, &f.Version, &f.PublicKey, &f.Signature, &f.RecoverySealed, &f.CreatedBy, &f.CreatedAt, &f.Sealed)
		return f, err
	}, `SELECT k.folder_id, k.version, k.public_key, k.signature, k.recovery_sealed, k.created_by, k.created_at, g.sealed
		FROM folder_keys k JOIN users u ON u.id = $1
		LEFT JOIN folder_grants g ON g.folder_id = k.folder_id AND g.version = k.version AND g.user_id = u.id
		WHERE `+sees("k.folder_id")+` ORDER BY k.folder_id, k.version`, userID)
	return k, err
}

// Todo is what a person's device can seal for others with the keys the person holds.
type Todo struct {
	Devices  []DeviceKey     // the person's devices that lack the person's key
	People   []PersonNeed    // people who see a folder and lack a version of its key; for admins only
	Roots    []RootNeed      // admins who lack the newest root's private key; for those who hold it
	Recovery []FolderVersion // versions of folder keys not yet sealed for the recovery key
	Rekey    []string        // encrypted folders whose key needs a new version; for those who hold the root
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

// RootNeed is an admin whose person lacks the newest root's private key.
type RootNeed struct {
	UserID    string
	Name      string
	PublicKey []byte
	Active    bool // a device of theirs that holds their key was seen within CheckLife
}

type FolderVersion struct {
	FolderID string
	Version  int
}

// PinNeed is a version of a folder's key to lock for a PIN link: its secret is locked with a key
// from the folder key's version SecretVersion.
type PinNeed struct {
	PinID         string
	FolderID      string
	Version       int
	SecretVersion int
	SecretLocked  []byte
}

// TodoOf lists what a person can seal for others, given the versions of folder keys sealed
// for them: only those can they seal on. Only admins pass folder keys on to other people, after
// a check, so only they get the people who lack them; and only those who hold the root's private
// key pass it on, to other admins, and make new versions of folder keys, which it signs. A device
// whose check has its confirmation waits for nobody: it takes the person's key from there.
func (d *DB) TodoOf(ctx context.Context, userID string, admin bool, now time.Time) (Todo, error) {
	var t Todo
	var roots, holdsRoot bool
	if err := d.pool.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM roots), EXISTS (SELECT 1 FROM root_grants WHERE user_id = $1)",
		userID).Scan(&roots, &holdsRoot); err != nil {
		return t, err
	}
	// Seen lately: a device is touched every 10 minutes while it is used (auth's touchEvery).
	seen := now.Add(-CheckLife)
	err := eachRow(ctx, d.pool, `SELECT id, public_key, name, client, created_at, last_seen_at > $2 FROM devices WHERE user_id = $1
		AND revoked_at IS NULL AND public_key IS NOT NULL AND id NOT IN (SELECT device_id FROM person_keys)
		AND NOT EXISTS (SELECT 1 FROM key_checks c WHERE c.device_id = devices.id AND c.confirmation IS NOT NULL AND c.created_at > $2)
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
	if admin && holdsRoot {
		err = eachRow(ctx, d.pool, `SELECT u.id, u.name, u.public_key, EXISTS (SELECT 1 FROM devices v
				JOIN person_keys k ON k.device_id = v.id WHERE v.user_id = u.id AND v.revoked_at IS NULL AND v.last_seen_at > $2)
			FROM users u WHERE u.role = 'admin' AND u.id <> $1 AND u.public_key IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM root_grants g WHERE g.user_id = u.id)
			ORDER BY u.id`, []any{userID, seen}, func(row scanner) error {
			var n RootNeed
			if err := row.Scan(&n.UserID, &n.Name, &n.PublicKey, &n.Active); err != nil {
				return err
			}
			t.Roots = append(t.Roots, n)
			return nil
		})
		if err != nil {
			return t, err
		}
	}
	if roots {
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
	if holdsRoot {
		// A folder switched off gets its new version when it is turned on again.
		err = eachRow(ctx, d.pool, `SELECT f.id FROM folders f WHERE f.rekey AND f.encrypted AND f.deleted_at IS NULL AND EXISTS (
				SELECT 1 FROM folder_grants g WHERE g.folder_id = f.id AND g.user_id = $1
				AND g.version = (SELECT MAX(version) FROM folder_keys WHERE folder_id = f.id))
			ORDER BY f.created_at, f.id`, []any{userID}, func(row scanner) error {
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
	}
	err = eachRow(ctx, d.pool, `SELECT p.id, p.folder_id, g.version, p.secret_version, p.secret_locked
		FROM pins p JOIN folder_grants g ON g.folder_id = p.folder_id AND g.user_id = $1
		WHERE p.secret_locked IS NOT NULL AND p.ended_at IS NULL AND (p.expires_at IS NULL OR p.expires_at > $2)
		AND NOT EXISTS (SELECT 1 FROM pin_keys k WHERE k.pin_id = p.id AND k.version = g.version)
		AND EXISTS (SELECT 1 FROM folder_grants s WHERE s.folder_id = p.folder_id AND s.version = p.secret_version AND s.user_id = g.user_id)
		ORDER BY p.id, g.version`, []any{userID, now}, func(row scanner) error {
		var n PinNeed
		if err := row.Scan(&n.PinID, &n.FolderID, &n.Version, &n.SecretVersion, &n.SecretLocked); err != nil {
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
// for the old one goes, and their note with it, and their other devices and folders wait for
// someone to seal again.
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
			"DELETE FROM root_grants WHERE user_id = $1",
			"DELETE FROM person_keys WHERE device_id IN (SELECT id FROM devices WHERE user_id = $1)",
			"UPDATE users SET password_lock = NULL, note = NULL WHERE id = $1",
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

// SetNote keeps a person's note, locked with a key from their private key; ErrNoKey for a
// person without a key.
func (d *DB) SetNote(ctx context.Context, userID string, note []byte) error {
	res, err := d.pool.ExecContext(ctx, "UPDATE users SET note = $1 WHERE id = $2 AND public_key IS NOT NULL", note, userID)
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

// GrantRoot keeps the newest root's private key sealed for an admin who has a key, given by
// themselves (who made the root, or opened it with the recovery code) or by an admin who holds
// it; else ErrNotFound. One already sealed for them stays.
func (d *DB) GrantRoot(ctx context.Context, giverID, userID string, sealed []byte, now time.Time) error {
	return d.grant(ctx, `SELECT COUNT(*) FROM users u WHERE u.id = $1 AND u.role = 'admin' AND u.public_key IS NOT NULL
		AND EXISTS (SELECT 1 FROM roots) AND ($1 = $2 OR EXISTS (SELECT 1 FROM users g JOIN root_grants r ON r.user_id = g.id
			WHERE g.id = $2 AND g.role = 'admin'))`,
		[]any{userID, giverID},
		"INSERT INTO root_grants (user_id, sealed, created_at) VALUES ($1, $2, $3)", userID, sealed, now)
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
		WHERE p.id = $3 AND p.secret_locked IS NOT NULL`, []any{version, giverID, pinID},
		"INSERT INTO pin_keys (pin_id, version, locked) VALUES ($1, $2, $3)", pinID, version, locked)
}

// NewFolderKey is a new version of a folder's key, made on someone's device, signed by the root,
// with its private key sealed for them and for the recovery key.
type NewFolderKey struct {
	FolderID       string
	Version        int
	PublicKey      []byte
	Signature      []byte
	Sealed         []byte // for its maker
	RecoverySealed []byte
	By             string // its maker's user id
}

// newestRoot is the root that signs; ErrNoKey without one.
func newestRoot(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) ([]byte, error) {
	var public []byte
	err := q.QueryRowContext(ctx, "SELECT public_key FROM roots ORDER BY seq DESC LIMIT 1").Scan(&public)
	if noRow(err) {
		return nil, ErrNoKey
	}
	return public, err
}

func insertFolderKey(ctx context.Context, tx *sql.Tx, k NewFolderKey, now time.Time) error {
	root, err := newestRoot(ctx, tx)
	if err != nil {
		return err
	}
	if !e2ee.Verify(root, e2ee.FolderKeyMessage(k.FolderID, k.Version, k.PublicKey), k.Signature) {
		return ErrBadSignature
	}
	var newest int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM folder_keys WHERE folder_id = $1", k.FolderID).Scan(&newest); err != nil {
		return err
	}
	if k.Version != newest+1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO folder_keys (folder_id, version, public_key, signature, recovery_sealed, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, k.FolderID, k.Version, k.PublicKey, k.Signature, k.RecoverySealed, k.By, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO folder_grants (folder_id, version, user_id, sealed, created_at) VALUES ($1, $2, $3, $4, $5)",
		k.FolderID, k.Version, k.By, k.Sealed, now); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE folders SET rekey = $1 WHERE id = $2", false, k.FolderID)
	return err
}

// EncryptFolder makes a folder's new files encrypted, with the next version of its key, or
// plain again, with the root's plain statement for its newest version and its name. A new
// version every time encryption is turned on keeps an older plain statement from counting
// again. ErrConflict when key is missing for on, or comes for off, or isn't the next version;
// ErrBadSignature when a signature doesn't verify; ErrNoKey without a root.
func (d *DB) EncryptFolder(ctx context.Context, folderID string, on bool, key *NewFolderKey, plain []byte, now time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		f, err := scanFolder(tx.QueryRowContext(ctx, "SELECT "+folderSelect+" FROM folders WHERE id = $1 AND deleted_at IS NULL", folderID))
		if err != nil {
			return err
		}
		if on != (key != nil) {
			return ErrConflict
		}
		if on {
			if err := insertFolderKey(ctx, tx, *key, now); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "UPDATE folders SET encrypted = $1, plain_signature = NULL WHERE id = $2", true, folderID)
			return err
		}
		if err := checkPlain(ctx, tx, f.ID, f.KeyVersion, f.Name, plain); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE folders SET encrypted = $1, plain_signature = $2 WHERE id = $3", false, plain, folderID)
		return err
	})
}

// checkPlain checks a folder's plain statement for its newest version and its name: under the
// newest root, or none while there is no root.
func checkPlain(ctx context.Context, tx *sql.Tx, folderID string, version int, name string, signature []byte) error {
	root, err := newestRoot(ctx, tx)
	switch {
	case errors.Is(err, ErrNoKey):
		if signature != nil {
			return ErrBadSignature
		}
		return nil
	case err != nil:
		return err
	case !e2ee.Verify(root, e2ee.PlainMessage(folderID, version, name), signature):
		return ErrBadSignature
	}
	return nil
}

// AddFolderKey records the next version of a folder's key; ErrConflict unless it is the next
// one.
func (d *DB) AddFolderKey(ctx context.Context, key NewFolderKey, now time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error { return insertFolderKey(ctx, tx, key, now) })
}

// FolderKeyOf returns a version of a folder's key.
func (d *DB) FolderKeyOf(ctx context.Context, folderID string, version int) (FolderKey, error) {
	k := FolderKey{FolderID: folderID, Version: version}
	err := d.pool.QueryRowContext(ctx, "SELECT public_key, signature, recovery_sealed, created_by, created_at FROM folder_keys WHERE folder_id = $1 AND version = $2",
		folderID, version).Scan(&k.PublicKey, &k.Signature, &k.RecoverySealed, &k.CreatedBy, &k.CreatedAt)
	if noRow(err) {
		return k, ErrNoKey
	}
	return k, err
}

// RecoverableKeys lists every version of every folder's key that is sealed for the recovery
// key.
func (d *DB) RecoverableKeys(ctx context.Context) ([]FolderKey, error) {
	var out []FolderKey
	err := eachRow(ctx, d.pool, `SELECT folder_id, version, public_key, signature, recovery_sealed, created_by, created_at FROM folder_keys
		WHERE recovery_sealed IS NOT NULL ORDER BY folder_id, version`, nil, func(row scanner) error {
		var k FolderKey
		if err := row.Scan(&k.FolderID, &k.Version, &k.PublicKey, &k.Signature, &k.RecoverySealed, &k.CreatedBy, &k.CreatedAt); err != nil {
			return err
		}
		out = append(out, k)
		return nil
	})
	return out, err
}

// The setting for new folders lives in meta, as "1".
const metaNewEncrypted = "e2ee_new_folders_encrypted"

// Root is a root key, which is the recovery key: its public key, signed by the one before (nil
// for the first), and its private key locked with its recovery code (nil once a newer one came).
type Root struct {
	Seq       int
	PublicKey []byte
	Signature []byte
	Locked    []byte
}

// Roots lists the chain of roots, the first first; none before the first recovery key.
func (d *DB) Roots(ctx context.Context) ([]Root, error) {
	return queryAll(ctx, d.pool, func(row scanner) (Root, error) {
		var r Root
		err := row.Scan(&r.Seq, &r.PublicKey, &r.Signature, &r.Locked)
		return r, err
	}, "SELECT seq, public_key, signature, locked FROM roots ORDER BY seq")
}

// RecoveryKey returns the newest root's public key and its private key locked with the
// recovery code; nil without one.
func (d *DB) RecoveryKey(ctx context.Context) (public, locked []byte, err error) {
	err = d.pool.QueryRowContext(ctx, "SELECT public_key, locked FROM roots ORDER BY seq DESC LIMIT 1").Scan(&public, &locked)
	if noRow(err) {
		return nil, nil, nil
	}
	return public, locked, err
}

// PlainFolder is a folder's plain statement: its newest key version and its name, which the
// root signs, and the signature it has (nil while there is none).
type PlainFolder struct {
	FolderID  string
	Version   int
	Name      string
	Signature []byte
}

// ToSign is what a new root signs anew: every version of every folder's key, and every plain
// statement, with what the root before it signed.
type ToSign struct {
	FolderKeys []FolderKey
	Plain      []PlainFolder
}

// ToSign lists what a new root must sign: before the first, every folder as plain, deleted or
// not (none can have keys yet); after it, every version of every folder's key and every folder
// that sends plain under the root's signature.
func (d *DB) ToSign(ctx context.Context) (ToSign, error) {
	var first bool
	if err := d.pool.QueryRowContext(ctx, "SELECT NOT EXISTS (SELECT 1 FROM roots)").Scan(&first); err != nil {
		return ToSign{}, err
	}
	return toSign(ctx, d.pool, first)
}

func toSign(ctx context.Context, q querier, first bool) (ToSign, error) {
	var t ToSign
	var err error
	t.FolderKeys, err = queryAll(ctx, q, func(row scanner) (FolderKey, error) {
		var k FolderKey
		err := row.Scan(&k.FolderID, &k.Version, &k.PublicKey, &k.Signature, &k.CreatedBy, &k.CreatedAt)
		return k, err
	}, "SELECT folder_id, version, public_key, signature, created_by, created_at FROM folder_keys ORDER BY folder_id, version")
	if err != nil {
		return t, err
	}
	t.Plain, err = queryAll(ctx, q, func(row scanner) (PlainFolder, error) {
		var f PlainFolder
		err := row.Scan(&f.FolderID, &f.Version, &f.Name, &f.Signature)
		return f, err
	}, `SELECT f.id, COALESCE((SELECT MAX(version) FROM folder_keys WHERE folder_id = f.id), 0), f.name, f.plain_signature
		FROM folders f WHERE $1 OR (f.plain_signature IS NOT NULL AND NOT f.encrypted) ORDER BY f.id`, first)
	return t, err
}

// Resigned is a new root's signature of a version of a folder's key, or of a folder's plain
// statement (Version unused).
type Resigned struct {
	FolderID  string
	Version   int
	Signature []byte
}

// NewRoot is a new root, made on an admin's device: signed by the newest root before it (nil for
// the first), its private key locked with the new recovery code and sealed for the admin's person
// key, with everything ToSign lists signed anew.
type NewRoot struct {
	PublicKey  []byte
	Signature  []byte
	Locked     []byte
	Sealed     []byte
	By         string
	FolderKeys []Resigned
	Plain      []Resigned
}

// AddRoot records a new root, which signs from now on: the older ones' private keys and what
// was sealed for them go, and so does the root's private key sealed for other admins, who get the
// new one from an admin who holds it. ErrBadSignature for a signature that doesn't verify,
// ErrConflict when what was signed isn't all that ToSign lists now, ErrNoKey for an admin
// without a key, ErrNotFound for someone who isn't an admin.
func (d *DB) AddRoot(ctx context.Context, r NewRoot, now time.Time) error {
	return d.inTx(ctx, func(tx *sql.Tx) error {
		var seq int
		var prev []byte
		err := tx.QueryRowContext(ctx, "SELECT seq, public_key FROM roots ORDER BY seq DESC LIMIT 1").Scan(&seq, &prev)
		first := noRow(err)
		if err != nil && !first {
			return err
		}
		if e2ee.CheckPublicKey(r.PublicKey) != nil || first != (r.Signature == nil) ||
			(!first && !e2ee.Verify(prev, e2ee.RootMessage(r.PublicKey), r.Signature)) {
			return ErrBadSignature
		}
		var person []byte
		err = tx.QueryRowContext(ctx, "SELECT public_key FROM users WHERE id = $1 AND role = 'admin'", r.By).Scan(&person)
		if noRow(err) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if person == nil {
			return ErrNoKey
		}
		want, err := toSign(ctx, tx, first)
		if err != nil {
			return err
		}
		keys := map[FolderVersion][]byte{}
		for _, k := range r.FolderKeys {
			keys[FolderVersion{k.FolderID, k.Version}] = k.Signature
		}
		plain := map[string][]byte{}
		for _, p := range r.Plain {
			plain[p.FolderID] = p.Signature
		}
		if len(keys) != len(want.FolderKeys) || len(plain) != len(want.Plain) {
			return ErrConflict
		}
		for _, k := range want.FolderKeys {
			sig, ok := keys[FolderVersion{k.FolderID, k.Version}]
			if !ok {
				return ErrConflict
			}
			if !e2ee.Verify(r.PublicKey, e2ee.FolderKeyMessage(k.FolderID, k.Version, k.PublicKey), sig) {
				return ErrBadSignature
			}
			if _, err := tx.ExecContext(ctx, "UPDATE folder_keys SET signature = $1, recovery_sealed = NULL WHERE folder_id = $2 AND version = $3",
				sig, k.FolderID, k.Version); err != nil {
				return err
			}
		}
		for _, f := range want.Plain {
			sig, ok := plain[f.FolderID]
			if !ok {
				return ErrConflict
			}
			if !e2ee.Verify(r.PublicKey, e2ee.PlainMessage(f.FolderID, f.Version, f.Name), sig) {
				return ErrBadSignature
			}
			if _, err := tx.ExecContext(ctx, "UPDATE folders SET plain_signature = $1 WHERE id = $2", sig, f.FolderID); err != nil {
				return err
			}
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"UPDATE roots SET locked = NULL", nil},
			{"INSERT INTO roots (seq, public_key, signature, locked, created_at) VALUES ($1, $2, $3, $4, $5)", []any{seq + 1, r.PublicKey, r.Signature, r.Locked, now}},
			{"DELETE FROM root_grants", nil},
			{"INSERT INTO root_grants (user_id, sealed, created_at) VALUES ($1, $2, $3)", []any{r.By, r.Sealed, now}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		return nil
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
	err := d.pool.QueryRowContext(ctx, "SELECT secret_locked, secret_version, root_locked FROM pins WHERE id = $1", pinID).Scan(&s.Locked, &version, &s.Root)
	if noRow(err) {
		return nil, ErrNotFound
	}
	if err != nil || s.Locked == nil {
		return nil, err
	}
	s.Version = int(version.Int64)
	if s.Keys, err = d.PinKeys(ctx, pinID); err != nil {
		return nil, err
	}
	return &s, nil
}
