package app

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/storage"
)

// OpenDatabase opens the PostgreSQL database of cfg, brings its schema up to date, checks that
// it belongs to this storage and makes the first folder if there is none yet. The server and the
// commands that work on the database directly use it alike; the server waits while PostgreSQL
// doesn't answer.
func OpenDatabase(ctx context.Context, cfg *config.Config, now time.Time, wait bool) (*db.DB, error) {
	d, err := openPostgres(ctx, cfg.Database, wait)
	if err != nil {
		return nil, err
	}
	if err := setUpDatabase(ctx, d, cfg, now); err != nil {
		d.Close()
		return nil, err
	}
	return d, nil
}

func setUpDatabase(ctx context.Context, d *db.DB, cfg *config.Config, now time.Time) error {
	if err := d.Migrate(ctx); err != nil {
		return err
	}
	if err := storage.ClaimStorage(ctx, d, cfg.StorageKey()); err != nil {
		return err
	}
	_, err := storage.EnsureFirstFolder(ctx, d, cfg.StorageDir, cfg.Name, now)
	return err
}

// postgresRetry is how often the server asks again while PostgreSQL doesn't answer.
var postgresRetry = 5 * time.Second

// openPostgres opens the PostgreSQL database. When waiting, it asks again while nothing
// answers, as when a free database host wakes a paused one; a refused login or database, or a
// server Share can't use, ends it at once, since waiting won't change that.
func openPostgres(ctx context.Context, c config.Database, wait bool) (*db.DB, error) {
	start, lastLog := time.Now(), time.Time{}
	for {
		d, err := db.Open(ctx, c.Postgres)
		if err == nil || !wait || !db.Unreachable(err) {
			return d, err
		}
		if time.Since(lastLog) >= time.Minute {
			log.Printf("database: waiting for %s: %v", c, err)
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("gave up waiting for %s after %v: %w", c, time.Since(start).Round(time.Second), err)
		case <-time.After(postgresRetry):
		}
	}
}
