package db

import (
	"context"
	"strings"
	"time"
)

// How the library can be sorted (docs/sort-plan.md). Each groups the files differently: by the
// day they arrived, by the day they were taken, or by who sent them.
const (
	SortReceived = "received" // groups are upload days
	SortTaken    = "taken"    // groups are the days they were taken, else the days they arrived
	SortSender   = "sender"   // groups are the people who sent them, and "pin" for PIN guests
)

// SenderPIN is the group of the files that came with a PIN, whose senders have no name.
const SenderPIN = "pin"

// LibraryFilter narrows what the library shows.
type LibraryFilter struct {
	Folders []string // the folders to show files of; none shows nothing
	Kind    string   // photo, video or document; empty for all
	Query   string   // part of the file name, not case-sensitive
	Sort    string   // SortReceived (also when empty), SortTaken or SortSender
	Oldest  bool     // oldest first instead of newest first
	Group   string   // one group of Sort: a YYYY-MM-DD day, or a user id or SenderPIN; empty for all
}

// group is the SQL for the key of the group a file is in, as text.
func (f LibraryFilter) group() string {
	switch f.Sort {
	case SortTaken:
		return "COALESCE(taken_day, upload_day)::text"
	case SortSender:
		return "COALESCE(user_id::text, '" + SenderPIN + "')"
	default:
		return "upload_day::text"
	}
}

// time is the SQL for the time a file is ordered by inside its group.
func (f LibraryFilter) time() string {
	if f.Sort == SortTaken {
		return "COALESCE(taken_at, uploaded_at)"
	}
	return "uploaded_at"
}

// dir is the direction of the order.
func (f LibraryFilter) dir() string {
	if f.Oldest {
		return "ASC"
	}
	return "DESC"
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
	if f.Group != "" {
		w += " AND " + f.group() + " = " + p.add(f.Group)
	}
	return w
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// GroupSummary is one group of the library: a day, or a sender.
type GroupSummary struct {
	Key     string // a YYYY-MM-DD day, or a user id or SenderPIN
	Count   int
	Bytes   int64
	LastDay string // the upload day of its newest file, YYYY-MM-DD
}

// LibraryGroups lists the groups that have files, in the order of the filter: days newest or
// oldest first, senders by their newest file (newest first) or their oldest (oldest first).
func (d *DB) LibraryGroups(ctx context.Context, f LibraryFilter) ([]GroupSummary, error) {
	var p params
	order := f.group()
	if f.Sort == SortSender {
		order = "MAX(uploaded_at)"
		if f.Oldest {
			order = "MIN(uploaded_at)"
		}
	}
	return queryAll(ctx, d.pool, func(row scanner) (GroupSummary, error) {
		var s GroupSummary
		var last time.Time
		err := row.Scan(&s.Key, &s.Count, &s.Bytes, &last)
		s.LastDay = last.Format(time.DateOnly)
		return s, err
	}, "SELECT "+f.group()+" AS g, COUNT(*), SUM(size), MAX(upload_day) FROM files WHERE "+f.where(&p)+
		" GROUP BY g ORDER BY "+order+" "+f.dir()+", g "+f.dir(), p...)
}

// Position is where a page of the library ends, to continue after it: the place of its last file
// in the order of the filter.
type Position struct {
	GroupOrder string // what the groups are ordered by: the day, or the sender's newest or oldest upload
	Group      string
	Time       time.Time
	ID         string
}

// orderedFiles is the files of the filter with what they are ordered by: the group's order, the
// group, the time and the id, in that order, all in the filter's direction.
func (f LibraryFilter) orderedFiles(p *params) string {
	groupOrder := "g"
	if f.Sort == SortSender {
		agg := "MAX"
		if f.Oldest {
			agg = "MIN"
		}
		groupOrder = "to_char(" + agg + `(uploaded_at) OVER (PARTITION BY g) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS')`
	}
	return "SELECT *, " + groupOrder + " AS go FROM (SELECT " + fileColumns + ", " + f.group() + " AS g, " + f.time() +
		" AS t FROM files WHERE " + f.where(p) + ") m"
}

// LibraryFiles lists files in the order of the filter, at most limit, after the given position.
// Each comes with its Position, for the next page.
func (d *DB) LibraryFiles(ctx context.Context, f LibraryFilter, after *Position, limit int) ([]File, []Position, error) {
	var p params
	inner := f.orderedFiles(&p)
	w := "TRUE"
	if after != nil {
		cmp := "<"
		if f.Oldest {
			cmp = ">"
		}
		w = "(go, g, t, id) " + cmp + " (" + p.add(after.GroupOrder) + "::text, " + p.add(after.Group) + "::text, " +
			p.add(after.Time) + "::timestamptz, " + p.add(after.ID) + "::uuid)"
	}
	dir := f.dir()
	var positions []Position
	files, err := queryAll(ctx, d.pool, func(row scanner) (File, error) {
		var pos Position
		file, err := scanFile(withExtra{row, []any{&pos.Group, &pos.Time, &pos.GroupOrder}})
		pos.ID = file.ID
		positions = append(positions, pos)
		return file, err
	}, "SELECT "+fileColumns+", g, t, go FROM ("+inner+") o WHERE "+w+
		" ORDER BY go "+dir+", g "+dir+", t "+dir+", id "+dir+" LIMIT "+p.add(limit), p...)
	if err != nil {
		return nil, nil, err
	}
	return files, positions, nil
}

// withExtra scans a row's file columns, then the extra columns after them.
type withExtra struct {
	row   scanner
	extra []any
}

func (s withExtra) Scan(dest ...any) error { return s.row.Scan(append(dest, s.extra...)...) }

// LibraryIDs returns the ids of all matching files, newest first, and their total size.
func (d *DB) LibraryIDs(ctx context.Context, f LibraryFilter) ([]string, int64, error) {
	var p params
	ids := []string{} // so that the JSON says [] for none
	var total int64
	err := eachRow(ctx, d.pool, "SELECT id, size FROM files WHERE "+f.where(&p)+" ORDER BY uploaded_at DESC, id", p, func(row scanner) error {
		var id string
		var size int64
		err := row.Scan(&id, &size)
		ids, total = append(ids, id), total+size
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	return ids, total, nil
}
