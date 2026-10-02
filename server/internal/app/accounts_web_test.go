package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
)

// setPassword gives the person behind token a username and password.
func (e *env) setPassword(token, username, password string) {
	e.t.Helper()
	b := mustJSON(e.t, map[string]string{"username": username, "password": password})
	if r := e.do(nil, "PUT", "/api/me/password", token, bytes.NewReader(b), map[string]string{"Content-Type": "application/json"}); r.status != http.StatusNoContent {
		e.t.Fatalf("set password: %d %s", r.status, r.body)
	}
}

// signInWeb signs a browser in with a password, as the website does.
func (e *env) signInWeb(browser *http.Client, username, password string) response {
	e.t.Helper()
	return e.postJSON(browser, "/api/auth/login", "", map[string]string{
		"username": username, "password": password, "device_name": "Firefox · Linux", "client": "web",
	}, fromPage)
}

// withPage adds what a browser adds to the requests of Share's own pages.
func withPage(h map[string]string) map[string]string {
	for k, v := range fromPage {
		h[k] = v
	}
	return h
}

// tusCreate starts an upload from a browser.
func (e *env) tusCreate(browser *http.Client, name string, size int) string {
	e.t.Helper()
	meta := "filename " + base64.StdEncoding.EncodeToString([]byte(name)) +
		",filetype " + base64.StdEncoding.EncodeToString([]byte("application/octet-stream"))
	r := e.do(browser, "POST", "/tus/", "", nil, withPage(tus{}.headers(map[string]string{
		"Upload-Length": strconv.Itoa(size), "Upload-Metadata": meta,
	})))
	if r.status != http.StatusCreated {
		e.t.Fatalf("create: %d %s", r.status, r.body)
	}
	return r.header.Get("Location")
}

func (e *env) tusPatch(browser *http.Client, loc string, offset int, chunk []byte) response {
	return e.do(browser, "PATCH", loc, "", bytes.NewReader(chunk), withPage(tus{}.headers(map[string]string{
		"Upload-Offset": strconv.Itoa(offset), "Content-Type": "application/offset+octet-stream",
	})))
}

// setCookie is the cookie of that name an answer sets, or nil.
func setCookie(h http.Header, name string) *http.Cookie {
	for _, c := range (&http.Response{Header: h}).Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func mustDecode(t *testing.T, b []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
}

func deviceID(t *testing.T, r response) string {
	t.Helper()
	return r.json(t)["device"].(map[string]any)["id"].(string)
}

func TestWebSignInSetsTheSessionCookie(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	browser := e.webBrowser()

	r := e.signInWeb(browser, "stefan", "correct horse")
	if r.status != http.StatusOK {
		t.Fatalf("sign in: %d %s", r.status, r.body)
	}
	body := r.json(t)
	if _, hasToken := body["token"]; hasToken {
		t.Fatal("a browser must get its key as a cookie, not in the body")
	}
	assertShape(t, "login web", readFixture(t, "api/login_web.json")["response"], body)
	c := setCookie(r.header, "__Host-share_session")
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteStrictMode || c.Path != "/" || c.MaxAge < 399*24*3600 {
		t.Fatalf("session cookie %+v", c)
	}
	// The test server is on 127.0.0.1, which is home: such a sign-in works only at home.
	if dv := body["device"].(map[string]any); dv["client"] != "web" || dv["name"] != "Firefox · Linux" || dv["home_only"] != true {
		t.Errorf("device %v", dv)
	}

	me := e.do(browser, "GET", "/api/me", "", nil, nil)
	if me.status != http.StatusOK || me.json(t)["device"].(map[string]any)["id"] != deviceID(t, r) {
		t.Fatalf("me in the browser: %d %s", me.status, me.body)
	}
	for _, path := range []string{"/api/library", "/api/files", "/api/pins", "/api/users"} {
		if r := e.do(browser, "GET", path, "", nil, nil); r.status != http.StatusOK {
			t.Errorf("%s in the browser: %d %s", path, r.status, r.body)
		}
	}
	mine := e.do(browser, "GET", "/api/me/devices", "", nil, nil)
	assertShape(t, "my devices", readFixture(t, "api/me_devices.json")["response"], mine.json(t))
	devices := mine.json(t)["devices"].([]any)
	this := 0
	for _, d := range devices {
		if d := d.(map[string]any); d["this"] == true && d["client"] == "web" {
			this++
		}
	}
	if len(devices) != 2 || this != 1 {
		t.Errorf("my devices: %v", devices)
	}
}

func TestWebSignInWithAnInvite(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	browser := e.webBrowser()
	r := e.postJSON(browser, "/api/invites/accept", "", map[string]string{
		"token": e.invite(admin, "Maria", db.RoleMember), "device_name": "Safari · iPhone", "client": "web",
	}, fromPage)
	if r.status != http.StatusOK {
		t.Fatalf("accept: %d %s", r.status, r.body)
	}
	assertShape(t, "accept web", readFixture(t, "api/invite_accept_web.json")["response"], r.json(t))
	if setCookie(r.header, "__Host-share_session") == nil {
		t.Fatal("no session cookie")
	}
	if u := e.do(browser, "GET", "/api/me", "", nil, nil).json(t)["user"].(map[string]any); u["name"] != "Maria" || u["role"] != "member" {
		t.Errorf("me: %v", u)
	}

	// An invite that adds a phone for someone adds a browser just as well.
	add := e.postJSON(nil, "/api/users/"+admin.userID+"/invites", admin.token, nil, nil)
	laptop := e.webBrowser()
	if r := e.postJSON(laptop, "/api/invites/accept", "", map[string]string{"token": add.json(t)["token"].(string), "client": "web"}, fromPage); r.status != http.StatusOK {
		t.Fatalf("accept for a new browser: %d %s", r.status, r.body)
	}
	me := e.do(laptop, "GET", "/api/me", "", nil, nil).json(t)
	if me["user"].(map[string]any)["id"] != admin.userID || me["device"].(map[string]any)["name"] != "Browser" {
		t.Errorf("the laptop: %v", me)
	}
}

func TestSigningInHandsOverWhatTheBrowserSent(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	pin := e.newPin(db.PinPermanent)
	browser := e.webBrowser()
	if r := e.postJSON(browser, "/api/pin/unlock", "", map[string]string{"code": pin.Code}, fromPage); r.status != http.StatusOK {
		t.Fatalf("unlock: %d %s", r.status, r.body)
	}
	data := randomBytes(t, 4096)
	loc := e.tusCreate(browser, "half.mp4", len(data))
	if r := e.tusPatch(browser, loc, 0, data[:2048]); r.status != http.StatusNoContent {
		t.Fatalf("first half: %d %s", r.status, r.body)
	}

	// Signing in, the upload becomes Stefan's and goes on; the PIN session is over.
	r := e.signInWeb(browser, "stefan", "correct horse")
	if c := setCookie(r.header, "__Host-share_pin"); c == nil || c.MaxAge >= 0 {
		t.Errorf("the PIN cookie isn't deleted: %+v", c)
	}
	if r := e.tusPatch(browser, loc, 2048, data[2048:]); r.status != http.StatusNoContent {
		t.Fatalf("second half: %d %s", r.status, r.body)
	}
	if f := e.file(idOf(loc)); f.State != db.StateReady || f.UserID != admin.userID || f.PinSessionID != "" || f.PinID != "" {
		t.Errorf("the upload after signing in: %+v", f)
	}
	if r := e.do(browser, "GET", "/api/session", "", nil, nil); r.status != http.StatusUnauthorized {
		t.Errorf("the PIN session after signing in: %d %s", r.status, r.body)
	}

	// With both cookies, the account's counts.
	if r := e.postJSON(browser, "/api/pin/unlock", "", map[string]string{"code": pin.Code}, fromPage); r.status != http.StatusOK {
		t.Fatalf("unlock again: %d %s", r.status, r.body)
	}
	if f := e.file(idOf(e.tusCreate(browser, "b.txt", 5))); f.UserID != admin.userID || f.PinSessionID != "" {
		t.Errorf("an upload with both cookies: %+v", f)
	}
}

func TestRevokedBrowserFallsBackToThePin(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	browser := e.webBrowser()
	id := deviceID(t, e.signInWeb(browser, "stefan", "correct horse"))
	if r := e.postJSON(browser, "/api/pin/unlock", "", map[string]string{"code": e.newPin(db.PinPermanent).Code}, fromPage); r.status != http.StatusOK {
		t.Fatalf("unlock: %d %s", r.status, r.body)
	}
	if r := e.do(nil, "DELETE", "/api/devices/"+id, admin.token, nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("signing the browser out: %d %s", r.status, r.body)
	}

	r := e.do(browser, "GET", "/api/me", "", nil, nil)
	if r.status != http.StatusUnauthorized || r.errorCode() != "signed_out" {
		t.Fatalf("me after the sign-out: %d %s", r.status, r.body)
	}
	if c := setCookie(r.header, "__Host-share_session"); c == nil || c.MaxAge >= 0 {
		t.Errorf("the dead cookie isn't deleted: %+v", c)
	}
	// Without it, the PIN counts again.
	if f := e.file(idOf(e.tusCreate(browser, "c.txt", 5))); f.PinSessionID == "" || f.UserID != "" {
		t.Errorf("an upload after the sign-out: %+v", f)
	}
}

func TestBrowserLogoutAlwaysWorks(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	browser := e.webBrowser()
	signIn := e.signInWeb(browser, "stefan", "correct horse")
	cookie := setCookie(signIn.header, "__Host-share_session")

	r := e.postJSON(browser, "/api/auth/logout", "", struct{}{}, fromPage)
	if r.status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", r.status, r.body)
	}
	if c := setCookie(r.header, "__Host-share_session"); c == nil || c.MaxAge >= 0 {
		t.Errorf("the cookie isn't deleted: %+v", c)
	}
	if dv, _ := e.app.DB.DeviceByID(context.Background(), deviceID(t, signIn)); dv.RevokedAt == nil {
		t.Error("the browser is still signed in")
	}
	if r := e.do(browser, "GET", "/api/me", "", nil, nil); r.status != http.StatusUnauthorized || r.errorCode() != "unauthorized" {
		t.Errorf("me after logout: %d %s", r.status, r.body)
	}
	// Again, without a cookie, and with one whose session is over: both fine.
	if r := e.postJSON(browser, "/api/auth/logout", "", struct{}{}, fromPage); r.status != http.StatusNoContent {
		t.Errorf("logout again: %d %s", r.status, r.body)
	}
	u, _ := url.Parse(e.srv.URL)
	browser.Jar.SetCookies(u, []*http.Cookie{cookie})
	if r := e.postJSON(browser, "/api/auth/logout", "", struct{}{}, fromPage); r.status != http.StatusNoContent || setCookie(r.header, "__Host-share_session") == nil {
		t.Errorf("logout with an ended session: %d %s", r.status, r.body)
	}
}

func TestSigningInAgainEndsTheEarlierSession(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	browser := e.webBrowser()
	first := deviceID(t, e.signInWeb(browser, "stefan", "correct horse"))
	second := deviceID(t, e.signInWeb(browser, "stefan", "correct horse"))
	if dv, _ := e.app.DB.DeviceByID(context.Background(), first); first == second || dv.RevokedAt == nil {
		t.Errorf("the first session: %+v", dv)
	}
	if r := e.do(browser, "GET", "/api/me", "", nil, nil); r.status != http.StatusOK || deviceID(t, r) != second {
		t.Errorf("me: %d %s", r.status, r.body)
	}
}

func TestHomeOnlySessions(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	tunnel := map[string]string{"CF-Connecting-IP": "203.0.113.9", "Cf-Visitor": `{"scheme":"https"}`}
	send := func(method, target, remote, body string, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if remote != "" {
			req.RemoteAddr = remote
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://"+req.Host)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		e.app.Handler.ServeHTTP(rec, req)
		return rec
	}
	login := func(target, remote string) (*http.Cookie, bool) {
		t.Helper()
		rec := send("POST", target, remote, `{"username": "stefan", "password": "correct horse", "client": "web"}`, nil, nil)
		cookies := rec.Result().Cookies()
		if rec.Code != http.StatusOK || len(cookies) == 0 {
			t.Fatalf("sign in at %s from %s: %d %s", target, remote, rec.Code, rec.Body)
		}
		var me struct {
			Device struct {
				HomeOnly bool `json:"home_only"`
			}
		}
		mustDecode(t, rec.Body.Bytes(), &me)
		for _, c := range cookies {
			if strings.HasSuffix(c.Name, "share_session") {
				return c, me.Device.HomeOnly
			}
		}
		t.Fatalf("no session cookie: %v", cookies)
		return nil, false
	}

	// Plain http at home: a cookie without Secure, for a session that works only at home.
	c, homeOnly := login("http://192.168.8.1:8080/api/auth/login", "192.168.8.30:50000")
	if c.Name != "share_session" || c.Secure || !homeOnly {
		t.Fatalf("at home over http: %+v, home only %v", c, homeOnly)
	}
	if rec := send("GET", "http://192.168.8.1:8080/api/me", "192.168.8.30:50000", "", c, nil); rec.Code != http.StatusOK {
		t.Errorf("at home: %d %s", rec.Code, rec.Body)
	}
	// The same key through the tunnel was taken elsewhere: refused, and the session is over.
	stolen := &http.Cookie{Name: "__Host-share_session", Value: c.Value}
	if rec := send("GET", "http://share.example.test/api/me", "127.0.0.1:50000", "", stolen, tunnel); rec.Code != http.StatusUnauthorized {
		t.Errorf("through the tunnel: %d %s", rec.Code, rec.Body)
	}
	if rec := send("GET", "http://192.168.8.1:8080/api/me", "192.168.8.30:50000", "", c, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("at home after it was seen elsewhere: %d", rec.Code)
	}

	// https from the home network to an address at home works only at home too, also as a
	// bearer token over the public address.
	c, homeOnly = login("https://192.168.8.1:8443/api/auth/login", "192.168.8.30:50000")
	if c.Name != "__Host-share_session" || !c.Secure || !homeOnly {
		t.Fatalf("at home over https: %+v, home only %v", c, homeOnly)
	}
	if rec := send("GET", "https://share.example.test/api/me", "", "", nil, map[string]string{"Authorization": "Bearer " + c.Value}); rec.Code != http.StatusUnauthorized {
		t.Errorf("the key as a bearer token from elsewhere: %d %s", rec.Code, rec.Body)
	}

	// Signed in at the public address, a browser's session works everywhere.
	c, homeOnly = login("https://share.example.test/api/auth/login", "")
	if homeOnly {
		t.Fatal("a sign-in at the public address is home-only")
	}
	if rec := send("GET", "http://share.example.test/api/me", "127.0.0.1:50000", "", c, tunnel); rec.Code != http.StatusOK {
		t.Errorf("through the tunnel: %d %s", rec.Code, rec.Body)
	}

	// The server itself counts as home.
	c, homeOnly = login("http://localhost:8080/api/auth/login", "127.0.0.1:50000")
	if !homeOnly {
		t.Error("a sign-in on this machine isn't home-only")
	}
	if rec := send("GET", "http://localhost:8080/api/me", "127.0.0.1:50000", "", c, nil); rec.Code != http.StatusOK {
		t.Errorf("on this machine: %d %s", rec.Code, rec.Body)
	}
}

func TestBrowserSessionsEndUnused(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	browser := e.webBrowser()
	id := deviceID(t, e.signInWeb(browser, "stefan", "correct horse"))

	e.clock.Add(auth.WebSessionIdle + time.Hour)
	if r := e.do(browser, "GET", "/api/me", "", nil, nil); r.status != http.StatusUnauthorized || r.errorCode() != "signed_out" {
		t.Errorf("after 400 days: %d %s", r.status, r.body)
	}
	now := e.clock.Now()
	if n, err := e.app.DB.DeleteEndedDevices(context.Background(), now.Add(-30*24*time.Hour), now.Add(-auth.WebSessionIdle)); n != 1 || err != nil {
		t.Errorf("DeleteEndedDevices = %d, %v; want only the browser", n, err)
	}
	if _, err := e.app.DB.DeviceByID(context.Background(), id); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the browser is still there: %v", err)
	}
	// The app's phone doesn't run out.
	if r := e.do(nil, "GET", "/api/me", admin.token, nil, nil); r.status != http.StatusOK {
		t.Errorf("the phone: %d %s", r.status, r.body)
	}
}

func TestCookieRenewedOncePerDay(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	browser := e.webBrowser()
	value := setCookie(e.signInWeb(browser, "stefan", "correct horse").header, "__Host-share_session").Value
	renewed := func() *http.Cookie {
		r := e.do(browser, "GET", "/api/me", "", nil, nil)
		if r.status != http.StatusOK {
			t.Fatalf("me: %d %s", r.status, r.body)
		}
		return setCookie(r.header, "__Host-share_session")
	}
	e.clock.Add(11 * time.Minute)
	if c := renewed(); c != nil {
		t.Errorf("renewed on the same day: %+v", c)
	}
	e.clock.Add(24 * time.Hour)
	if c := renewed(); c == nil || c.Value != value || c.MaxAge < 399*24*3600 {
		t.Errorf("on the next day: %+v", c)
	}
	if c := renewed(); c != nil {
		t.Errorf("renewed twice in a day: %+v", c)
	}
}

func TestDeleteAccountInTheBrowser(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	browser := e.webBrowser()
	e.postJSON(browser, "/api/invites/accept", "", map[string]string{"token": e.invite(admin, "Maria", db.RoleMember), "client": "web"}, fromPage)
	r := e.postJSON(browser, "/api/me/delete", "", struct{}{}, fromPage)
	if r.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	if c := setCookie(r.header, "__Host-share_session"); c == nil || c.MaxAge >= 0 {
		t.Errorf("the cookie isn't deleted: %+v", c)
	}
}

func TestMembersSignOutTheirOwnDevices(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Pixel 8")
	add := e.postJSON(nil, "/api/users/"+maria.userID+"/invites", admin.token, nil, nil)
	browser := e.webBrowser()
	laptop := deviceID(t, e.postJSON(browser, "/api/invites/accept", "", map[string]string{"token": add.json(t)["token"].(string), "client": "web"}, fromPage))

	mine := e.get("/api/me/devices", maria.token)
	assertShape(t, "my devices", readFixture(t, "api/me_devices.json")["response"], mine.json(t))
	if n := len(mine.json(t)["devices"].([]any)); n != 2 {
		t.Fatalf("Maria's devices: %s", mine.body)
	}
	wantStatus(t, "Maria signs her browser out", e.do(nil, "DELETE", "/api/devices/"+laptop, maria.token, nil, nil), http.StatusNoContent, "")
	wantStatus(t, "the browser", e.do(browser, "GET", "/api/me", "", nil, nil), http.StatusUnauthorized, "signed_out")
	adminPhone := admin.body["device"].(map[string]any)["id"].(string)
	wantStatus(t, "Maria and someone else's phone", e.do(nil, "DELETE", "/api/devices/"+adminPhone, maria.token, nil, nil), http.StatusNotFound, "not_found")
	mariaPhone := maria.body["device"].(map[string]any)["id"].(string)
	wantStatus(t, "an admin and Maria's phone", e.do(nil, "DELETE", "/api/devices/"+mariaPhone, admin.token, nil, nil), http.StatusNoContent, "")
}

func TestWebsiteScreensGetThePage(t *testing.T) {
	e := newEnv(t)
	routes := readFixture(t, "web_routes.json")["examples"].(map[string]any)
	for _, p := range routes["page"].([]any) {
		if r := e.do(nil, "GET", p.(string), "", nil, nil); r.status != http.StatusOK || !strings.HasPrefix(r.header.Get("Content-Type"), "text/html") {
			t.Errorf("GET %s: %d %s", p, r.status, r.header.Get("Content-Type"))
		}
	}
	for _, p := range routes["not_found"].([]any) {
		if r := e.do(nil, "GET", p.(string), "", nil, nil); r.status != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", p, r.status)
		}
	}
	if r := e.do(nil, "POST", "/library", "", nil, nil); r.status != http.StatusMethodNotAllowed {
		t.Errorf("POST /library: %d", r.status)
	}
}
