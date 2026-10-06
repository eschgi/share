package db

import (
	"context"
	"strings"
	"time"
)

// LibraryFilter narrows what the library shows.
type LibraryFilter struct {
	Folders []string // the folders to show files of; none shows nothing
	Kind    string   // photo, video or document; empty for all
	Query   string   // part of the file name, not case-sensitive
	Day     string   // YYYY-MM-DD; empty for all days
}

func (f LibraryFilter) where() (string, []any) {
	w := "state = 'ready'"
	var args []any
	switch len(f.Folders) {
	case 0:
		w += " AND FALSE"
	case 1:
		w += " AND folder_id = ?"
		args = append(args, f.Folders[0])
	default:
		w += " AND folder_id IN (?" + strings.Repeat(", ?", len(f.Folders)-1) + ")"
		for _, id := range f.Folders {
			args = append(args, id)
		}
	}
	if f.Kind != "" {
		w += " AND kind = ?"
		args = append(args, f.Kind)
	}
	if f.Query != "" {
		// ILIKE lowers both sides by the column's collation: every letter.
		w += ` AND name ILIKE ? ESCAPE '\'`
		args = append(args, "%"+likeEscaper.Replace(f.Query)+"%")
	}
	if f.Day != "" {
		w += " AND upload_day = ?"
		args = append(args, f.Day)
	}
	return w, args
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// DaySummary is one upload day in the library.
type DaySummary struct {
	Day   string
	Count int
	Bytes int64
}

// LibraryDays lists the upload days that have files, newest first.
func (d *DB) LibraryDays(ctx context.Context, f LibraryFilter) ([]DaySummary, error) {
	w, args := f.where()
	rows, err := d.QueryContext(ctx, "SELECT upload_day, COUNT(*), SUM(size) FROM files WHERE "+w+
		" GROUP BY upload_day ORDER BY upload_day DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DaySummary
	for rows.Next() {
		var s DaySummary
		if err := rows.Scan(&s.Day, &s.Count, &s.Bytes); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Position is where a page of the library ends, to continue after it.
type Position struct {
	UploadedAt time.Time
	ID         string
}

// LibraryFiles lists files newest first, at most limit, after the given position.
func (d *DB) LibraryFiles(ctx context.Context, f LibraryFilter, after *Position, limit int) ([]File, error) {
	w, args := f.where()
	if after != nil {
		// The same order as the files_ready_by_time index: newest first, then by id.
		w += " AND (uploaded_at < ? OR (uploaded_at = ? AND id > ?))"
		args = append(args, ms(after.UploadedAt), ms(after.UploadedAt), after.ID)
	}
	args = append(args, limit)
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE "+w+" ORDER BY uploaded_at DESC, id LIMIT ?", args...)
}

// LibraryIDs returns the ids of all matching files, newest first, and their total size.
func (d *DB) LibraryIDs(ctx context.Context, f LibraryFilter) ([]string, int64, error) {
	w, args := f.where()
	rows, err := d.QueryContext(ctx, "SELECT id, size FROM files WHERE "+w+" ORDER BY uploaded_at DESC, id", args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	ids := []string{}
	var total int64
	for rows.Next() {
		var id string
		var size int64
		if err := rows.Scan(&id, &size); err != nil {
			return nil, 0, err
		}
		ids = append(ids, id)
		total += size
	}
	return ids, total, rows.Err()
}
