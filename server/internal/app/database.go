package app

import (
	"context"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/storage"
)

// OpenDatabase opens the database of cfg, brings its schema up to date, checks that it
// belongs to this storage and makes the first folder if there is none yet. The server and the
// commands that work on the database directly use it alike.
func OpenDatabase(ctx context.Context, cfg *config.Config, now time.Time) (*db.DB, error) {
	layout := storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}
	d, err := db.Open(layout.DBPath())
	if err != nil {
		return nil, err
	}
	if err := setUpDatabase(ctx, d, cfg, layout, now); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func setUpDatabase(ctx context.Context, d *db.DB, cfg *config.Config, layout storage.Layout, now time.Time) error {
	if err := d.Migrate(ctx, layout.BackupDir()); err != nil {
		return err
	}
	if err := storage.ClaimStorage(ctx, d, cfg.StorageKey()); err != nil {
		return err
	}
	_, err := storage.EnsureFirstFolder(ctx, d, cfg.StorageDir, cfg.Name, now)
	return err
}
