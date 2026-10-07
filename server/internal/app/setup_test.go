package app

import (
	"context"
	"encoding/json"
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
func setupConfig(t *testing.T, listen, settings string) *config.Config {
	t.Helper()
	cfg, err := config.Parse([]byte(fmt.Sprintf(
		`{"public_url": "https://share.example.test", "storage_dir": %q, "time_zone": "Europe/Rome", "http": {"listen": %q}%s%s}`,
		filepath.Join(t.TempDir(), "files"), listen, testDatabase(t), settings)))
	if err != nil {
		t.Fatal(err)
	}
	if !NeedsSetup(cfg) {
		t.Fatal("a new folder doesn't need to be set up")
	}
	return cfg
}

// firstAdmin is what the setup page sends to make the first admin.
func firstAdmin(t *testing.T) map[string]any {
	req := readFixture(t, "api/setup_admin.json")["request"].(map[string]any)
	if req["username"] != "stefan" || req["password"] != "correct horse" {
		t.Fatalf("the fixture's request: %v", req)
	}
	return req
}

// At home, the storage folder and the first admin are set up from the website, without the
// link in the log, also long after the start.
func TestSetupFromTheWebsite(t *testing.T) {
	cfg := setupConfig(t, "127.0.0.1:0", "")
	e := &env{t: t, cfg: cfg, clock: &clock{t: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}}
	setup := NewSetup(e.clock.Now())
	e.clock.Add(time.Hour)
	set := make(chan struct{})
	setUp := func() error {
		if err := storage.Init(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}); err != nil {
			return err
		}
		close(set)
		return nil
	}
	pre := &App{Cfg: cfg, now: e.clock.Now}
	e.srv = httptest.NewTLSServer(pre.setupHandler(setup, webui.New(cfg, ""), setUp))

	r := e.do(nil, "GET", "/api/setup", "", nil, nil)
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
	if r := e.postJSON(nil, "/api/setup", "", map[string]any{}, nil); r.status != http.StatusNoContent {
		t.Fatalf("POST /api/setup: %d %s", r.status, r.body)
	}
	<-set
	if NeedsSetup(cfg) {
		t.Fatal("still not set up")
	}
	e.srv.Close()

	a, err := New(context.Background(), cfg, Options{Now: e.clock.Now, Setup: setup})
	if err != nil {
		t.Fatal(err)
	}
	e.app = a
	e.srv = httptest.NewTLSServer(a.Handler)
	t.Cleanup(func() {
		e.srv.Close()
		a.Close()
	})
	if got := e.do(nil, "GET", "/api/info", "", nil, nil).json(t); got["setup"] != true {
		t.Errorf("info without an account: %v", got)
	}
	r = e.do(nil, "GET", "/api/setup", "", nil, nil)
	assertShape(t, "setup ready", readFixture(t, "api/setup_ready.json")["response"], r.json(t))
	if got := r.json(t); got["ready"] != true || got["needs_admin"] != true {
		t.Errorf("afterwards: %d %v", r.status, got)
	}

	browser := e.webBrowser()
	for field, value := range map[string]string{"name": " ", "username": "s", "password": "short"} {
		req := firstAdmin(t)
		req[field] = value
		if r := e.postJSON(browser, "/api/setup/admin", "", req, fromPage); r.status != http.StatusBadRequest || !strings.Contains(string(r.body), field) {
			t.Errorf("with a bad %s: %d %s", field, r.status, r.body)
		}
	}
	if r := e.postJSON(browser, "/api/setup/admin", "", firstAdmin(t), fromPage); r.status != http.StatusNoContent {
		t.Fatalf("POST /api/setup/admin: %d %s", r.status, r.body)
	}
	me := e.do(browser, "GET", "/api/me", "", nil, nil)
	if me.status != http.StatusOK {
		t.Fatalf("me: %d %s", me.status, me.body)
	}
	u, dv := me.json(t)["user"].(map[string]any), me.json(t)["device"].(map[string]any)
	if u["name"] != "Stefan" || u["role"] != "admin" || u["username"] != "stefan" || u["has_password"] != true || dv["client"] != "web" || dv["home_only"] != true {
		t.Errorf("the first admin: %v, %v", u, dv)
	}
	if r := e.postJSON(e.webBrowser(), "/api/setup/admin", "", firstAdmin(t), fromPage); r.status != http.StatusConflict || r.errorCode() != "already_set_up" {
		t.Errorf("a second admin: %d %s", r.status, r.body)
	}
	if got := e.do(nil, "GET", "/api/setup", "", nil, nil).json(t); got["needs_admin"] != false {
		t.Errorf("with an admin: %v", got)
	}
	if got := e.do(nil, "GET", "/api/info", "", nil, nil).json(t); got["setup"] != false {
		t.Errorf("info with an account: %v", got)
	}
	if r := e.signInWeb(e.webBrowser(), "stefan", "correct horse"); r.status != http.StatusOK {
		t.Errorf("signing in with the password: %d %s", r.status, r.body)
	}
}

// From outside the home network, the first admin can be made in the first minutes after a
// start, or with the link in the log.
func TestSetupFromOutsideHome(t *testing.T) {
	cfg := setupConfig(t, "127.0.0.1:0", `, "proxy": {"headers": "cloudflare", "trusted_proxies": ["192.168.8.20"]}`)
	if err := storage.Init(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}); err != nil {
		t.Fatal(err)
	}
	c := &clock{t: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}
	setup := NewSetup(c.Now())
	a, err := New(context.Background(), cfg, Options{Now: c.Now, Setup: setup})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.Close() })
	send := func(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req := httptest.NewRequest(method, "https://share.example.test"+path, rd)
		req.RemoteAddr = "192.168.8.20:40000"
		req.Header.Set("CF-Connecting-IP", "203.0.113.9")
		req.Header.Set("Cf-Visitor", `{"scheme":"https"}`)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		a.Handler.ServeHTTP(rec, req)
		return rec
	}
	code := func(rec *httptest.ResponseRecorder) string {
		var body struct{ Error struct{ Code string } }
		json.Unmarshal(rec.Body.Bytes(), &body)
		return body.Error.Code
	}

	c.Add(SetupWindow - time.Second)
	if rec := send("GET", "/api/setup", nil, nil); rec.Code != http.StatusOK {
		t.Errorf("in the first minutes: %d %s", rec.Code, rec.Body)
	}
	c.Add(time.Second)
	if rec := send("GET", "/api/setup", nil, nil); rec.Code != http.StatusForbidden || code(rec) != "setup_closed" {
		t.Errorf("later: %d %s", rec.Code, rec.Body)
	}
	if rec := send("POST", "/api/setup/admin", firstAdmin(t), nil); rec.Code != http.StatusForbidden || code(rec) != "setup_closed" {
		t.Errorf("an admin later: %d %s", rec.Code, rec.Body)
	}
	older := map[string]string{SetupHeader: NewSetup(c.Now()).Secret}
	if rec := send("POST", "/api/setup/admin", firstAdmin(t), older); rec.Code != http.StatusForbidden || code(rec) != "setup_closed" {
		t.Errorf("with the link of another start: %d %s", rec.Code, rec.Body)
	}
	link := map[string]string{SetupHeader: setup.Secret}
	if rec := send("GET", "/api/setup", nil, link); rec.Code != http.StatusOK {
		t.Errorf("with the link in the log: %d %s", rec.Code, rec.Body)
	}
	rec := send("POST", "/api/setup/admin", firstAdmin(t), link)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("the admin with the link in the log: %d %s", rec.Code, rec.Body)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 || !strings.HasPrefix(cookies[0].Name, "__Host-") || !cookies[0].Secure {
		t.Errorf("the browser's cookie: %v", cookies)
	}
	if u, err := a.DB.UserByUsername(context.Background(), "stefan"); err != nil || u.Role != "admin" {
		t.Errorf("the first admin: %+v, %v", u, err)
	}
	// Once someone has an account, there is nothing to hide.
	if rec := send("GET", "/api/setup", nil, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"needs_admin":false`) {
		t.Errorf("with an admin: %d %s", rec.Code, rec.Body)
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
	cfg := setupConfig(t, addr, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunSetup(ctx, cfg, NewSetup(time.Now())) }()

	for start := time.Now(); ; time.Sleep(20 * time.Millisecond) {
		if res, err := http.Get("http://" + addr + "/healthz"); err == nil {
			res.Body.Close()
			break
		}
		if time.Since(start) > 5*time.Second {
			t.Fatal("the setup server doesn't answer")
		}
	}
	res, err := http.Post("http://"+addr+"/api/setup", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("POST /api/setup: %d %s", res.StatusCode, body)
	}
	// RunSetup may take its whole shutdown time, 5 seconds: the server waits that long for a
	// connection that hasn't sent a request yet, such as one a client dialled in reserve.
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
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
	cfg := setupConfig(t, "127.0.0.1:0", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- RunSetup(ctx, cfg, NewSetup(time.Now())) }()
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
