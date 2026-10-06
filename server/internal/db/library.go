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

// where is the condition of the filter, with its arguments in p.
func (f LibraryFilter) where(p *params) string {
	w := "state = 'ready'"
	switch len(f.Folders) {
	case 0:
		w += " AND FALSE"
	case 1:
		w += " AND folder_id = " + p.add(f.Folders[0])
	default:
		w += " AND folder_id = ANY(" + p.add(f.Folders) + ")"
	}
	if f.Kind != "" {
		w += " AND kind = " + p.add(f.Kind)
	}
	if f.Query != "" {
		// ILIKE lowers both sides by the column's collation: every letter.
		w += " AND name ILIKE " + p.add("%"+likeEscaper.Replace(f.Query)+"%") + ` ESCAPE '\'`
	}
	if f.Day != "" {
		w += " AND upload_day = " + p.add(f.Day)
	}
	return w
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
	var p params
	rows, err := d.QueryContext(ctx, "SELECT upload_day, COUNT(*), SUM(size) FROM files WHERE "+f.where(&p)+
		" GROUP BY upload_day ORDER BY upload_day DESC", p...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DaySummary
	for rows.Next() {
		var s DaySummary
		var day time.Time
		if err := rows.Scan(&day, &s.Count, &s.Bytes); err != nil {
			return nil, err
		}
		s.Day = day.Format(time.DateOnly)
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
	var p params
	w := f.where(&p)
	if after != nil {
		// The same order as the files_ready_by_time index: newest first, then by id.
		at := p.add(after.UploadedAt)
		w += " AND (uploaded_at < " + at + " OR (uploaded_at = " + at + " AND id > " + p.add(after.ID) + "))"
	}
	return queryFiles(ctx, d, "SELECT "+fileColumns+" FROM files WHERE "+w+" ORDER BY uploaded_at DESC, id LIMIT "+p.add(limit), p...)
}

// LibraryIDs returns the ids of all matching files, newest first, and their total size.
func (d *DB) LibraryIDs(ctx context.Context, f LibraryFilter) ([]string, int64, error) {
	var p params
	rows, err := d.QueryContext(ctx, "SELECT id, size FROM files WHERE "+f.where(&p)+" ORDER BY uploaded_at DESC, id", p...)
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
