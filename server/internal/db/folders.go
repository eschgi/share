package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// ErrLastFolder means the change would leave no folder to send into.
var ErrLastFolder = errors.New("the last folder can't go")

// ErrBusy means the folder's files are still being moved from an earlier rename.
var ErrBusy = errors.New("the folder's files are still being moved")

// RenameFolder gives a live folder a new name and, if dir differs from its directory, a new
// directory; the old one is noted until the files are moved over (FinishRelocation). It
// returns ErrNotFound for no such live folder, ErrConflict if a live folder has the name or
// any folder the directory, and ErrBusy while an earlier move isn't finished.
func (d *DB) RenameFolder(ctx context.Context, id, name, dir string) (Folder, error) {
	var out Folder
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		f, err := scanFolder(tx.QueryRowContext(ctx, "SELECT "+folderColumns+" FROM folders WHERE id = ? AND deleted_at IS NULL", id))
		if err != nil {
			return err
		}
		if dir != f.Dir {
			if f.RenamingFrom != nil {
				return ErrBusy
			}
			old := f.Dir
			f.RenamingFrom = &old
		}
		f.Name, f.Dir = name, dir
		if _, err := tx.ExecContext(ctx, "UPDATE folders SET name = ?, dir = ?, renaming_from = ? WHERE id = ?",
			f.Name, f.Dir, f.RenamingFrom, f.ID); err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return err
		}
		out = f
		return bumpLibraryVersion(ctx, tx)
	})
	return out, err
}

// DeleteFolder deletes a folder in one transaction: its files in the library go to the trash,
// its PINs end, and only Recently deleted still shows it. The files' bytes are moved to the
// trash afterwards (storage.Library.DeleteFolder). It returns the files it trashed;
// ErrLastFolder for the last live folder, ErrNotFound for no such live folder.
func (d *DB) DeleteFolder(ctx context.Context, id, by string, at time.Time) ([]File, error) {
	var out []File
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		var live int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM folders WHERE deleted_at IS NULL").Scan(&live); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "UPDATE folders SET deleted_at = ?, deleted_by = ? WHERE id = ? AND deleted_at IS NULL",
			ms(at), nullString(by), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if live <= 1 {
			return ErrLastFolder
		}
		files, err := queryFiles(ctx, tx, "SELECT "+fileColumns+" FROM files WHERE folder_id = ? AND state = 'ready'", id)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE files SET state = 'trashed', deleted_at = ?, deleted_by = ?, updated_at = ?
			WHERE folder_id = ? AND state = 'ready'`, ms(at), nullString(by), ms(at), id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE pins SET ended_at = ? WHERE folder_id = ? AND ended_at IS NULL", ms(at), id); err != nil {
			return err
		}
		for i := range files {
			files[i].State, files[i].DeletedAt, files[i].DeletedBy, files[i].UpdatedAt = StateTrashed, &at, by, at
		}
		out = files
		return bumpLibraryVersion(ctx, tx)
	})
	return out, err
}

// ReviveFolder brings a deleted folder back, with its people, when one of its files is
// restored. If a live folder has its name now, it gets a number: "Wedding (2)".
func (d *DB) ReviveFolder(ctx context.Context, id string) (Folder, error) {
	var out Folder
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		f, err := scanFolder(tx.QueryRowContext(ctx, "SELECT "+folderColumns+" FROM folders WHERE id = ?", id))
		if err != nil || f.DeletedAt == nil {
			out = f
			return err
		}
		for n := 1; n <= 1000; n++ {
			name := f.Name
			if n > 1 {
				name = numberedName(f.Name, n)
			}
			_, err := tx.ExecContext(ctx, "UPDATE folders SET name = ?, deleted_at = NULL, deleted_by = NULL WHERE id = ?", name, id)
			if isUniqueViolation(err) {
				continue
			}
			if err != nil {
				return err
			}
			f.Name, f.DeletedAt, f.DeletedBy = name, nil, ""
			out = f
			return bumpLibraryVersion(ctx, tx)
		}
		return fmt.Errorf("no free name for the folder %q", f.Name)
	})
	return out, err
}

// numberedName is the n-th choice for a taken folder name, still at most 60 characters.
func numberedName(name string, n int) string {
	suffix := fmt.Sprintf(" (%d)", n)
	r := []rune(name)
	if max := 60 - len([]rune(suffix)); len(r) > max {
		r = r[:max]
	}
	return strings.TrimSpace(string(r)) + suffix
}

// ReadyInDeletedFolders lists files in the library whose folder is deleted: uploads that
// finished after their folder went.
func (d *DB) ReadyInDeletedFolders(ctx context.Context) ([]File, error) {
	return queryFiles(ctx, d, "SELECT "+fileColumns+` FROM files
		WHERE state = 'ready' AND folder_id IN (SELECT id FROM folders WHERE deleted_at IS NOT NULL)`)
}

// DropEmptyDeletedFolders forgets the deleted folders that have no file left, not even in the
// trash, and returns them.
func (d *DB) DropEmptyDeletedFolders(ctx context.Context) ([]Folder, error) {
	var out []Folder
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = queryFolders(ctx, tx, "SELECT "+folderColumns+` FROM folders f WHERE deleted_at IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM files WHERE folder_id = f.id)
			AND NOT EXISTS (SELECT 1 FROM files WHERE moved_from LIKE f.id || '/%')`)
		if err != nil {
			return err
		}
		for _, f := range out {
			if _, err := tx.ExecContext(ctx, "DELETE FROM folders WHERE id = ?", f.ID); err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// FoldersByID returns the folders among ids, deleted or not.
func (d *DB) FoldersByID(ctx context.Context, ids []string) ([]Folder, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return queryFolders(ctx, d, "SELECT "+folderColumns+" FROM folders WHERE id IN (?"+strings.Repeat(", ?", len(ids)-1)+") ORDER BY created_at, rowid", args...)
}
