package app

// Setting Share up from the website, while nobody has an account. Before a storage folder on a
// drive has its marker, `share serve` serves only the website's setup page and its API, which
// sets the folder up as `share init` does: nothing that needs the database, which would be
// made in that folder too, maybe on a drive that isn't mounted. Then the server starts for
// real, and the same page makes the first admin. Visitors at home may do this at any time,
// others in the first minutes after a start, or with the link in the log.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/webui"
)

const (
	// SetupHeader carries the secret of the setup link in the log.
	SetupHeader = "X-Share-Setup"
	// SetupWindow is how long after a start Share can be set up from outside the home network
	// without the link in the log.
	SetupWindow = 15 * time.Minute
)

// Setup says who may set Share up while nobody has an account: visitors at home, anyone until
// Until, and anyone with the secret of the link in the log.
type Setup struct {
	Secret string // new at every start
	Until  time.Time
}

// NewSetup is the setup of a server that starts at now.
func NewSetup(now time.Time) *Setup {
	b := make([]byte, 24)
	rand.Read(b)
	return &Setup{Secret: base64.RawURLEncoding.EncodeToString(b), Until: now.Add(SetupWindow)}
}

// Page is the setup page's address.
func (s *Setup) Page(cfg *config.Config) string {
	return strings.TrimSuffix(cfg.PublicURL, "/") + "/setup"
}

// Link is the setup page's address with the secret, for the log.
func (s *Setup) Link(cfg *config.Config) string { return s.Page(cfg) + "#" + s.Secret }

// allows reports whether a request may set Share up.
func (s *Setup) allows(r *http.Request, now time.Time) bool {
	if auth.AtHome(r) || now.Before(s.Until) {
		return true
	}
	got := r.Header.Get(SetupHeader)
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(s.Secret)) == 1
}

func setupClosed(w http.ResponseWriter) {
	httpx.WriteError(w, http.StatusForbidden, "setup_closed",
		"From outside the home network, Share can be set up only in the first minutes after it starts: restart it, or open the link in its log.")
}

// NeedsSetup reports whether the storage folder has to be set up before the server can start:
// on a drive, without its marker.
func NeedsSetup(cfg *config.Config) bool {
	if cfg.S3 != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(cfg.StorageDir, storage.MarkerName))
	return err != nil
}

// SetupStatus is the answer to GET /api/setup: before the folder is set up, what it looks like;
// afterwards, whether the first admin is still to come.
type SetupStatus struct {
	Ready           bool           `json:"ready"`
	NeedsAdmin      bool           `json:"needs_admin"`
	Name            string         `json:"name"`
	Languages       []string       `json:"languages"`
	DefaultLanguage string         `json:"default_language"`
	StorageDir      string         `json:"storage_dir"`
	Exists          bool           `json:"exists"`
	Empty           bool           `json:"empty"`
	FSType          string         `json:"fs_type"`
	TotalBytes      int64          `json:"total_bytes"`
	FreeBytes       int64          `json:"free_bytes"`
	Warnings        []SetupFinding `json:"warnings"`
}

// SetupFinding is something `share check` would say about the drive.
type SetupFinding struct {
	Code    string `json:"code"`
	Level   string `json:"level"` // "problem" or "warning"
	Message string `json:"message"`
}

func setupStatus(cfg *config.Config) SetupStatus {
	return SetupStatus{Name: cfg.Name, Languages: cfg.Languages, DefaultLanguage: cfg.DefaultLanguage, Warnings: []SetupFinding{}}
}

// setupHandler is the whole server before the folder is set up: /healthz, the setup page with
// the website's files, and the setup API. Every other page goes to the setup page; every other
// API call is refused until then.
func (a *App) setupHandler(setup *Setup, ui http.Handler, setUp func() error) http.Handler {
	cfg := a.Cfg
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !setup.allows(r, a.now()) {
			setupClosed(w)
			return
		}
		u := storage.CheckUnset(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir})
		st := setupStatus(cfg)
		st.StorageDir, st.Exists, st.Empty = cfg.StorageDir, u.Exists, u.Empty
		st.FSType, st.TotalBytes, st.FreeBytes = u.Report.Storage.Type, u.Report.Storage.Total, u.Report.Storage.Free
		for _, f := range u.Report.Problems {
			st.Warnings = append(st.Warnings, SetupFinding{f.Code, "problem", f.Message})
		}
		for _, f := range u.Report.Warnings {
			st.Warnings = append(st.Warnings, SetupFinding{f.Code, "warning", f.Message})
		}
		httpx.WriteJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/setup", func(w http.ResponseWriter, r *http.Request) {
		if !setup.allows(r, a.now()) {
			setupClosed(w)
			return
		}
		if err := setUp(); err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "setup_failed", err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusServiceUnavailable, "setting_up", "Share isn't set up yet.")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if p == "/setup" || strings.Contains(p, ".") || strings.HasPrefix(p, "/assets/") || strings.HasPrefix(p, "/icons/") || strings.HasPrefix(p, "/fonts/") {
			ui.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/setup", http.StatusFound)
	})
	outer := http.NewServeMux()
	outer.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("ok\n"))
	})
	outer.Handle("/", a.guard(mux))
	return outer
}

// RunSetup serves the setup page until the storage folder is set up: through the page, by
// `share init`, or by mounting a drive that has its marker. It returns nil then, with the
// ports free again for the server.
func RunSetup(ctx context.Context, cfg *config.Config, setup *Setup) error {
	layout := storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}
	a := &App{Cfg: cfg, now: time.Now}
	ready := make(chan struct{})
	var once sync.Once
	setUp := func() error {
		if err := storage.Init(layout); err != nil {
			return err
		}
		once.Do(func() { close(ready) })
		return nil
	}
	handler := a.setupHandler(setup, webui.New(cfg, ""), setUp)

	var servers []*http.Server
	errCh := make(chan error, 2)
	if c := cfg.HTTP; c != nil {
		s := newServer(c.Listen, handler)
		servers = append(servers, s)
		go func() { errCh <- s.ListenAndServe() }()
	}
	// The https port only with certificate files: Share's own certificate lives in data_dir,
	// in the folder that isn't set up yet.
	if c := cfg.HTTPS; c != nil && !cfg.SelfSigned() {
		s := newServer(c.Listen, handler)
		servers = append(servers, s)
		go func() { errCh <- s.ListenAndServeTLS(c.Certificate.CertFile, c.Certificate.KeyFile) }()
	}
	stop := func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, s := range servers {
			s.Shutdown(shutdown)
		}
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			stop()
			return ctx.Err()
		case err := <-errCh:
			if !errors.Is(err, http.ErrServerClosed) {
				stop()
				return err
			}
		case <-ready:
			stop()
			return nil
		case <-tick.C:
			if !NeedsSetup(cfg) { // share init, or a drive with its marker
				stop()
				return nil
			}
		}
	}
}

type firstAdminRequest struct {
	Name       string `json:"name"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
}

// registerSetup serves the setup page once the folder is set up: whether the first admin is
// still to come, and making them, which signs the page's browser in.
func (a *App) registerSetup(mux *http.ServeMux, setup *Setup) {
	failed := func(w http.ResponseWriter, err error) {
		log.Printf("setup: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
	}
	alreadySetUp := func(w http.ResponseWriter) {
		httpx.WriteError(w, http.StatusConflict, "already_set_up", "Share has an admin already: sign in.")
	}
	mux.HandleFunc("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		none, err := a.Auth.NoAccounts(r.Context())
		if err != nil {
			failed(w, err)
			return
		}
		if none && !setup.allows(r, a.now()) {
			setupClosed(w)
			return
		}
		st := setupStatus(a.Cfg)
		st.Ready, st.NeedsAdmin = true, none
		httpx.WriteJSON(w, http.StatusOK, st)
	})
	mux.HandleFunc("POST /api/setup/admin", func(w http.ResponseWriter, r *http.Request) {
		var req firstAdminRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		if none, err := a.Auth.NoAccounts(r.Context()); err != nil {
			failed(w, err)
			return
		} else if !none {
			alreadySetUp(w)
			return
		}
		if !setup.allows(r, a.now()) {
			setupClosed(w)
			return
		}
		s, err := a.Auth.FirstAdmin(r.Context(), r, req.Name, req.Username, req.Password, req.DeviceName)
		var input *auth.InputError
		switch {
		case err == nil:
			auth.SetAccountCookie(w, r, s.Token)
			auth.ClearSessionCookie(w, r)
			w.WriteHeader(http.StatusNoContent)
		case errors.As(err, &input):
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+input.Field+" "+input.Problem+".")
		case errors.Is(err, auth.ErrNotFirst):
			alreadySetUp(w)
		default:
			failed(w, err)
		}
	})
}
