package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/upload"
)

// clock is a settable time shared by the server's goroutines and the test.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) Now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

type env struct {
	t     *testing.T
	app   *App
	srv   *httptest.Server
	clock *clock
	cfg   *config.Config
	free  atomic.Int64
}

func newEnv(t *testing.T) *env { return newEnvWith(t, "") }

// newEnvWith adds settings to the test configuration, e.g. `"home_url": "…"`.
func newEnvWith(t *testing.T, settings string) *env {
	t.Helper()
	dir := t.TempDir()
	storageDir := filepath.Join(dir, "storage")
	if settings != "" {
		settings = ", " + settings
	}
	cfg, err := config.Parse([]byte(fmt.Sprintf(
		`{"public_url": "https://share.example.test", "storage_dir": %q, "time_zone": "Europe/Rome", "http": {"listen": "127.0.0.1:0"}%s}`,
		storageDir, settings)))
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.Init(storage.Layout{StorageDir: cfg.StorageDir, DataDir: cfg.DataDir}); err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, cfg: cfg, clock: &clock{t: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}}
	e.free.Store(1 << 40)
	upCfg := upload.DefaultConfig()
	upCfg.FreeSpace = func() (int64, error) { return e.free.Load(), nil }
	a, err := New(context.Background(), cfg, Options{Now: e.clock.Now, Upload: &upCfg})
	if err != nil {
		t.Fatal(err)
	}
	e.app = a
	e.srv = httptest.NewTLSServer(a.Handler)
	t.Cleanup(func() {
		e.srv.Close()
		a.Close()
	})
	return e
}

// newPin makes a PIN of the given kind through the auth service, like `share pin create`.
func (e *env) newPin(kind string) db.Pin {
	e.t.Helper()
	p, err := e.app.Auth.CreatePin(context.Background(), kind, "test")
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func (r response) json(t *testing.T) map[string]any {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("response %d is not JSON: %q", r.status, r.body)
	}
	return v
}

func (r response) errorCode() string {
	var v struct{ Error struct{ Code string } }
	json.Unmarshal(r.body, &v)
	return v.Error.Code
}

// do sends one request; token, if set, goes into Authorization.
func (e *env) do(client *http.Client, method, path, token string, body io.Reader, headers map[string]string) response {
	e.t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, body)
	if err != nil {
		e.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if client == nil {
		client = e.srv.Client()
	}
	res, err := client.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	r := response{res.StatusCode, res.Header, b}
	if code := r.errorCode(); code != "" {
		if want, ok := errorStatuses(e.t)[code]; !ok {
			e.t.Errorf("%s %s: error code %q is not in contract/errors.json", method, path, code)
		} else if want != r.status {
			e.t.Errorf("%s %s: %q came with status %d; contract/errors.json says %d", method, path, code, r.status, want)
		}
	}
	return r
}

// errorStatuses maps every error code in contract/errors.json to its HTTP status.
func errorStatuses(t *testing.T) map[string]int {
	t.Helper()
	errorStatusesOnce.Do(func() {
		var v struct {
			Codes map[string]struct{ Status int }
		}
		b, err := os.ReadFile(filepath.Join("..", "..", "..", "contract", "errors.json"))
		if err == nil {
			err = json.Unmarshal(b, &v)
		}
		if err != nil {
			t.Fatalf("contract/errors.json: %v", err)
		}
		errorStatusesMap = make(map[string]int, len(v.Codes))
		for code, c := range v.Codes {
			errorStatusesMap[code] = c.Status
		}
	})
	return errorStatusesMap
}

var (
	errorStatusesOnce sync.Once
	errorStatusesMap  map[string]int
)

func (e *env) postJSON(client *http.Client, path, token string, v any, headers map[string]string) response {
	e.t.Helper()
	b, _ := json.Marshal(v)
	h := map[string]string{"Content-Type": "application/json"}
	for k, val := range headers {
		h[k] = val
	}
	return e.do(client, "POST", path, token, bytes.NewReader(b), h)
}

// unlockApp unlocks as the app would and returns the session token.
func (e *env) unlockApp(code, oldToken string) (string, map[string]any) {
	e.t.Helper()
	r := e.postJSON(nil, "/api/pin/unlock", oldToken, map[string]string{"code": code, "client": "app"}, nil)
	if r.status != http.StatusOK {
		e.t.Fatalf("unlock: %d %s", r.status, r.body)
	}
	v := r.json(e.t)
	return v["token"].(string), v
}

// tus is a minimal tus 1.0 client, enough to drive the server the way Uppy and the app do.
type tus struct {
	e     *env
	token string
}

func (c tus) headers(extra map[string]string) map[string]string {
	h := map[string]string{"Tus-Resumable": "1.0.0"}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func (c tus) create(name string, size int) response {
	meta := "filename " + base64.StdEncoding.EncodeToString([]byte(name)) +
		",filetype " + base64.StdEncoding.EncodeToString([]byte("application/octet-stream"))
	return c.e.do(nil, "POST", "/tus/", c.token, nil, c.headers(map[string]string{
		"Upload-Length": strconv.Itoa(size), "Upload-Metadata": meta,
	}))
}

func (c tus) mustCreate(name string, size int) string {
	c.e.t.Helper()
	r := c.create(name, size)
	if r.status != http.StatusCreated {
		c.e.t.Fatalf("create: %d %s", r.status, r.body)
	}
	return r.header.Get("Location")
}

func (c tus) patch(loc string, offset int, chunk []byte) response {
	return c.e.do(nil, "PATCH", loc, c.token, bytes.NewReader(chunk), c.headers(map[string]string{
		"Upload-Offset": strconv.Itoa(offset), "Content-Type": "application/offset+octet-stream",
	}))
}

func (c tus) head(loc string) (offset int, r response) {
	r = c.e.do(nil, "HEAD", loc, c.token, nil, c.headers(nil))
	offset, _ = strconv.Atoi(r.header.Get("Upload-Offset"))
	return offset, r
}

// send uploads data from offset on in chunks and returns the last response.
func (c tus) send(loc string, data []byte, offset, chunk int) response {
	c.e.t.Helper()
	var r response
	for offset < len(data) {
		end := min(offset+chunk, len(data))
		r = c.patch(loc, offset, data[offset:end])
		if r.status != http.StatusNoContent {
			c.e.t.Fatalf("PATCH at %d: %d %s", offset, r.status, r.body)
		}
		offset, _ = strconv.Atoi(r.header.Get("Upload-Offset"))
	}
	return r
}

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func (e *env) file(id string) db.File {
	e.t.Helper()
	f, err := e.app.DB.FileByID(context.Background(), id)
	if err != nil {
		e.t.Fatalf("file %s: %v", id, err)
	}
	return f
}

func idOf(loc string) string { return strings.TrimPrefix(loc, "/tus/") }

func TestUploadInChunksLandsInTheDayFolder(t *testing.T) {
	size, chunk := 3<<20, 1<<20
	if !testing.Short() {
		// Larger than Cloudflare's 100 MB request limit, sent in 20 MiB pieces as clients do.
		size, chunk = 120<<20, 20<<20
	}
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	token, _ := e.unlockApp(pin.Code, "")
	c := tus{e, token}
	data := randomBytes(t, size)

	loc := c.mustCreate("Holiday video.mp4", len(data))
	if !strings.HasPrefix(loc, "/tus/") || len(idOf(loc)) != 26 {
		t.Fatalf("Location %q is not a relative upload path", loc)
	}
	last := c.send(loc, data, 0, chunk)
	if got := last.header.Get("Share-File-Id"); got != idOf(loc) {
		t.Errorf("Share-File-Id = %q, want %q", got, idOf(loc))
	}

	f := e.file(idOf(loc))
	if f.State != db.StateReady || f.Kind != db.KindVideo || f.UploadDay != "2026-09-27" {
		t.Fatalf("file row: %+v", f)
	}
	stored, err := os.ReadFile(filepath.Join(e.cfg.StorageDir, "2026-09-27", "Holiday video.mp4"))
	if err != nil {
		t.Fatal(err)
	}
	if sha256.Sum256(stored) != sha256.Sum256(data) {
		t.Fatal("stored file differs from what was sent")
	}
}

func TestLostFinalResponseMakesNoDuplicate(t *testing.T) {
	e := newEnv(t)
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	c := tus{e, token}
	data := randomBytes(t, 4096)
	loc := c.mustCreate("a.jpg", len(data))
	c.send(loc, data, 0, 4096)

	// The client never saw that 204 and asks again, as tus-js-client does.
	offset, r := c.head(loc)
	if r.status != http.StatusOK || offset != len(data) {
		t.Fatalf("HEAD after finishing: %d, offset %d", r.status, offset)
	}
	if r := c.patch(loc, len(data), nil); r.status != http.StatusNoContent {
		t.Fatalf("empty PATCH at the end: %d %s", r.status, r.body)
	}
	if r := e.do(nil, "DELETE", loc, token, nil, map[string]string{"Tus-Resumable": "1.0.0"}); r.status != http.StatusForbidden {
		t.Fatalf("DELETE of a finished upload: %d", r.status)
	}
	ready, _ := e.app.DB.FilesInStates(context.Background(), db.StateReady)
	if len(ready) != 1 {
		t.Fatalf("%d files in the library, want 1", len(ready))
	}
}

// failingReader delivers n bytes and then breaks the connection's body.
type failingReader struct {
	data []byte
	n    int
}

func (r *failingReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, errors.New("connection lost")
	}
	k := copy(p, r.data[:min(len(r.data), r.n, len(p))])
	r.data, r.n = r.data[k:], r.n-k
	return k, nil
}

func TestInterruptedPatchResumesWhereItStopped(t *testing.T) {
	e := newEnv(t)
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	c := tus{e, token}
	data := randomBytes(t, 3<<20)
	loc := c.mustCreate("clip.mov", len(data))

	req, _ := http.NewRequest("PATCH", e.srv.URL+loc, &failingReader{data: data, n: 1 << 20})
	req.ContentLength = int64(len(data))
	for k, v := range c.headers(map[string]string{"Upload-Offset": "0", "Content-Type": "application/offset+octet-stream"}) {
		req.Header.Set(k, v)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if res, err := e.srv.Client().Do(req); err == nil {
		res.Body.Close()
	}

	offset, r := c.head(loc)
	if r.status != http.StatusOK || offset > 1<<20 {
		t.Fatalf("HEAD after the break: %d, offset %d", r.status, offset)
	}
	c.send(loc, data, offset, 1<<20)
	stored, _ := os.ReadFile(filepath.Join(e.cfg.StorageDir, "2026-09-27", "clip.mov"))
	if sha256.Sum256(stored) != sha256.Sum256(data) {
		t.Fatalf("stored file differs after resuming at %d", offset)
	}
}

func TestUploadsAreInvisibleToOtherSessions(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	mine, _ := e.unlockApp(pin.Code, "")
	theirs, _ := e.unlockApp(pin.Code, "")
	loc := tus{e, mine}.mustCreate("secret.pdf", 10)
	for _, method := range []string{"HEAD", "PATCH", "DELETE"} {
		r := e.do(nil, method, loc, theirs, strings.NewReader(""), map[string]string{
			"Tus-Resumable": "1.0.0", "Upload-Offset": "0", "Content-Type": "application/offset+octet-stream",
		})
		if r.status != http.StatusNotFound {
			t.Errorf("%s by another session: %d, want 404", method, r.status)
		}
	}
	if r := e.do(nil, "HEAD", loc, "", nil, map[string]string{"Tus-Resumable": "1.0.0"}); r.status != http.StatusUnauthorized {
		t.Errorf("HEAD without a session: %d, want 401", r.status)
	}
}

func TestEndedPinStopsUploadsUntilANewUnlock(t *testing.T) {
	e := newEnv(t)
	first := e.newPin(db.PinDay)
	token, _ := e.unlockApp(first.Code, "")
	c := tus{e, token}
	data := randomBytes(t, 2<<20)
	loc := c.mustCreate("party.mp4", len(data))
	c.send(loc, data[:1<<20], 0, 1<<20)

	e.clock.Add(25 * time.Hour) // the 24-hour PIN ends mid-upload
	r := c.patch(loc, 1<<20, data[1<<20:])
	if r.status != http.StatusUnauthorized || r.errorCode() != "session_ended" {
		t.Fatalf("PATCH after the PIN ended: %d %s", r.status, r.body)
	}

	second := e.newPin(db.PinDay)
	newToken, v := e.unlockApp(second.Code, token)
	if v["moved_uploads"].(float64) != 1 {
		t.Fatalf("moved_uploads = %v", v["moved_uploads"])
	}
	c.token = newToken
	offset, _ := c.head(loc)
	c.send(loc, data, offset, 1<<20)
	if f := e.file(idOf(loc)); f.State != db.StateReady || f.PinID != second.ID {
		t.Fatalf("after continuing: %+v", f)
	}
}

func TestBrowsersCantChangeStateFromOtherSites(t *testing.T) {
	e := newEnv(t)
	r := e.postJSON(nil, "/api/pin/unlock", "", map[string]string{"code": "K7M2Q"},
		map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"})
	if r.status != http.StatusForbidden {
		t.Fatalf("cross-site unlock: %d, want 403", r.status)
	}
	// JSON endpoints also insist on the JSON content type.
	r = e.do(nil, "POST", "/api/pin/unlock", "", strings.NewReader(`{"code":"K7M2Q"}`), map[string]string{"Content-Type": "text/plain"})
	if r.status != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain unlock: %d, want 415", r.status)
	}
}

func TestMethodOverrideIsIgnored(t *testing.T) {
	e := newEnv(t)
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	loc := tus{e, token}.mustCreate("a.txt", 5)
	r := e.do(nil, "POST", loc, token, strings.NewReader("hello"), map[string]string{
		"Tus-Resumable": "1.0.0", "X-HTTP-Method-Override": "PATCH", "Upload-Offset": "0",
		"Content-Type": "application/offset+octet-stream",
	})
	if r.status != http.StatusNotFound {
		t.Fatalf("POST with a method override: %d, want 404", r.status)
	}
	if offset, _ := (tus{e, token}).head(loc); offset != 0 {
		t.Fatalf("the override wrote %d bytes", offset)
	}
}

func TestWrongPinsPauseAndThenWorkAgain(t *testing.T) {
	e := newEnv(t)
	e.newPin(db.PinPermanent)
	headers := map[string]string{"Share-Client": "phone-aaaaaaaaaaaaaaaa"}
	unlock := func(code string) response {
		return e.postJSON(nil, "/api/pin/unlock", "", map[string]string{"code": code, "client": "app"}, headers)
	}
	fixture := readFixture(t, "api/pin_unlock_errors.json")
	cases := fixture["cases"].([]any)

	if r := unlock("abc"); r.status != http.StatusBadRequest || r.errorCode() != "pin_format" {
		t.Fatalf("malformed: %d %s", r.status, r.body)
	} else {
		assertShape(t, "pin_format", cases[0].(map[string]any)["response"], r.json(t))
	}
	for i := 1; i <= 4; i++ {
		r := unlock("22222")
		if r.status != http.StatusUnauthorized || r.errorCode() != "pin_wrong" {
			t.Fatalf("wrong try %d: %d %s", i, r.status, r.body)
		}
		if i == 1 {
			assertShape(t, "pin_wrong", cases[1].(map[string]any)["response"], r.json(t))
		}
	}
	r := unlock("22222")
	if r.status != http.StatusTooManyRequests || r.header.Get("Retry-After") != "600" {
		t.Fatalf("5th wrong try: %d, Retry-After %q", r.status, r.header.Get("Retry-After"))
	}
	assertShape(t, "pin_locked", cases[2].(map[string]any)["response"], r.json(t))

	e.clock.Add(10 * time.Minute)
	if r := unlock("22222"); r.status != http.StatusUnauthorized {
		t.Fatalf("after the pause: %d, want 401", r.status)
	}
}

func TestFullDriveRejectsNewUploads(t *testing.T) {
	e := newEnv(t)
	token, _ := e.unlockApp(e.newPin(db.PinPermanent).Code, "")
	e.free.Store(e.cfg.MinFreeSpace() + 100)
	r := tus{e, token}.create("big.mov", 1000)
	if r.status != http.StatusRequestEntityTooLarge || r.errorCode() != "no_space" {
		t.Fatalf("create on a full drive: %d %s", r.status, r.body)
	}
}

func TestWebsiteSessionCookie(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinDay)
	jar, _ := cookiejar.New(nil)
	browser := e.srv.Client()
	browser.Jar = jar

	r := e.postJSON(browser, "/api/pin/unlock", "", map[string]string{"code": strings.ToLower(pin.Code), "client": "web"}, nil)
	if r.status != http.StatusOK {
		t.Fatalf("unlock: %d %s", r.status, r.body)
	}
	body := r.json(t)
	if _, hasToken := body["token"]; hasToken {
		t.Fatal("the website must get its token as a cookie, not in the body")
	}
	assertShape(t, "unlock web", readFixture(t, "api/pin_unlock_web.json")["response"], body)
	cookie := r.header.Get("Set-Cookie")
	for _, want := range []string{"__Host-share_pin=", "HttpOnly", "Secure", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("Set-Cookie %q lacks %s", cookie, want)
		}
	}

	session := readFixture(t, "api/session.json")["cases"].([]any)
	r = e.do(browser, "GET", "/api/session", "", nil, nil)
	if r.status != http.StatusOK {
		t.Fatalf("session: %d %s", r.status, r.body)
	}
	assertShape(t, "session", session[0].(map[string]any)["response"], r.json(t))

	if r := e.do(browser, "POST", "/api/session/end", "", nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("end session: %d", r.status)
	}
	r = e.do(browser, "GET", "/api/session", "", nil, nil)
	if r.status != http.StatusUnauthorized {
		t.Fatalf("session after ending: %d", r.status)
	}
	assertShape(t, "unauthorized", session[2].(map[string]any)["response"], r.json(t))
}

func TestInfoMatchesContract(t *testing.T) {
	e := newEnv(t)
	r := e.do(nil, "GET", "/api/info", "", nil, nil)
	if r.status != http.StatusOK || r.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("info: %d, Cache-Control %q", r.status, r.header.Get("Cache-Control"))
	}
	got := r.json(t)
	assertShape(t, "info", readFixture(t, "api/info.json")["response"], got)
	if got["chunk_size_bytes"].(float64) != 20<<20 {
		t.Errorf("chunk_size_bytes = %v", got["chunk_size_bytes"])
	}
	_, unlock := e.unlockApp(e.newPin(db.PinDay).Code, "")
	assertShape(t, "unlock app", readFixture(t, "api/pin_unlock_app.json")["response"], unlock)
}

func TestTusCapabilityDiscoveryNeedsNoSession(t *testing.T) {
	e := newEnv(t)
	r := e.do(nil, "OPTIONS", "/tus/", "", nil, nil)
	if r.status >= 300 || r.header.Get("Tus-Version") == "" {
		t.Fatalf("OPTIONS: %d, Tus-Version %q", r.status, r.header.Get("Tus-Version"))
	}
}

func TestPagesHaveSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	r := e.do(nil, "GET", "/", "", nil, nil)
	if r.status != http.StatusOK {
		t.Fatalf("GET /: %d", r.status)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "Referrer-Policy"} {
		if r.header.Get(h) == "" {
			t.Errorf("GET / has no %s", h)
		}
	}
	if r := e.do(nil, "GET", "/api/nope", "", nil, nil); r.status != http.StatusNotFound || r.errorCode() != "not_found" {
		t.Errorf("unknown API path: %d %s", r.status, r.body)
	}
}

func TestServeShutsDownGracefully(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.app.Serve(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve didn't stop")
	}
}

func readFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "contract", name))
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return v
}

// assertShape checks that got has exactly the keys of want, with the same JSON types, all the
// way down. Values may differ (ids, times), except error codes, which are the contract.
func assertShape(t *testing.T, label string, want, got any) {
	t.Helper()
	if err := sameShape("", want, got); err != nil {
		t.Errorf("%s: %v\nwant shape of %v\ngot %v", label, err, want, got)
	}
}

func sameShape(path string, want, got any) error {
	if want == nil || got == nil {
		return nil // null stands for "optional value", e.g. expires_at of a permanent PIN
	}
	if reflect.TypeOf(want) != reflect.TypeOf(got) {
		return fmt.Errorf("%s: type %T, want %T", path, got, want)
	}
	switch w := want.(type) {
	case map[string]any:
		g := got.(map[string]any)
		for k := range w {
			if _, ok := g[k]; !ok {
				return fmt.Errorf("%s.%s is missing", path, k)
			}
		}
		for k := range g {
			if _, ok := w[k]; !ok {
				return fmt.Errorf("%s.%s is not in the contract", path, k)
			}
			if err := sameShape(path+"."+k, w[k], g[k]); err != nil {
				return err
			}
		}
		if code, ok := w["code"].(string); ok && strings.HasSuffix(path, "error") && g["code"] != code {
			return fmt.Errorf("%s.code = %v, want %s", path, g["code"], code)
		}
	case []any:
		g := got.([]any)
		if len(w) > 0 && len(g) > 0 {
			return sameShape(path+"[0]", w[0], g[0])
		}
	}
	return nil
}
