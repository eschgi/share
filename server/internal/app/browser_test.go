package app

import (
	"net/http"
	"net/http/cookiejar"
	"testing"

	"github.com/eschgi/share/server/internal/db"
)

// fromPage is what a browser adds to the requests of Share's own pages.
var fromPage = map[string]string{"Sec-Fetch-Site": "same-origin"}

// webBrowser is a client that keeps cookies, like a browser. It is a copy, so the test server's
// shared client stays without cookies.
func (e *env) webBrowser() *http.Client {
	jar, _ := cookiejar.New(nil)
	c := *e.srv.Client()
	c.Jar = jar
	return &c
}

func TestCrossSiteRequestsGetJSON(t *testing.T) {
	e := newEnv(t)
	for name, headers := range map[string]map[string]string{
		"another site":        {"Sec-Fetch-Site": "cross-site"},
		"a sibling subdomain": {"Sec-Fetch-Site": "same-site"},
		"another origin":      {"Origin": "https://evil.example"},
	} {
		r := e.postJSON(nil, "/api/pin/unlock", "", map[string]string{"code": "K7M2Q"}, headers)
		if r.status != http.StatusForbidden || r.errorCode() != "cross_origin" {
			t.Errorf("%s: %d %s, want 403 cross_origin", name, r.status, r.body)
		}
	}
	if r := e.do(nil, "GET", "/api/info", "", nil, nil); r.header.Get("Cross-Origin-Resource-Policy") != "same-origin" {
		t.Errorf("API answers have Cross-Origin-Resource-Policy %q", r.header.Get("Cross-Origin-Resource-Policy"))
	}
}

func TestCookieRequestsMustComeFromAPage(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	browser := e.webBrowser()
	if r := e.postJSON(browser, "/api/pin/unlock", "", map[string]string{"code": pin.Code}, fromPage); r.status != http.StatusOK {
		t.Fatalf("unlock: %d %s", r.status, r.body)
	}

	// With the cookie, a request that doesn't say where it comes from isn't one of the pages'.
	if r := e.postJSON(browser, "/api/session/end", "", struct{}{}, nil); r.status != http.StatusForbidden || r.errorCode() != "cross_origin" {
		t.Errorf("without Origin: %d %s, want 403 cross_origin", r.status, r.body)
	}
	if r := e.do(browser, "POST", "/tus/", "", nil, tus{}.headers(map[string]string{"Upload-Length": "5"})); r.status != http.StatusForbidden {
		t.Errorf("tus without Origin: %d, want 403", r.status)
	}
	// A POST without a body, which a form could send, must still be JSON.
	if r := e.do(browser, "POST", "/api/session/end", "", nil, fromPage); r.status != http.StatusUnsupportedMediaType {
		t.Errorf("without a JSON body: %d %s, want 415", r.status, r.body)
	}
	// Reading needs nothing of that.
	if r := e.do(browser, "GET", "/api/session", "", nil, nil); r.status != http.StatusOK {
		t.Errorf("reading the session: %d %s", r.status, r.body)
	}
	if r := e.postJSON(browser, "/api/session/end", "", struct{}{}, fromPage); r.status != http.StatusNoContent {
		t.Errorf("from the page: %d %s", r.status, r.body)
	}

	// The app sends a bearer token and no Origin, as before.
	token, _ := e.unlockApp(pin.Code, "")
	if r := e.do(nil, "POST", "/api/session/end", token, nil, nil); r.status != http.StatusNoContent {
		t.Errorf("the app ending its session: %d %s", r.status, r.body)
	}
}

func TestUnknownClientIsABadRequest(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	r := e.postJSON(nil, "/api/pin/unlock", "", map[string]string{"code": pin.Code, "client": "tv"}, nil)
	if r.status != http.StatusBadRequest || r.errorCode() != "bad_request" {
		t.Errorf("unlock for a tv: %d %s, want 400 bad_request", r.status, r.body)
	}
	admin := e.admin()
	e.setPassword(admin.token, "stefan", "correct horse")
	r = e.postJSON(nil, "/api/auth/login", "", map[string]string{"username": "stefan", "password": "correct horse", "client": "tv"}, nil)
	if r.status != http.StatusBadRequest || r.errorCode() != "bad_request" {
		t.Errorf("sign-in for a tv: %d %s, want 400 bad_request", r.status, r.body)
	}
	r = e.postJSON(nil, "/api/invites/accept", "", map[string]string{"token": e.invite(admin, "Maria", db.RoleMember), "client": "tv"}, nil)
	if r.status != http.StatusBadRequest || r.errorCode() != "bad_request" {
		t.Errorf("an invite for a tv: %d %s, want 400 bad_request", r.status, r.body)
	}
}
