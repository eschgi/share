package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/eschgi/share/server/internal/db"
)

// folderNames is a --folder that may be given several times.
type folderNames []string

func (f *folderNames) String() string     { return strings.Join(*f, ", ") }
func (f *folderNames) Set(v string) error { *f = append(*f, v); return nil }

// findFolder finds a folder by name, ignoring case, or by id.
func findFolder(live []db.Folder, nameOrID string) (db.Folder, error) {
	for _, f := range live {
		if f.ID == nameOrID || strings.EqualFold(f.Name, nameOrID) {
			return f, nil
		}
	}
	return db.Folder{}, fmt.Errorf("no folder %q; see share folders", nameOrID)
}

// pickFolders turns --folder names into folder ids. Without any it takes the only folder, as
// long as there is just one.
func pickFolders(ctx context.Context, d *db.DB, names []string) ([]string, error) {
	live, err := d.LiveFolders(ctx)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		if len(live) == 1 {
			return []string{live[0].ID}, nil
		}
		return nil, errors.New("there are several folders: say which with --folder NAME (share folders lists them)")
	}
	var out []string
	for _, name := range names {
		f, err := findFolder(live, name)
		if err != nil {
			return nil, err
		}
		out = append(out, f.ID)
	}
	return out, nil
}

func folders(args []string) error {
	f := newFlags("folders")
	if _, err := f.parse(args); err != nil {
		return err
	}
	cfg, err := f.load()
	if err != nil {
		return err
	}
	ctx := context.Background()
	d, _, err := openDB(ctx, cfg)
	if err != nil {
		return err
	}
	defer d.Close()
	live, err := d.LiveFolders(ctx)
	if err != nil {
		return err
	}
	stats, err := d.FolderStats(ctx)
	if err != nil {
		return err
	}
	people, err := d.CountFolderPeople(ctx, time.Now())
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tFILES\tSIZE\tPEOPLE\tDIRECTORY")
	for _, folder := range live {
		s := stats[folder.ID]
		who := fmt.Sprint(people.Admins + people.Members[folder.ID])
		if people.Members[folder.ID] == 0 {
			who = "only admins"
		}
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%s\n", folder.Name, s.Files, gib(s.Bytes), who, folder.Dir)
	}
	return w.Flush()
}
