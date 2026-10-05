package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/storage"
)

// checkDatabase says where Share keeps its records, for `share check`; PostgreSQL is asked
// whether it answers and lets Share in.
func checkDatabase(cfg *config.Config) error {
	if cfg.Database == nil {
		fmt.Printf("Database:      SQLite in %s\n", storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}.DBPath())
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := db.OpenPostgres(ctx, cfg.Database.Postgres)
	if err != nil {
		fmt.Printf("Database:      %s\nProblem: %v\n", cfg.Database, err)
		return errors.New("fix the problems above")
	}
	defer d.Close()
	var version string
	if err := d.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		fmt.Printf("Database:      %s\nProblem: %v\n", cfg.Database, err)
		return errors.New("fix the problems above")
	}
	fmt.Printf("Database:      %s, version %s\n", cfg.Database, version)
	return nil
}
