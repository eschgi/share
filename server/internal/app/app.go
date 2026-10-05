// Package app wires the server together and runs it.
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/api"
	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/checksum"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/downloads"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/jobs"
	"github.com/eschgi/share/server/internal/localtls"
	"github.com/eschgi/share/server/internal/s3"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/thumbs"
	"github.com/eschgi/share/server/internal/upload"
	"github.com/eschgi/share/server/internal/webui"
)

// Options changes how New builds the app; the zero value is what `share serve` uses.
type Options struct {
	Now            func() time.Time
	WaitForStorage bool // wait for the storage marker instead of failing (a drive may mount late)
	Upload         *upload.Config
	Version        string // the program's version, for the About screens; "dev" when empty
	// CheckStorage looks at the drives or the bucket for the admins' storage page;
	// storage.Check or storage.CheckS3 when nil.
	CheckStorage func(context.Context) storage.Report
	// S3 is the bucket of the "s3" setting, opened by a test against its own server.
	S3 *s3.Bucket
}

// App is a running server's parts.
type App struct {
	Cfg    *config.Config
	DB     *db.DB
	Lib    *storage.Library
	Auth   *auth.Service
	Tus    *upload.TusHandler // nil when the files are in a bucket
	S3     *s3.Bucket         // the bucket, nil when the files are on a drive
	Thumbs *thumbs.Store
	CRCs   *checksum.Store // nil in a bucket: a ZIP needs the files on a drive
	UI     *webui.UI
	Local  *localtls.Loader // Share's own certificate on the https port; nil without one
	// Handler answers on both ports, http and https.
	Handler http.Handler

	now   func() time.Time
	sched *jobs.Scheduler

	hintsMu sync.Mutex
	hints   map[string]time.Time // when each hint about the proxy was last logged
}

// New opens the storage and database and builds the handlers.
func New(ctx context.Context, cfg *config.Config, opts Options) (*App, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	layout := storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}
	var bucket *s3.Bucket
	var checkStorage func(context.Context) storage.Report
	if cfg.S3 != nil {
		if bucket = opts.S3; bucket == nil {
			var err error
			if bucket, err = s3.Open(cfg.S3, s3.Options{}); err != nil {
				return nil, fmt.Errorf("s3: %w", err)
			}
		}
		// A bucket has no marker: it is ready when it answers, with a clock close to ours.
		if opts.WaitForStorage {
			if err := bucket.WaitReachable(ctx, log.Printf); err != nil {
				return nil, fmt.Errorf("s3: %w", err)
			}
		}
		checkStorage = func(ctx context.Context) storage.Report {
			return storage.CheckS3(ctx, cfg.DataDir, bucket, cfg.Origins())
		}
	} else {
		if opts.WaitForStorage {
			if err := storage.WaitForMarker(ctx, cfg.StorageDir, log.Printf); err != nil {
				return nil, err
			}
		} else if _, err := os.Stat(filepath.Join(cfg.StorageDir, storage.MarkerName)); err != nil {
			return nil, fmt.Errorf("%s isn't a Share storage folder (no %s); mount the drive or volume and run `share init`", cfg.StorageDir, storage.MarkerName)
		}
		checkStorage = func(context.Context) storage.Report { return storage.Check(layout, cfg.MinFreeSpace()) }
	}
	report := checkStorage(ctx)
	for _, p := range report.Problems {
		log.Printf("storage: problem: %s", p.Message)
	}
	for _, w := range report.Warnings {
		log.Printf("storage: %s", w.Message)
	}
	if opts.CheckStorage != nil {
		checkStorage = opts.CheckStorage
	}

	d, err := OpenDatabase(ctx, cfg, now(), opts.WaitForStorage)
	if err != nil {
		return nil, err
	}
	serverID, err := d.ServerID(ctx)
	if err != nil {
		d.Close()
		return nil, err
	}
	var lib *storage.Library
	if bucket != nil {
		lib = storage.OpenS3Library(d, bucket, layout, cfg.Location, now, log.Printf)
	} else if lib, err = storage.OpenLibrary(d, layout, cfg.Location, now, log.Printf); err != nil {
		d.Close()
		return nil, err
	}

	authSvc := auth.NewService(d, now)
	maxFile := cfg.MaxFileSize()
	if report.MaxFileSize > 0 && (maxFile == 0 || report.MaxFileSize < maxFile) {
		maxFile = report.MaxFileSize
	}
	upCfg := upload.DefaultConfig()
	if opts.Upload != nil {
		upCfg = *opts.Upload
	}
	upCfg.MaxFileSize, upCfg.MinFreeSpace = maxFile, cfg.MinFreeSpace()
	if upCfg.ChunkSize == 0 {
		upCfg.ChunkSize = cfg.ChunkSize()
	}
	var tus *upload.TusHandler
	if bucket == nil {
		if tus, err = upload.NewTusHandler(upCfg, authSvc, lib, now); err != nil {
			lib.Close()
			d.Close()
			return nil, err
		}
	}
	openOriginal := func(ctx context.Context, f db.File) (io.ReadSeekCloser, error) { return lib.Open(ctx, f) }
	th := &thumbs.Store{DB: d, Open: openOriginal, Now: now, Logf: log.Printf}
	if bucket != nil {
		th.Bucket = bucket // the thumbnails go next to the files
	} else {
		th.Dir = layout.ThumbsDir()
	}
	lib.OnPurged = th.Remove
	var crcs *checksum.Store
	if bucket == nil {
		crcs = &checksum.Store{DB: d, Root: lib.Root(), Open: lib.OpenFile, Pace: checksum.Pace, Logf: log.Printf}
		lib.OnReady = crcs.Wake
	}
	dl := &downloads.Store{Now: now}
	bucketOrigin := ""
	if bucket != nil {
		bucketOrigin = bucket.Origin()
	}
	ui := webui.New(cfg, bucketOrigin)
	if !ui.Built() {
		log.Printf("webui: the website isn't built into this binary; serving a placeholder")
	}
	var local *localtls.Loader
	if cfg.SelfSigned() {
		if err := localtls.Ensure(cfg.DataDir, cfg.CertificateHost()); err != nil {
			lib.Close()
			d.Close()
			return nil, fmt.Errorf("https certificate: %w", err)
		}
		if local, err = localtls.NewLoader(cfg.DataDir); err != nil {
			lib.Close()
			d.Close()
			return nil, fmt.Errorf("https certificate: %w", err)
		}
	}
	version := opts.Version
	if version == "" {
		version = "dev"
	}
	apiHandlers := &api.API{
		Cfg: cfg, Auth: authSvc, ServerID: serverID, MaxFileSize: maxFile, Lib: lib, Thumbs: th, Local: local,
		APK: &api.APK{Path: cfg.App.APKFile}, Downloads: dl, Checksums: crcs, S3: bucket, Now: now, ServerVersion: version,
		CheckStorage: checkStorage,
	}

	mux := http.NewServeMux()
	if tus != nil {
		mux.Handle(upload.BasePath, tus)
	} else {
		mux.HandleFunc(upload.BasePath, func(w http.ResponseWriter, r *http.Request) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "Files go into the bucket here, not over tus.")
		})
		upload.NewS3Handler(upCfg, authSvc, lib, now).Register(mux)
	}
	apiHandlers.Register(mux)
	mux.Handle("/", ui)

	a := &App{Cfg: cfg, DB: d, Lib: lib, Auth: authSvc, Tus: tus, S3: bucket, Thumbs: th, CRCs: crcs, UI: ui, Local: local, now: now}
	// Health checks get their answer however they arrive, also from a proxy that names no
	// visitor. Everything else goes through the guard. Browsers may only change state from
	// this site itself; the app sends no Origin.
	outer := http.NewServeMux()
	outer.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
	outer.Handle("/", a.guard(sameOrigin(mux)))
	a.Handler = outer
	a.sched = &jobs.Scheduler{Now: now, Logf: log.Printf, Last: lastRun(d), Ran: recordRun(d), Tasks: []jobs.Task{
		// Also at every start: it repairs what a stop in the middle of something left behind.
		{Name: "reconcile uploads", Every: 5 * time.Minute, AtStart: true, Run: func(ctx context.Context) error {
			authSvc.PruneLimits()
			if tus != nil {
				tus.PruneQueues()
			}
			dl.Prune()
			return lib.Reconcile(ctx, cfg.IncompleteTTL())
		}},
		{Name: "expire PINs and sessions", Every: time.Hour, Run: func(ctx context.Context) error {
			if _, err := d.EndExpiredPins(ctx, now()); err != nil {
				return err
			}
			if _, err := d.DeleteStalePinSessions(ctx, now().Add(-30*24*time.Hour)); err != nil {
				return err
			}
			if _, err := d.DeleteEndedDevices(ctx, now().Add(-30*24*time.Hour), now().Add(-auth.WebSessionIdle)); err != nil {
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

// lastRun reads when a periodic job last ran from the database, so that a server that only
// runs for minutes at a time still does its hourly and daily work.
func lastRun(d *db.DB) func(context.Context, string) time.Time {
	return func(ctx context.Context, name string) time.Time {
		t, err := d.JobRun(ctx, name)
		if err != nil {
			log.Printf("jobs: %s: %v", name, err)
		}
		return t
	}
}

// recordRun notes a run of a periodic job in the database.
func recordRun(d *db.DB) func(context.Context, string, time.Time) {
	return func(ctx context.Context, name string, at time.Time) {
		if err := d.SetJobRun(ctx, name, at); err != nil {
			log.Printf("jobs: %s: %v", name, err)
		}
	}
}

// Serve repairs what the last run left behind and does the housekeeping that is due, then
// answers requests until ctx ends, and shuts down gracefully: running requests get 20 seconds.
func (a *App) Serve(ctx context.Context) error {
	a.sched.RunDue(ctx)
	if token, err := a.Auth.FirstStartInvite(ctx); err != nil {
		log.Printf("share: first-start invite: %v", err)
	} else if token != "" {
		log.Printf("share: nobody has an account yet. To become the admin, open this link on your phone or computer; it works once, for 7 days:")
		log.Printf("share:   %s/join#%s", strings.TrimSuffix(a.Cfg.PublicURL, "/"), token)
		log.Printf("share: or make an invite with your name: share invite --admin --name YOURNAME")
	}

	var servers []*http.Server
	var starts []func() error
	if c := a.Cfg.HTTP; c != nil {
		s := newServer(c.Listen, a.Handler)
		servers, starts = append(servers, s), append(starts, s.ListenAndServe)
		log.Printf("share: http on %s", c.Listen)
	}
	if c := a.Cfg.HTTPS; c != nil {
		s := newServer(c.Listen, a.Handler)
		start := func() error { return s.ListenAndServeTLS(c.Certificate.CertFile, c.Certificate.KeyFile) }
		if a.Local != nil {
			s.TLSConfig = &tls.Config{GetCertificate: a.Local.GetCertificate, MinVersion: tls.VersionTLS12}
			start = func() error { return s.ListenAndServeTLS("", "") }
			log.Printf("share: https on %s, with its own certificate %s", c.Listen, a.Local.Fingerprint())
		} else {
			log.Printf("share: https on %s, with %s", c.Listen, c.Certificate.CertFile)
		}
		servers, starts = append(servers, s), append(starts, start)
	}
	log.Printf("share: public address %s", a.Cfg.PublicURL)
	if a.Cfg.Home != nil {
		log.Printf("share: the app at home uses %s", a.Cfg.HomeURL)
	}
	if c := a.Cfg.HTTP; c != nil && !inContainer() {
		for _, u := range homeURLs(c.Listen) {
			log.Printf("share: on this network, also %s", u)
		}
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
	if a.CRCs != nil {
		workers.Go(func() { a.CRCs.Run(jobsCtx) })
	}

	errCh := make(chan error, len(servers))
	for _, start := range starts {
		go func() { errCh <- start() }()
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
