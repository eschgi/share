package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// unlockWeb unlocks a PIN the way the website does, over plain http, and returns the answer's
// cookies.
func unlockWeb(t *testing.T, h http.Handler, code, remote string, headers map[string]string) (*httptest.ResponseRecorder, []*http.Cookie) {
	t.Helper()
	return unlockAt(t, h, "http://share.example.test", code, remote, headers)
}

// unlockAt unlocks a PIN at an address, e.g. https://… for the https port.
func unlockAt(t *testing.T, h http.Handler, origin, code, remote string, headers map[string]string) (*httptest.ResponseRecorder, []*http.Cookie) {
	t.Helper()
	req := httptest.NewRequest("POST", origin+"/api/pin/unlock", strings.NewReader(`{"code": "`+code+`", "client": "web"}`))
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

// refused checks that a request was turned away with an error code, and set no cookie.
func refused(t *testing.T, name string, rec *httptest.ResponseRecorder, cookies []*http.Cookie, status int, code string) {
	t.Helper()
	if rec.Code != status || !strings.Contains(rec.Body.String(), `"`+code+`"`) || len(cookies) != 0 {
		t.Errorf("%s: %d %s %v, want %d %s", name, rec.Code, rec.Body, cookies, status, code)
	}
	if want := errorStatuses(t)[code]; want != status {
		t.Errorf("contract/errors.json says %s is %d", code, want)
	}
}

func TestThroughTheTunnel(t *testing.T) {
	e := newEnvWith(t, `"proxy": "cloudflare"`)
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
	rec, cookies = unlockWeb(t, e.app.Handler, pin.Code, "127.0.0.1:50000", map[string]string{"CF-Connecting-IP": "203.0.113.9", "Cf-Visitor": `{"scheme":"http"}`})
	refused(t, "unlocking over plain http through the tunnel", rec, cookies, http.StatusForbidden, "https_required")

	// From the tunnel's address only the tunnel's requests get in, on either port: without
	// its headers a request doesn't count as one from this machine either.
	for name, headers := range map[string]map[string]string{
		"no headers":               nil,
		"no Cf-Visitor":            {"CF-Connecting-IP": "203.0.113.9"},
		"an unreadable Cf-Visitor": {"CF-Connecting-IP": "203.0.113.9", "Cf-Visitor": "https"},
		"no visitor's address":     {"CF-Connecting-IP": "somewhere", "Cf-Visitor": `{"scheme":"https"}`},
	} {
		rec, cookies := unlockWeb(t, e.app.Handler, pin.Code, "127.0.0.1:50000", headers)
		refused(t, name, rec, cookies, http.StatusBadRequest, "proxy_headers")
	}
	rec, cookies = unlockAt(t, e.app.Handler, "https://share.example.test", pin.Code, "127.0.0.1:50000", nil)
	refused(t, "the https port without the tunnel's headers", rec, cookies, http.StatusBadRequest, "proxy_headers")

	// The tunnel's header counts only from a trusted proxy: from the internet it is plain http.
	rec, cookies = unlockWeb(t, e.app.Handler, pin.Code, "203.0.113.50:50000", tunnel)
	refused(t, "a made-up tunnel header", rec, cookies, http.StatusForbidden, "https_required")
}

func TestThroughAProxy(t *testing.T) {
	e := newEnvWith(t, `"proxy": {"headers": "x-forwarded", "trusted_proxies": ["192.168.8.20"]}`)
	pin := e.newPin(db.PinPermanent)
	proxy := "192.168.8.20:50000"
	via := func(forwardedFor, proto string) map[string]string {
		return map[string]string{"X-Forwarded-For": forwardedFor, "X-Forwarded-Proto": proto}
	}
	rec, cookies := unlockWeb(t, e.app.Handler, pin.Code, proxy, via("203.0.113.9", "https"))
	if rec.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Name != "__Host-share_pin" || !cookies[0].Secure {
		t.Fatalf("through the proxy: %d %s %v", rec.Code, rec.Body, cookies)
	}

	// A visitor on plain http: a page goes on to https, the API refuses.
	req := httptest.NewRequest("GET", "http://share.example.test/join", nil)
	req.RemoteAddr = proxy
	for k, v := range via("203.0.113.9", "http") {
		req.Header.Set(k, v)
	}
	rec = httptest.NewRecorder()
	e.app.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://share.example.test/join" {
		t.Errorf("a page over plain http: %d %q", rec.Code, rec.Header().Get("Location"))
	}
	rec, cookies = unlockWeb(t, e.app.Handler, pin.Code, proxy, via("203.0.113.9", "http"))
	refused(t, "unlocking over plain http", rec, cookies, http.StatusForbidden, "https_required")

	// The proxy's own X-Forwarded-Proto counts, not one the visitor sent before it.
	req = httptest.NewRequest("POST", "http://share.example.test/api/pin/unlock", strings.NewReader(`{"code": "`+pin.Code+`", "client": "web"}`))
	req.RemoteAddr = proxy
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Add("X-Forwarded-Proto", "https")
	req.Header.Add("X-Forwarded-Proto", "http")
	rec = httptest.NewRecorder()
	e.app.Handler.ServeHTTP(rec, req)
	refused(t, "a made-up X-Forwarded-Proto", rec, rec.Result().Cookies(), http.StatusForbidden, "https_required")

	// Without its headers, nothing from the proxy gets in.
	for name, headers := range map[string]map[string]string{
		"no headers":           nil,
		"no X-Forwarded-Proto": {"X-Forwarded-For": "203.0.113.9"},
		"no X-Forwarded-For":   {"X-Forwarded-Proto": "https"},
		"no visitor's address": via("somewhere", "https"),
		"only the X-Real-IP":   {"X-Real-IP": "203.0.113.9"},
	} {
		rec, cookies := unlockWeb(t, e.app.Handler, pin.Code, proxy, headers)
		refused(t, name, rec, cookies, http.StatusBadRequest, "proxy_headers")
	}

	// Everyone else in the home network still uses plain http at home.
	rec, cookies = unlockWeb(t, e.app.Handler, pin.Code, "192.168.8.30:50000", nil)
	if rec.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Name != "share_pin" || cookies[0].Secure {
		t.Errorf("at home: %d %v", rec.Code, cookies)
	}
}

func TestForgottenProxy(t *testing.T) {
	e := newEnv(t) // no "proxy"
	pin := e.newPin(db.PinPermanent)
	forwarded := map[string]string{"X-Forwarded-For": "203.0.113.9", "X-Forwarded-Proto": "https"}
	tunnel := map[string]string{"CF-Connecting-IP": "203.0.113.9", "Cf-Visitor": `{"scheme":"https"}`}

	// A proxy that config.json doesn't name doesn't make its visitors visitors at home.
	for name, tc := range map[string]struct {
		origin, remote string
		headers        map[string]string
	}{
		"Caddy on this machine":       {"http://share.example.test", "127.0.0.1:50000", forwarded},
		"cloudflared on this machine": {"http://share.example.test", "127.0.0.1:50000", tunnel},
		"nginx in the home network":   {"http://share.example.test", "192.168.8.20:50000", map[string]string{"X-Real-IP": "203.0.113.9"}},
		"a proxy on the https port":   {"https://share.example.test", "192.168.8.20:50000", forwarded},
		"a container next to Share":   {"http://share.example.test", "172.18.0.3:50000", tunnel},
	} {
		rec, cookies := unlockAt(t, e.app.Handler, tc.origin, pin.Code, tc.remote, tc.headers)
		refused(t, name, rec, cookies, http.StatusForbidden, "proxy_untrusted")
	}

	// Visitors on the internet may come with proxy headers of their own: over https nothing
	// changes, over plain http they are refused as always.
	rec, cookies := unlockAt(t, e.app.Handler, "https://share.example.test", pin.Code, "203.0.113.9:50000", forwarded)
	if rec.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Name != "__Host-share_pin" {
		t.Errorf("over https from the internet: %d %v", rec.Code, cookies)
	}
	rec, cookies = unlockWeb(t, e.app.Handler, pin.Code, "203.0.113.9:50000", forwarded)
	refused(t, "over plain http from the internet", rec, cookies, http.StatusForbidden, "https_required")

	// A request from this machine without them, like one the website's dev server forwards,
	// is at home as before.
	rec, cookies = unlockWeb(t, e.app.Handler, pin.Code, "127.0.0.1:50000", map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusOK || len(cookies) != 1 || cookies[0].Name != "share_pin" {
		t.Errorf("from this machine: %d %v", rec.Code, cookies)
	}
}

func TestProxyRateLimits(t *testing.T) {
	e := newEnvWith(t, `"proxy": {"headers": "x-forwarded", "trusted_proxies": ["192.168.8.20"]}`)
	e.newPin(db.PinPermanent)
	try := func(forwardedFor string) int {
		rec, _ := unlockWeb(t, e.app.Handler, "22222", "192.168.8.20:50000", map[string]string{"X-Forwarded-For": forwardedFor, "X-Forwarded-Proto": "https"})
		return rec.Code
	}
	// A visitor who makes up another address before each wrong PIN still counts as one: 30
	// wrong tries from one address, and it waits.
	for i := range 29 {
		if code := try(fmt.Sprintf("6.6.6.%d, 203.0.113.9", i)); code != http.StatusUnauthorized {
			t.Fatalf("wrong try %d: %d", i+1, code)
		}
	}
	if code := try("6.6.6.99, 203.0.113.9"); code != http.StatusTooManyRequests {
		t.Errorf("the 30th wrong try: %d", code)
	}
	// And the proxy's other visitors aren't locked out with them.
	if code := try("203.0.113.10"); code != http.StatusUnauthorized {
		t.Errorf("another visitor: %d", code)
	}
}

func TestHealthz(t *testing.T) {
	e := newEnvWith(t, `"proxy": {"headers": "x-forwarded", "trusted_proxies": ["192.168.8.20"]}`)
	// Health checks pass however they arrive: from the internet, or from a proxy that names
	// no visitor.
	for _, remote := range []string{"203.0.113.9:50000", "192.168.8.20:50000", "127.0.0.1:50000"} {
		for _, method := range []string{"GET", "HEAD"} {
			req := httptest.NewRequest(method, "http://share.example.test/healthz", nil)
			req.RemoteAddr = remote
			rec := httptest.NewRecorder()
			e.app.Handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK || method == "GET" && rec.Body.String() != "ok\n" {
				t.Errorf("%s %s: %d %q", method, remote, rec.Code, rec.Body)
			}
		}
	}
	// Anything else on that path is guarded as before.
	req := httptest.NewRequest("POST", "http://share.example.test/healthz", nil)
	req.RemoteAddr = "203.0.113.9:50000"
	rec := httptest.NewRecorder()
	e.app.Handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST from the internet: %d", rec.Code)
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

	e := newEnvWith(t, `"proxy": {"headers": "cloudflare", "trusted_proxies": ["192.168.8.20"]}`)
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
		// Not even for a visitor at home: what comes through a proxy can't be told from what
		// comes from the internet.
		"through the tunnel": ask("192.168.8.20:50000", map[string]string{"CF-Connecting-IP": "192.168.8.30", "Cf-Visitor": `{"scheme":"https"}`},
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
