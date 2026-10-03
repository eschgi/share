package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// PIN kinds.
const (
	PinPermanent = "permanent"
	PinDay       = "day"
)

// Pin is an upload PIN.
type Pin struct {
	ID          string
	Code        string
	Kind        string
	CreatedBy   string
	CreatedAt   time.Time
	ExpiresAt   *time.Time
	EndedAt     *time.Time
	FolderID    string // the folder it sends into; "" once that folder is gone for good
	ShowsFolder bool   // guests with it also see and download what is in the folder
}

// LiveAt reports whether the PIN still lets people send at time now.
func (p Pin) LiveAt(now time.Time) bool {
	return p.EndedAt == nil && (p.ExpiresAt == nil || now.Before(*p.ExpiresAt))
}

const pinColumns = "id, code, kind, created_by, created_at, expires_at, ended_at, folder_id, shows_folder"

func scanPin(row interface{ Scan(...any) error }) (Pin, error) {
	var p Pin
	var created int64
	var expires, ended sql.NullInt64
	var folderID sql.NullString
	err := row.Scan(&p.ID, &p.Code, &p.Kind, &p.CreatedBy, &created, &expires, &ended, &folderID, &p.ShowsFolder)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.CreatedAt = fromMS(created)
	p.ExpiresAt = optTime(expires)
	p.EndedAt = optTime(ended)
	p.FolderID = folderID.String
	return p, nil
}

// InsertPin stores a new PIN. It returns ErrConflict if the code was ever used before.
func (d *DB) InsertPin(ctx context.Context, p Pin) error {
	if p.FolderID == "" {
		return errors.New("db: a PIN needs a folder")
	}
	_, err := d.ExecContext(ctx, "INSERT INTO pins ("+pinColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		p.ID, p.Code, p.Kind, p.CreatedBy, ms(p.CreatedAt), nullMS(p.ExpiresAt), nullMS(p.EndedAt), p.FolderID, p.ShowsFolder)
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// PinByCode looks up a PIN by its code, live or not.
func (d *DB) PinByCode(ctx context.Context, code string) (Pin, error) {
	return scanPin(d.QueryRowContext(ctx, "SELECT "+pinColumns+" FROM pins WHERE code = ?", code))
}

// PinByID looks up a PIN by id.
func (d *DB) PinByID(ctx context.Context, id string) (Pin, error) {
	return scanPin(d.QueryRowContext(ctx, "SELECT "+pinColumns+" FROM pins WHERE id = ?", id))
}

// Pins lists every PIN, newest first.
func (d *DB) Pins(ctx context.Context) ([]Pin, error) {
	rows, err := d.QueryContext(ctx, "SELECT "+pinColumns+" FROM pins ORDER BY created_at DESC, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pin
	for rows.Next() {
		p, err := scanPin(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// EndPin ends a PIN. Its sessions stop working at once, because a session is only valid
// while its PIN is live. Ending an already ended PIN changes nothing.
func (d *DB) EndPin(ctx context.Context, id string, at time.Time) error {
	res, err := d.ExecContext(ctx, "UPDATE pins SET ended_at = ? WHERE id = ? AND ended_at IS NULL", ms(at), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := d.PinByID(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// EndExpiredPins marks 24-hour PINs whose time is up as ended, so lists show them as such.
// They stopped working at expires_at already; this only records it.
func (d *DB) EndExpiredPins(ctx context.Context, now time.Time) (int64, error) {
	res, err := d.ExecContext(ctx,
		"UPDATE pins SET ended_at = expires_at WHERE ended_at IS NULL AND expires_at IS NOT NULL AND expires_at <= ?", ms(now))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
