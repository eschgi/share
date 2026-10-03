package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Folder is a part of the library with its own people. On the drive it is a directory in the
// storage folder, with the day folders inside.
type Folder struct {
	ID   string
	Name string
	Dir  string // the directory in storage_dir; "" is storage_dir itself
	// RenamingFrom is set while files may still lie under an older directory ("" is
	// storage_dir itself): after a rename, or for the day folders from before folders.
	RenamingFrom *string
	CreatedBy    string
	CreatedAt    time.Time
	DeletedAt    *time.Time
	DeletedBy    string
}

const folderColumns = "id, name, dir, renaming_from, created_by, created_at, deleted_at, deleted_by"

func scanFolder(row interface{ Scan(...any) error }) (Folder, error) {
	var f Folder
	var renaming, deletedBy sql.NullString
	var created int64
	var deleted sql.NullInt64
	err := row.Scan(&f.ID, &f.Name, &f.Dir, &renaming, &f.CreatedBy, &created, &deleted, &deletedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFound
	}
	if err != nil {
		return f, err
	}
	if renaming.Valid {
		f.RenamingFrom = &renaming.String
	}
	f.CreatedAt, f.DeletedAt, f.DeletedBy = fromMS(created), optTime(deleted), deletedBy.String
	return f, nil
}

func queryFolders(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) ([]Folder, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		f, err := scanFolder(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// EnsureFirstFolder makes the first folder if there is none yet, in one transaction: f gets
// every file and PIN there is, every member and every open invite for a new member, and dates
// from the oldest file. With files, it notes that their day folders still lie in storage_dir
// itself, to be moved into f's directory (RenamingFrom ""). It returns the oldest live folder
// and whether it made f. Once any folder exists it changes nothing, so the server and the
// command line can both call it.
func (d *DB) EnsureFirstFolder(ctx context.Context, f Folder, now time.Time) (Folder, bool, error) {
	made := false
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM folders").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return nil
		}
		var oldest sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT MIN(created_at) FROM files").Scan(&oldest); err != nil {
			return err
		}
		f.CreatedAt, f.RenamingFrom = now, nil
		if oldest.Valid {
			root := ""
			f.CreatedAt, f.RenamingFrom = fromMS(oldest.Int64), &root
		}
		if err := insertFolder(ctx, tx, f); err != nil {
			return err
		}
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{"UPDATE files SET folder_id = ? WHERE folder_id IS NULL", []any{f.ID}},
			{"UPDATE pins SET folder_id = ? WHERE folder_id IS NULL", []any{f.ID}},
			{"INSERT INTO folder_people (folder_id, user_id) SELECT ?, id FROM users WHERE role = 'member'", []any{f.ID}},
			{`INSERT INTO invite_folders (invite_id, folder_id) SELECT id, ? FROM invites
				WHERE user_id IS NULL AND role = 'member' AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`,
				[]any{f.ID, ms(now)}},
		} {
			if _, err := tx.ExecContext(ctx, q.sql, q.args...); err != nil {
				return err
			}
		}
		made = true
		return nil
	})
	if err != nil {
		return Folder{}, false, err
	}
	folders, err := d.LiveFolders(ctx)
	if err != nil {
		return Folder{}, false, err
	}
	if len(folders) == 0 {
		return Folder{}, false, errors.New("db: no folder left")
	}
	return folders[0], made, nil
}

// InsertFolder stores a new folder. It returns ErrConflict if a live folder has its name, or
// any folder its directory.
func (d *DB) InsertFolder(ctx context.Context, f Folder) error {
	return d.Tx(ctx, func(tx *sql.Tx) error {
		if err := insertFolder(ctx, tx, f); err != nil {
			return err
		}
		return bumpLibraryVersion(ctx, tx)
	})
}

func insertFolder(ctx context.Context, tx *sql.Tx, f Folder) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO folders ("+folderColumns+") VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		f.ID, f.Name, f.Dir, f.RenamingFrom, f.CreatedBy, ms(f.CreatedAt), nullMS(f.DeletedAt), nullString(f.DeletedBy))
	if isUniqueViolation(err) {
		return ErrConflict
	}
	return err
}

// FolderByID returns one folder, deleted or not.
func (d *DB) FolderByID(ctx context.Context, id string) (Folder, error) {
	return scanFolder(d.QueryRowContext(ctx, "SELECT "+folderColumns+" FROM folders WHERE id = ?", id))
}

// LiveFolders lists the folders that aren't deleted, the oldest first: the first one is where
// things go when nobody says.
func (d *DB) LiveFolders(ctx context.Context) ([]Folder, error) {
	return queryFolders(ctx, d, "SELECT "+folderColumns+" FROM folders WHERE deleted_at IS NULL ORDER BY created_at, rowid")
}

// FoldersOf lists the live folders a member was given, the oldest first. Admins see every
// folder whatever this says.
func (d *DB) FoldersOf(ctx context.Context, userID string) ([]Folder, error) {
	return queryFolders(ctx, d, "SELECT "+folderColumns+` FROM folders
		WHERE deleted_at IS NULL AND id IN (SELECT folder_id FROM folder_people WHERE user_id = ?) ORDER BY created_at, rowid`, userID)
}

// SetFolderPerson gives a person a folder, or takes it away. Doing it twice changes nothing.
func (d *DB) SetFolderPerson(ctx context.Context, folderID, userID string, sees bool) error {
	q := "INSERT OR IGNORE INTO folder_people (folder_id, user_id) VALUES (?, ?)"
	if !sees {
		q = "DELETE FROM folder_people WHERE folder_id = ? AND user_id = ?"
	}
	_, err := d.ExecContext(ctx, q, folderID, userID)
	return err
}

// DirTaken reports whether a folder, deleted or not, has a directory, ignoring case.
func (d *DB) DirTaken(ctx context.Context, dir string) (bool, error) {
	var n int
	err := d.QueryRowContext(ctx, "SELECT COUNT(*) FROM folders WHERE dir = ? COLLATE NOCASE", dir).Scan(&n)
	return n > 0, err
}

// Relocating lists the folders whose files may still lie under an older directory.
func (d *DB) Relocating(ctx context.Context) ([]Folder, error) {
	return queryFolders(ctx, d, "SELECT "+folderColumns+" FROM folders WHERE renaming_from IS NOT NULL ORDER BY created_at, rowid")
}

// FinishRelocation notes that nothing is left under a folder's older directory from.
func (d *DB) FinishRelocation(ctx context.Context, id, from string) error {
	_, err := d.ExecContext(ctx, "UPDATE folders SET renaming_from = NULL WHERE id = ? AND renaming_from = ?", id, from)
	return err
}

// FolderStat is what a folder holds: its files in the library, their bytes, and how many
// people and PIN sessions sent them.
type FolderStat struct {
	Files   int
	Bytes   int64
	Senders int
}

// FolderStats counts the library's files by folder.
func (d *DB) FolderStats(ctx context.Context) (map[string]FolderStat, error) {
	rows, err := d.QueryContext(ctx, `SELECT folder_id, COUNT(*), SUM(size), COUNT(DISTINCT COALESCE(user_id, 'pin:' || pin_session_id))
		FROM files WHERE state = 'ready' GROUP BY folder_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]FolderStat{}
	for rows.Next() {
		var id sql.NullString
		var s FolderStat
		if err := rows.Scan(&id, &s.Files, &s.Bytes, &s.Senders); err != nil {
			return nil, err
		}
		out[id.String] = s
	}
	return out, rows.Err()
}

// FolderCover is a folder's newest photo or video that has a thumbnail; ErrNotFound if it has
// none.
func (d *DB) FolderCover(ctx context.Context, folderID string) (File, error) {
	return scanFile(d.QueryRowContext(ctx, "SELECT "+fileColumns+` FROM files
		WHERE folder_id = ? AND state = 'ready' AND kind IN ('photo', 'video') AND thumb IN ('client', 'server')
		ORDER BY uploaded_at DESC, id LIMIT 1`, folderID))
}

// FolderPeople says who sees the folders: Members counts, by folder, the members who were
// given it and the open invites for new members that give it; admins and open invites for
// new admins see every folder, and Admins counts them.
type FolderPeople struct {
	Members map[string]int
	Admins  int
}

// CountFolderPeople counts who sees the folders.
func (d *DB) CountFolderPeople(ctx context.Context, now time.Time) (FolderPeople, error) {
	out := FolderPeople{Members: map[string]int{}}
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`SELECT fp.folder_id, COUNT(*) FROM folder_people fp JOIN users u ON u.id = fp.user_id
			WHERE u.role = 'member' GROUP BY fp.folder_id`, nil},
		{`SELECT i.folder_id, COUNT(*) FROM invite_folders i JOIN invites v ON v.id = i.invite_id
			WHERE v.user_id IS NULL AND v.role = 'member' AND v.used_at IS NULL AND v.revoked_at IS NULL AND v.expires_at > ?
			GROUP BY i.folder_id`, []any{ms(now)}},
	} {
		rows, err := d.QueryContext(ctx, q.sql, q.args...)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var id string
			var n int
			if err := rows.Scan(&id, &n); err != nil {
				rows.Close()
				return out, err
			}
			out.Members[id] += n
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return out, err
		}
	}
	err := d.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM users WHERE role = 'admin') +
		(SELECT COUNT(*) FROM invites WHERE user_id IS NULL AND role = 'admin' AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?)`,
		ms(now)).Scan(&out.Admins)
	return out, err
}
