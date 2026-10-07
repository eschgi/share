package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
)

// checkDatabase asks PostgreSQL whether it answers and lets Share in, for `share check`.
func checkDatabase(cfg *config.Config) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	d, err := db.Open(ctx, cfg.Database.Postgres)
	if err != nil {
		fmt.Printf("Database:      %s\nProblem: %v\n", cfg.Database, err)
		return errors.New("fix the problems above")
	}
	defer d.Close()
	version, err := d.ServerVersion(ctx)
	if err != nil {
		fmt.Printf("Database:      %s\nProblem: %v\n", cfg.Database, err)
		return errors.New("fix the problems above")
	}
	fmt.Printf("Database:      %s, version %s\n", cfg.Database, version)
	return nil
}
