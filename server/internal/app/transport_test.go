package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// unlockWeb unlocks a PIN the way the website does and returns the answer's cookies.
func unlockWeb(t *testing.T, h http.Handler, code, remote string, headers map[string]string) (*httptest.ResponseRecorder, []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest("POST", "http://share.example.test/api/pin/unlock", strings.NewReader(`{"code": "`+code+`", "client": "web"}`))
	req.RemoteAddr = remote
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec, rec.Result().Cookies()
}

func TestPlainHTTPAtHome(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	for _, remote := range []string{"127.0.0.1:50000", "192.168.8.30:50000", "[fd00::30]:50000"} {
		rec, cookies := unlockWeb(t, e.app.Handler, pin.Code, remote, nil)
		if rec.Code != http.StatusOK || len(cookies) != 1 {
			t.Fatalf("%s: unlock %d, cookies %v", remote, rec.Code, cookies)
		}
		// Browsers keep neither Secure nor __Host- cookies from a plain-http page.
		if c := cookies[0]; c.Name != "share_pin" || c.Secure || !c.HttpOnly {
			t.Errorf("%s: cookie %+v", remote, c)
		}

		// The cookie works over plain http, and the secure cookie's name doesn't.
		for name, want := range map[string]int{"share_pin": http.StatusOK, "__Host-share_pin": http.StatusUnauthorized} {
			req := httptest.NewRequest("GET", "http://share.example.test/api/session", nil)
			req.RemoteAddr = remote
			req.AddCookie(&http.Cookie{Name: name, Value: cookies[0].Value})
			rec := httptest.NewRecorder()
			e.app.Handler.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("%s: session with %s: %d, want %d", remote, name, rec.Code, want)
			}
		}
	}

	// The page sets the browser id without Secure too.
	req := httptest.NewRequest("GET", "http://share.example.test/", nil)
	req.RemoteAddr = "192.168.8.30:50000"
	rec := httptest.NewRecorder()
	e.app.Handler.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Secure || strings.HasPrefix(c.Name, "__Host-") {
			t.Errorf("page cookie over plain http: %+v", c)
		}
	}
}

func TestPlainHTTPFromOutsideIsRefused(t *testing.T) {
	e := newEnv(t)
	// A page goes on to the public address, which is https.
	req := httptest.NewRequest("GET", "http://share.example.test/join?x=1", nil)
	req.RemoteAddr = "203.0.113.9:50000"
	rec := httptest.NewRecorder()
	e.app.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://share.example.test/join?x=1" {
		t.Errorf("page: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	// The API and uploads get an error, before any PIN is looked at.
	pin := e.newPin(db.PinPermanent)
	rec, cookies := unlockWeb(t, e.app.Handler, pin.Code, "203.0.113.9:50000", nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), `"https_required"`) || len(cookies) != 0 {
		t.Errorf("unlock: %d %s %v", rec.Code, rec.Body, cookies)
	}
	if want := errorStatuses(t)["https_required"]; want != http.StatusForbidden {
		t.Errorf("contract/errors.json says https_required is %d", want)
	}
}

func TestThroughTheTunnel(t *testing.T) {
	e := newEnv(t)
	pin := e.newPin(db.PinPermanent)
	tunnel := map[string]string{"CF-Connecting-IP": "203.0.113.9", "Cf-Visitor": `{"scheme":"https"}`}
	rec, cookies := unlockWeb(t, e.app.Handler, pin.Code, "127.0.0.1:50000", tunnel)
	if rec.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Name != "__Host-share_pin" || !cookies[0].Secure {
		t.Fatalf("through the tunnel: %d %v", rec.Code, cookies)
	}

	// A visitor who reached Cloudflare over plain http is sent to https.
	req := httptest.NewRequest("GET", "http://share.example.test/", nil)
	req.RemoteAddr = "127.0.0.1:50000"
	req.Header.Set("CF-Connecting-IP", "203.0.113.9")
	req.Header.Set("Cf-Visitor", `{"scheme":"http"}`)
	rec = httptest.NewRecorder()
	e.app.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://share.example.test/" {
		t.Errorf("plain http through the tunnel: %d %q", rec.Code, rec.Header().Get("Location"))
	}

	// The tunnel's header counts only from a trusted proxy: from the internet it is plain http.
	rec, _ = unlockWeb(t, e.app.Handler, pin.Code, "203.0.113.50:50000", tunnel)
	if rec.Code != http.StatusForbidden {
		t.Errorf("a made-up tunnel header: %d", rec.Code)
	}
}

func TestHomeAddressOverPlainHTTP(t *testing.T) {
	e := newEnvWith(t, `"home_url": "http://192.168.8.1:8080"`)
	srv := e.do(nil, "GET", "/api/server", e.admin().token, nil, nil).json(t)
	assertShape(t, "server", readFixture(t, "api/server.json")["response"], srv)
	if srv["local_url"] != "http://192.168.8.1:8080" || len(srv["local_cert_sha256"].([]any)) != 0 {
		t.Errorf("server: %v", srv)
	}
}

func TestHomeProof(t *testing.T) {
	// The worked example both sides check against.
	ex := readFixture(t, "api/home_proof.json")["example"].(map[string]any)
	if got := auth.HomeProof(ids.HashToken(ex["token"].(string)), ex["nonce"].(string)); got != ex["proof"] {
		t.Fatalf("HomeProof = %s, want %s", got, ex["proof"])
	}

	e := newEnv(t)
	admin := e.admin()
	device := admin.body["device"].(map[string]any)["id"].(string)
	ask := func(remote string, headers map[string]string, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://share.example.test/api/home/proof", strings.NewReader(body))
		req.RemoteAddr = remote
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		e.app.Handler.ServeHTTP(rec, req)
		return rec
	}
	nonce := "a-nonce-the-phone-picked-0123456789"
	rec := ask("192.168.8.30:50000", nil, `{"device": "`+device+`", "nonce": "`+nonce+`"}`)
	var got struct{ Proof string }
	json.Unmarshal(rec.Body.Bytes(), &got)
	if rec.Code != http.StatusOK || got.Proof != auth.HomeProof(ids.HashToken(admin.token), nonce) {
		t.Fatalf("at home: %d %s", rec.Code, rec.Body)
	}
	assertShape(t, "home proof", readFixture(t, "api/home_proof.json")["response"], map[string]any{"proof": got.Proof})

	for name, rec := range map[string]*httptest.ResponseRecorder{
		"through the tunnel": ask("127.0.0.1:50000", map[string]string{"CF-Connecting-IP": "203.0.113.9", "Cf-Visitor": `{"scheme":"https"}`},
			`{"device": "`+device+`", "nonce": "`+nonce+`"}`),
		"an unknown phone": ask("192.168.8.30:50000", nil, `{"device": "dvnope", "nonce": "`+nonce+`"}`),
	} {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
	if rec := ask("192.168.8.30:50000", nil, `{"device": "`+device+`", "nonce": "short"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("a short nonce: %d", rec.Code)
	}
	// Over https it isn't needed, and isn't answered.
	if r := e.postJSON(nil, "/api/home/proof", "", map[string]string{"device": device, "nonce": nonce}, nil); r.status != http.StatusNotFound {
		t.Errorf("over https: %d", r.status)
	}
	// A signed-out phone gets no proof either.
	e.do(nil, "POST", "/api/auth/logout", admin.token, nil, nil)
	if rec := ask("192.168.8.30:50000", nil, `{"device": "`+device+`", "nonce": "`+nonce+`"}`); rec.Code != http.StatusNotFound {
		t.Errorf("signed out: %d", rec.Code)
	}
}
