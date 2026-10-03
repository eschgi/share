package storage

import (
	"context"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// EnsureFirstFolder makes the first folder when there is none: after the upgrade to folders,
// or on a new server. It is named after the server and gets everything there is. Its directory
// is storage_dir itself, where the day folders from before folders are. It returns the oldest
// folder.
func EnsureFirstFolder(ctx context.Context, d *db.DB, name string, now time.Time) (db.Folder, error) {
	if name = FolderName(name); name == "" {
		name = "Share"
	}
	f := db.Folder{ID: ids.New(), Name: name, Dir: "", CreatedBy: "first-start"}
	folder, _, err := d.EnsureFirstFolder(ctx, f, now)
	return folder, err
}
