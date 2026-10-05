package app

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/webui"
)

// setupConfig is a configuration whose storage folder isn't set up yet.
func setupConfig(t *testing.T, listen string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(fmt.Sprintf(
		`{"public_url": "https://share.example.test", "storage_dir": %q, "time_zone": "Europe/Rome", "http": {"listen": %q}%s}`,
		filepath.Join(t.TempDir(), "files"), listen, testDatabase(t))))
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsSetup(cfg) {
		t.Fatal("a new folder doesn't need to be set up")
	}
	return cfg
}

// The storage folder is set up from the website with the secret from the log; then the server
// starts for real and hands the page the first admin's invite.
func TestSetupFromTheWebsite(t *testing.T) {
	cfg := setupConfig(t, "127.0.0.1:0")
	secret := NewSetupSecret()
	set := make(chan struct{})
	setUp := func() error {
		if err := storage.Init(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}); err != nil {
			return err
		}
		close(set)
		return nil
	}
	e := &env{t: t, cfg: cfg, clock: &clock{t: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}}
	pre := &App{Cfg: cfg, now: time.Now}
	e.srv = httptest.NewTLSServer(pre.setupHandler(secret, webui.New(cfg, ""), setUp))
	with := map[string]string{SetupHeader: secret}

	for name, headers := range map[string]map[string]string{"no secret": nil, "an older secret": {SetupHeader: NewSetupSecret()}} {
		if r := e.do(nil, "GET", "/api/setup", "", nil, headers); r.status != http.StatusForbidden || r.errorCode() != "forbidden" {
			t.Errorf("%s: %d %s", name, r.status, r.body)
		}
	}
	r := e.do(nil, "GET", "/api/setup", "", nil, with)
	if r.status != http.StatusOK {
		t.Fatalf("GET /api/setup: %d %s", r.status, r.body)
	}
	assertShape(t, "setup", readFixture(t, "api/setup.json")["response"], r.json(t))
	if got := r.json(t); got["ready"] != false || got["exists"] != false || got["storage_dir"] != cfg.StorageDir || got["fs_type"] == "" {
		t.Errorf("before: %v", got)
	}
	if r := e.do(nil, "GET", "/api/info", "", nil, nil); r.status != http.StatusServiceUnavailable || r.errorCode() != "setting_up" {
		t.Errorf("another API call: %d %s", r.status, r.body)
	}
	if r := e.do(noRedirects(e.srv.Client()), "GET", "/library", "", nil, nil); r.status != http.StatusFound || r.header.Get("Location") != "/setup" {
		t.Errorf("a page: %d %q", r.status, r.header.Get("Location"))
	}
	if r := e.do(nil, "GET", "/healthz", "", nil, nil); r.status != http.StatusOK {
		t.Errorf("/healthz: %d", r.status)
	}
	if r := e.postJSON(nil, "/api/setup", "", map[string]any{}, with); r.status != http.StatusNoContent {
		t.Fatalf("POST /api/setup: %d %s", r.status, r.body)
	}
	<-set
	if NeedsSetup(cfg) {
		t.Fatal("still not set up")
	}
	e.srv.Close()

	a, err := New(context.Background(), cfg, Options{Now: e.clock.Now, SetupSecret: secret})
	if err != nil {
		t.Fatal(err)
	}
	e.app = a
	e.srv = httptest.NewTLSServer(a.Handler)
	t.Cleanup(func() {
		e.srv.Close()
		a.Close()
	})
	r = e.do(nil, "GET", "/api/setup", "", nil, with)
	if r.status != http.StatusOK {
		t.Fatalf("GET /api/setup afterwards: %d %s", r.status, r.body)
	}
	assertShape(t, "setup ready", readFixture(t, "api/setup_ready.json")["response"], r.json(t))
	if got := r.json(t); got["ready"] != true || got["needs_admin"] != true {
		t.Errorf("afterwards: %v", got)
	}
	if r := e.postJSON(nil, "/api/setup/invite", "", map[string]any{}, map[string]string{SetupHeader: NewSetupSecret()}); r.status != http.StatusForbidden {
		t.Errorf("the invite with an older secret: %d %s", r.status, r.body)
	}
	logged, err := a.Auth.FirstStartInvite(context.Background()) // as Serve logs it
	if err != nil {
		t.Fatal(err)
	}
	r = e.postJSON(nil, "/api/setup/invite", "", map[string]any{}, with)
	if r.status != http.StatusCreated {
		t.Fatalf("the invite: %d %s", r.status, r.body)
	}
	assertShape(t, "setup invite", readFixture(t, "api/setup_invite.json")["response"], r.json(t))
	if got := r.json(t)["invite"]; got != logged {
		t.Errorf("the page's invite %v isn't the one in the log, %s", got, logged)
	}
	e.accept(logged, "Stefan's computer")
	if r := e.postJSON(nil, "/api/setup/invite", "", map[string]any{}, with); r.status != http.StatusConflict || r.errorCode() != "already_set_up" {
		t.Errorf("a second invite: %d %s", r.status, r.body)
	}
	if got := e.do(nil, "GET", "/api/setup", "", nil, with).json(t); got["needs_admin"] != false {
		t.Errorf("with an admin: %v", got)
	}

	// A server that didn't just set up its folder knows nothing of a setup.
	other := newEnv(t)
	if r := other.do(nil, "GET", "/api/setup", "", nil, with); r.status != http.StatusNotFound || r.errorCode() != "not_found" {
		t.Errorf("without a setup: %d %s", r.status, r.body)
	}
}

// RunSetup serves on the configured port until the page sets the folder up, then frees it.
func TestRunSetupHandsOverThePort(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	cfg := setupConfig(t, addr)
	secret := NewSetupSecret()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunSetup(ctx, cfg, secret) }()

	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if res, err := http.Get("http://" + addr + "/healthz"); err == nil {
			res.Body.Close()
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("the setup server doesn't answer")
		}
	}
	req, _ := http.NewRequest("POST", "http://"+addr+"/api/setup", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(SetupHeader, secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /api/setup: %d %s", res.StatusCode, body)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunSetup didn't end")
	}
	again, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("the port isn't free again: %v", err)
	}
	again.Close()
}

// `share init` from the command line, or mounting a drive with its marker, ends the setup too.
func TestRunSetupEndsWithShareInit(t *testing.T) {
	cfg := setupConfig(t, "127.0.0.1:0")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunSetup(ctx, cfg, NewSetupSecret()) }()
	if err := storage.Init(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunSetup didn't notice the marker")
	}
}
