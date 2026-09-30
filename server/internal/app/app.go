// Package app wires the server together and runs it.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/api"
	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/jobs"
	"github.com/eschgi/share/server/internal/localtls"
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
	Local   *localtls.Loader // nil without a local address
	Handler http.Handler
	// LocalHandler answers on the local address; nil without one.
	LocalHandler http.Handler

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
	lib.OnPurged = th.Remove
	ui := webui.New(cfg)
	if !ui.Built() {
		log.Printf("webui: the website isn't built into this binary; serving a placeholder")
	}
	var local *localtls.Loader
	if cfg.Local.Listen != "" {
		host := cfg.Local.URLHost()
		if err := localtls.Ensure(cfg.DataDir, host); err != nil {
			lib.Close()
			d.Close()
			return nil, fmt.Errorf("local address certificate: %w", err)
		}
		if local, err = localtls.NewLoader(cfg.DataDir); err != nil {
			lib.Close()
			d.Close()
			return nil, fmt.Errorf("local address certificate: %w", err)
		}
	}
	apiHandlers := &api.API{
		Cfg: cfg, Auth: authSvc, ServerID: serverID, MaxFileSize: maxFile, Lib: lib, Thumbs: th, Local: local,
		APK: &api.APK{Path: cfg.App.APKFile}, Now: now,
	}

	mux := http.NewServeMux()
	mux.Handle(upload.BasePath, up)
	apiHandlers.Register(mux)
	mux.Handle("/", ui)

	a := &App{
		Cfg: cfg, DB: d, Lib: lib, Auth: authSvc, Upload: up, Thumbs: th, UI: ui, Local: local,
		// Browsers may only change state from this site itself; the app sends no Origin.
		Handler: http.NewCrossOriginProtection().Handler(mux),
		now:     now,
	}
	if local != nil {
		// The local address is for the app: the API and uploads, not the website.
		localMux := http.NewServeMux()
		localMux.Handle(upload.BasePath, up)
		apiHandlers.Register(localMux)
		a.LocalHandler = http.NewCrossOriginProtection().Handler(localMux)
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
			if _, err := d.DeleteStalePinSessions(ctx, now().Add(-30*24*time.Hour)); err != nil {
				return err
			}
			_, err := d.DeleteOldInvites(ctx, now().Add(-30*24*time.Hour))
			return err
		}},
		{Name: "empty the trash", Every: 24 * time.Hour, Run: func(ctx context.Context) error {
			n, err := lib.PurgeOld(ctx, cfg.TrashDays)
			if n > 0 {
				log.Printf("storage: removed %d files deleted more than %d days ago", n, cfg.TrashDays)
			}
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
	if token, err := a.Auth.FirstStartInvite(ctx); err != nil {
		log.Printf("share: first-start invite: %v", err)
	} else if token != "" {
		log.Printf("share: nobody has an account yet. To become the admin, open this link on your phone; it works once, for 7 days:")
		log.Printf("share:   %s/join#%s", strings.TrimSuffix(a.Cfg.PublicURL, "/"), token)
		log.Printf("share: or make an invite with your name: share invite --admin --name YOURNAME")
	}

	main := newServer(a.Cfg.Listen, a.Handler)
	servers := []*http.Server{main}
	var local *http.Server
	if a.LocalHandler != nil {
		local = newServer(a.Cfg.Local.Listen, a.LocalHandler)
		local.TLSConfig = &tls.Config{GetCertificate: a.Local.GetCertificate, MinVersion: tls.VersionTLS12}
		servers = append(servers, local)
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

	errCh := make(chan error, len(servers))
	go func() {
		log.Printf("share: listening on %s for %s", a.Cfg.Listen, a.Cfg.PublicURL)
		if a.Cfg.TLSCertFile != "" {
			errCh <- main.ListenAndServeTLS(a.Cfg.TLSCertFile, a.Cfg.TLSKeyFile)
		} else {
			errCh <- main.ListenAndServe()
		}
	}()
	if local != nil {
		go func() {
			log.Printf("share: local address %s on %s, certificate %s", a.Cfg.Local.URL, a.Cfg.Local.Listen, a.Local.Fingerprint())
			errCh <- local.ListenAndServeTLS("", "")
		}()
	}
	var err error
	select {
	case err = <-errCh: // one of them couldn't start: stop the other too
	case <-ctx.Done():
		log.Printf("share: shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, s := range servers {
		if serr := s.Shutdown(shutdownCtx); err == nil {
			err = serr
		}
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}

func newServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
	}
}
