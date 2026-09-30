// Package app wires the server together and runs it.
package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/api"
	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/jobs"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/thumbs"
	"github.com/eschgi/share/server/internal/upload"
	"github.com/eschgi/share/server/internal/webui"
)

// Options changes how New builds the app; the zero value is what `share serve` uses.
type Options struct {
	Now            func() time.Time
	WaitForStorage bool // wait for the storage marker instead of failing (the drive may mount late)
	Upload         *upload.Config
}

// App is a running server's parts.
type App struct {
	Cfg     *config.Config
	DB      *db.DB
	Lib     *storage.Library
	Auth    *auth.Service
	Upload  *upload.Handler
	Thumbs  *thumbs.Store
	UI      *webui.UI
	Handler http.Handler

	now   func() time.Time
	sched *jobs.Scheduler
}

// New opens the storage and database and builds the handlers.
func New(ctx context.Context, cfg *config.Config, opts Options) (*App, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	layout := storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}
	if opts.WaitForStorage {
		if err := storage.WaitForMarker(ctx, cfg.StorageDir, log.Printf); err != nil {
			return nil, err
		}
	} else if _, err := os.Stat(filepath.Join(cfg.StorageDir, storage.MarkerName)); err != nil {
		return nil, fmt.Errorf("%s isn't a Share storage folder (no %s); mount the drive and run `share init`", cfg.StorageDir, storage.MarkerName)
	}
	report := storage.Check(layout)
	for _, p := range report.Problems {
		log.Printf("storage: problem: %s", p)
	}
	for _, w := range report.Warnings {
		log.Printf("storage: %s", w)
	}

	d, err := db.Open(layout.DBPath())
	if err != nil {
		return nil, err
	}
	if err := d.Migrate(ctx, layout.BackupDir()); err != nil {
		d.Close()
		return nil, err
	}
	serverID, err := d.ServerID(ctx)
	if err != nil {
		d.Close()
		return nil, err
	}
	lib, err := storage.OpenLibrary(d, layout, cfg.Location, now, log.Printf)
	if err != nil {
		d.Close()
		return nil, err
	}

	authSvc := auth.NewService(d, now, cfg.Proxies, cfg.ClientIPHeader)
	maxFile := cfg.MaxFileSize()
	if report.MaxFileSize > 0 && (maxFile == 0 || report.MaxFileSize < maxFile) {
		maxFile = report.MaxFileSize
	}
	upCfg := upload.DefaultConfig()
	if opts.Upload != nil {
		upCfg = *opts.Upload
	}
	upCfg.MaxFileSize, upCfg.MinFreeSpace = maxFile, cfg.MinFreeSpace()
	up, err := upload.New(upCfg, authSvc, lib, now)
	if err != nil {
		lib.Close()
		d.Close()
		return nil, err
	}
	th := &thumbs.Store{DB: d, Dir: layout.ThumbsDir(), Root: lib.Root(), Now: now, Logf: log.Printf}
	ui := webui.New(cfg)
	if !ui.Built() {
		log.Printf("webui: the website isn't built into this binary; serving a placeholder")
	}

	mux := http.NewServeMux()
	mux.Handle(upload.BasePath, up)
	(&api.API{Cfg: cfg, Auth: authSvc, ServerID: serverID, MaxFileSize: maxFile, Thumbs: th, Now: now}).Register(mux)
	mux.Handle("/", ui)

	a := &App{
		Cfg: cfg, DB: d, Lib: lib, Auth: authSvc, Upload: up, Thumbs: th, UI: ui,
		// Browsers may only change state from this site itself; the app sends no Origin.
		Handler: http.NewCrossOriginProtection().Handler(mux),
		now:     now,
	}
	a.sched = &jobs.Scheduler{Now: now, Logf: log.Printf, Tasks: []jobs.Task{
		{Name: "reconcile uploads", Every: 5 * time.Minute, Run: func(ctx context.Context) error {
			authSvc.PruneLimits()
			up.PruneQueues()
			return lib.Reconcile(ctx, cfg.IncompleteTTL())
		}},
		{Name: "expire PINs and sessions", Every: time.Hour, Run: func(ctx context.Context) error {
			if _, err := d.EndExpiredPins(ctx, now()); err != nil {
				return err
			}
			_, err := d.DeleteStalePinSessions(ctx, now().Add(-30*24*time.Hour))
			return err
		}},
	}}
	return a, nil
}

// Close releases the database and the storage folder.
func (a *App) Close() error {
	return errors.Join(a.Lib.Close(), a.DB.Close())
}

// Serve repairs what the last run left behind, then answers requests until ctx ends, and
// shuts down gracefully: running requests get 20 seconds.
func (a *App) Serve(ctx context.Context) error {
	if err := a.Lib.Reconcile(ctx, a.Cfg.IncompleteTTL()); err != nil {
		log.Printf("storage: reconcile: %v", err)
	}
	srv := &http.Server{
		Addr:              a.Cfg.Listen,
		Handler:           a.Handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
	// The background jobs stop with the server, and Serve waits for them, so none of them is
	// still using the database when the caller closes it.
	jobsCtx, stopJobs := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() {
		stopJobs()
		workers.Wait()
	}()
	workers.Go(func() { a.sched.Run(jobsCtx) })
	workers.Go(func() { a.Thumbs.Run(jobsCtx) })

	errCh := make(chan error, 1)
	go func() {
		log.Printf("share: listening on %s for %s", a.Cfg.Listen, a.Cfg.PublicURL)
		if a.Cfg.TLSCertFile != "" {
			errCh <- srv.ListenAndServeTLS(a.Cfg.TLSCertFile, a.Cfg.TLSKeyFile)
		} else {
			errCh <- srv.ListenAndServe()
		}
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	log.Printf("share: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
