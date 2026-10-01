package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
)

// signedIn is a phone with an account.
type signedIn struct {
	token  string
	userID string
	body   map[string]any
}

func (e *env) accept(inviteToken, deviceName string) signedIn {
	e.t.Helper()
	r := e.postJSON(nil, "/api/invites/accept", "", map[string]string{"token": inviteToken, "device_name": deviceName}, nil)
	if r.status != http.StatusOK {
		e.t.Fatalf("accept: %d %s", r.status, r.body)
	}
	v := r.json(e.t)
	return signedIn{v["token"].(string), v["user"].(map[string]any)["id"].(string), v}
}

// admin makes the first admin through the first-start invite, as on a new server.
func (e *env) admin() signedIn {
	e.t.Helper()
	token, err := e.app.Auth.FirstStartInvite(context.Background())
	if err != nil || token == "" {
		e.t.Fatalf("first-start invite: %q, %v", token, err)
	}
	return e.accept(token, "Stefan's phone")
}

// invite makes an invite as p.
func (e *env) invite(p signedIn, name, role string) string {
	e.t.Helper()
	token, _, err := e.app.Auth.CreateInvite(context.Background(), name, role, "", p.userID, auth.InviteLifetime)
	if err != nil {
		e.t.Fatal(err)
	}
	return token
}

func TestFirstStartInviteMakesTheAdmin(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	old, _ := e.app.Auth.FirstStartInvite(ctx)
	token, _ := e.app.Auth.FirstStartInvite(ctx) // a restart before anyone used it
	if r := e.postJSON(nil, "/api/invites/peek", "", map[string]string{"token": old}, nil); r.errorCode() != "invite_revoked" {
		t.Errorf("the earlier first-start invite: %d %s, want invite_revoked", r.status, r.body)
	}

	r := e.postJSON(nil, "/api/invites/peek", "", map[string]string{"token": token}, nil)
	assertShape(t, "peek", readFixture(t, "api/invite_peek.json")["response"], r.json(t))
	if v := r.json(t); v["role"] != "admin" || v["inviter"] != nil || v["adds_phone"] != false {
		t.Errorf("peek: %v", v)
	}
	admin := e.accept(token, "Stefan's phone")
	assertShape(t, "accept", readFixture(t, "api/invite_accept.json")["response"], admin.body)

	me := e.do(nil, "GET", "/api/me", admin.token, nil, nil)
	assertShape(t, "me", readFixture(t, "api/me.json")["response"], me.json(t))
	if u := me.json(t)["user"].(map[string]any); u["role"] != "admin" || u["has_password"] != false {
		t.Errorf("me: %v", u)
	}
	if again, _ := e.app.Auth.FirstStartInvite(ctx); again != "" {
		t.Error("another first-start invite once someone has an account")
	}
}

func TestInvitesWorkOnce(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	token := e.invite(admin, "  Maria ", db.RoleMember)
	peek := func(tok string) response {
		return e.postJSON(nil, "/api/invites/peek", "", map[string]string{"token": tok}, nil)
	}
	if v := peek(token).json(t); v["inviter"] != "Admin" || v["name"] != "Maria" || v["role"] != "member" {
		t.Errorf("peek: %v", v)
	}
	maria := e.accept(token, "Pixel 8")
	if u := maria.body["user"].(map[string]any); u["name"] != "Maria" || u["role"] != "member" {
		t.Errorf("accepted as %v", u)
	}

	errs := readFixture(t, "api/invite_errors.json")["cases"].([]any)
	used := e.postJSON(nil, "/api/invites/accept", "", map[string]string{"token": token}, nil)
	if used.status != http.StatusGone || used.errorCode() != "invite_used" {
		t.Errorf("second accept: %d %s", used.status, used.body)
	}
	assertShape(t, "invite_used", errs[1].(map[string]any)["response"], used.json(t))

	late := e.invite(admin, "Peter", db.RoleMember)
	e.clock.Add(auth.InviteLifetime)
	if r := peek(late); r.errorCode() != "invite_expired" {
		t.Errorf("a day later: %d %s", r.status, r.body)
	}
	// Tokens can't be guessed, and trying many unknown ones pauses the address.
	for i := range 20 {
		fake := "shi_" + strings.Repeat("A", 42) + strconv.Itoa(1000+i)
		if r := peek(fake); r.errorCode() != "invite_unknown" && !(i == 19 && r.errorCode() == "invite_locked") {
			t.Fatalf("unknown invite %d: %d %s", i, r.status, r.body)
		}
	}
	if r := peek(late); r.status != http.StatusTooManyRequests || r.errorCode() != "invite_locked" {
		t.Errorf("after 20 unknown invites: %d %s, want 429 invite_locked", r.status, r.body)
	}
}

func TestSignInWithPassword(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	put := func(token string, v any) response {
		b := mustJSON(t, v)
		return e.do(nil, "PUT", "/api/me/password", token, bytes.NewReader(b), map[string]string{"Content-Type": "application/json"})
	}
	if r := put(admin.token, map[string]string{"username": "Stefan", "password": "short"}); r.errorCode() != "bad_request" {
		t.Errorf("short password: %d %s", r.status, r.body)
	}
	if r := put(admin.token, map[string]string{"username": "Stefan", "password": "correct horse"}); r.status != http.StatusNoContent {
		t.Fatalf("set password: %d %s", r.status, r.body)
	}
	if r := put(admin.token, map[string]string{"username": "stefan", "password": "battery staple"}); r.errorCode() != "password_wrong" {
		t.Errorf("changing it without the current one: %d %s", r.status, r.body)
	}

	login := func(user, pass string) response {
		return e.postJSON(nil, "/api/auth/login", "", map[string]string{"username": user, "password": pass, "device_name": "Laptop"}, nil)
	}
	if r := login("stefan", "wrong"); r.status != http.StatusUnauthorized || r.errorCode() != "login_wrong" || r.json(t)["error"].(map[string]any)["attempts_left"] != 9.0 {
		t.Errorf("wrong password: %d %s", r.status, r.body)
	}
	if r := login("nobody", "correct horse"); r.errorCode() != "login_wrong" {
		t.Errorf("unknown user: %d %s", r.status, r.body)
	}
	r := login("STEFAN", "correct horse") // usernames ignore case
	if r.status != http.StatusOK {
		t.Fatalf("login: %d %s", r.status, r.body)
	}
	assertShape(t, "login", readFixture(t, "api/login.json")["response"], r.json(t))
	laptop := r.json(t)["token"].(string)

	if r := e.do(nil, "POST", "/api/auth/logout", laptop, nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("logout: %d %s", r.status, r.body)
	}
	if r := e.do(nil, "GET", "/api/me", laptop, nil, nil); r.status != http.StatusUnauthorized || r.errorCode() != "signed_out" {
		t.Errorf("after logout: %d %s, want 401 signed_out", r.status, r.body)
	}
	if r := e.do(nil, "GET", "/api/me", admin.token, nil, nil); r.status != http.StatusOK {
		t.Errorf("the other phone stays signed in: %d %s", r.status, r.body)
	}

	for range 10 { // the successful login above started the count again
		login("stefan", "wrong")
	}
	if r := login("stefan", "correct horse"); r.status != http.StatusTooManyRequests || r.header.Get("Retry-After") == "" {
		t.Errorf("after 10 wrong passwords: %d %s, want 429 login_locked", r.status, r.body)
	}
}

func TestDeleteAccount(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	maria := e.accept(e.invite(admin, "Maria", db.RoleMember), "Pixel 8")
	if r := e.do(nil, "POST", "/api/me/delete", admin.token, nil, nil); r.status != http.StatusConflict || r.errorCode() != "last_admin" {
		t.Errorf("the only admin: %d %s, want 409 last_admin", r.status, r.body)
	}
	if r := e.do(nil, "POST", "/api/me/delete", maria.token, nil, nil); r.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", r.status, r.body)
	}
	if r := e.do(nil, "GET", "/api/me", maria.token, nil, nil); r.errorCode() != "signed_out" {
		t.Errorf("after deleting the account: %d %s", r.status, r.body)
	}
	if _, err := e.app.DB.UserByID(context.Background(), maria.userID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the account is still there: %v", err)
	}
}

func TestPhonesSeeTheLibraryAndPINsDont(t *testing.T) {
	e := newEnv(t)
	admin := e.admin()
	pin := e.newPin(db.PinPermanent)
	pinToken, _ := e.unlockApp(pin.Code, "")
	up := tus{e, pinToken}
	photo := jpegBytes(t, 64, 48, 1)
	first := up.sendFile("IMG_1.jpg", photo)
	if r := e.putThumb(pinToken, first, "", "image/jpeg", jpegBytes(t, 32, 24, 1)); r.status != http.StatusNoContent {
		t.Fatalf("thumbnail from the uploader: %d %s", r.status, r.body)
	}
	e.clock.Add(time.Minute)
	video := randomBytes(t, 300_000)
	second := up.sendFile("clip.mp4", video)
	e.clock.Add(24 * time.Hour)
	third := up.sendFile("Kündigung.pdf", randomBytes(t, 1000))

	get := func(path, token string, headers map[string]string) response {
		return e.do(nil, "GET", path, token, nil, headers)
	}
	for _, path := range []string{"/api/library", "/api/files", "/api/files/ids", "/api/files/" + first, "/api/files/" + first + "/content"} {
		if r := get(path, pinToken, nil); r.status != http.StatusForbidden || r.errorCode() != "forbidden" {
			t.Errorf("PIN session on %s: %d %s, want 403", path, r.status, r.body)
		}
	}

	lib := get("/api/library", admin.token, nil)
	assertShape(t, "library", readFixture(t, "api/library.json")["response"], lib.json(t))
	days := lib.json(t)["days"].([]any)
	if len(days) != 2 || days[0].(map[string]any)["day"] != "2026-09-28" || days[1].(map[string]any)["count"] != 2.0 {
		t.Errorf("days: %v", days)
	}
	if v := get("/api/library?kind=video", admin.token, nil).json(t)["days"].([]any); len(v) != 1 {
		t.Errorf("videos: %v", v)
	}

	page := get("/api/files?limit=2", admin.token, nil).json(t)
	assertShape(t, "files", readFixture(t, "api/files.json")["response"], page)
	files := page["files"].([]any)
	if len(files) != 2 || files[0].(map[string]any)["id"] != third || page["next_cursor"] == nil {
		t.Fatalf("first page: %v", page)
	}
	rest := get("/api/files?limit=2&cursor="+page["next_cursor"].(string), admin.token, nil).json(t)
	if f := rest["files"].([]any); len(f) != 1 || f[0].(map[string]any)["id"] != first || rest["next_cursor"] != nil {
		t.Errorf("second page: %v", rest)
	}
	if f := get("/api/files?q="+url.QueryEscape("kündig"), admin.token, nil).json(t)["files"].([]any); len(f) != 1 {
		t.Errorf("search: %v", f)
	}
	ids := get("/api/files/ids?day=2026-09-27", admin.token, nil).json(t)
	assertShape(t, "ids", readFixture(t, "api/file_ids.json")["response"], ids)
	if ids["bytes"] != float64(len(photo)+len(video)) || len(ids["ids"].([]any)) != 2 {
		t.Errorf("ids: %v", ids)
	}
	info := get("/api/files/"+second, admin.token, nil).json(t)
	assertShape(t, "file", readFixture(t, "api/file.json")["response"], info)
	if info["kind"] != "video" || info["from"] != nil {
		t.Errorf("file: %v", info)
	}

	// Downloads: whole, resumed with Range, and If-Range falling back to the whole file.
	whole := get("/api/files/"+second+"/content", admin.token, nil)
	if whole.status != http.StatusOK || !bytes.Equal(whole.body, video) || whole.header.Get("ETag") != `"`+second+`"` {
		t.Fatalf("download: %d, %d bytes, ETag %s", whole.status, len(whole.body), whole.header.Get("ETag"))
	}
	if cc := whole.header.Get("Cache-Control"); !strings.Contains(cc, "no-store") || !strings.Contains(cc, "no-transform") {
		t.Errorf("Cache-Control %q", cc)
	}
	if cd := get("/api/files/"+third+"/content", admin.token, nil).header.Get("Content-Disposition"); cd != `attachment; filename="K_ndigung.pdf"; filename*=UTF-8''K%C3%BCndigung.pdf` {
		t.Errorf("Content-Disposition %q", cd)
	}
	part := get("/api/files/"+second+"/content", admin.token, map[string]string{"Range": "bytes=100000-", "If-Range": `"` + second + `"`})
	if part.status != http.StatusPartialContent || !bytes.Equal(part.body, video[100000:]) {
		t.Errorf("Range: %d, %d bytes", part.status, len(part.body))
	}
	changed := get("/api/files/"+second+"/content", admin.token, map[string]string{"Range": "bytes=100000-", "If-Range": `"other"`})
	if changed.status != http.StatusOK || len(changed.body) != len(video) {
		t.Errorf("If-Range with another ETag: %d, %d bytes; want the whole file", changed.status, len(changed.body))
	}

	if r := get("/api/files/"+second+"/thumb", admin.token, nil); r.status != http.StatusNotFound {
		t.Errorf("thumbnail of a file without one: %d", r.status)
	}
	if r := get("/api/files/"+first+"/thumb", admin.token, nil); r.status != http.StatusOK || r.header.Get("Content-Type") != "image/jpeg" {
		t.Errorf("thumbnail: %d %s", r.status, r.header.Get("Content-Type"))
	}
}

func TestHomeAddressWithPinnedCertificate(t *testing.T) {
	e := newEnvWith(t, `"https": {"listen": "127.0.0.1:0"}, "home_url": "https://127.0.0.1:8443"`)
	admin := e.admin()
	srv := e.do(nil, "GET", "/api/server", admin.token, nil, nil).json(t)
	assertShape(t, "server", readFixture(t, "api/server.json")["response"], srv)
	pins := srv["local_cert_sha256"].([]any)
	if srv["local_url"] != "https://127.0.0.1:8443" || len(pins) != 1 || len(pins[0].(string)) != 64 {
		t.Fatalf("server: %v", srv)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	local := &http.Server{Handler: e.app.Handler, TLSConfig: &tls.Config{GetCertificate: e.app.Local.GetCertificate}}
	go local.ServeTLS(ln, "", "")
	t.Cleanup(func() { local.Close() })

	// How the app trusts the address at home: not through a CA, only by the pinned fingerprint.
	pinned := func(want string) *http.Client {
		return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
				sum := sha256.Sum256(raw[0])
				if hex.EncodeToString(sum[:]) != want {
					return errors.New("certificate doesn't match the pin")
				}
				return nil
			},
		}}}
	}
	base := "https://" + ln.Addr().String()
	res, err := pinned(pins[0].(string)).Get(base + "/api/info")
	if err != nil {
		t.Fatalf("pinned: %v", err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("info over the address at home: %d", res.StatusCode)
	}
	if _, err := pinned(strings.Repeat("0", 64)).Get(base + "/api/info"); err == nil {
		t.Error("a wrong pin was accepted")
	}
	res, err = pinned(pins[0].(string)).Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Errorf("the website over the https port: %d", res.StatusCode)
	}

	// A PIN session learns the address at home too, when it unlocks in the app.
	pin := e.newPin(db.PinDay)
	_, unlocked := e.unlockApp(pin.Code, "")
	if s := unlocked["server"].(map[string]any); s["local_url"] != "https://127.0.0.1:8443" {
		t.Errorf("unlock: %v", s)
	}
}

func TestAPKFromTheServer(t *testing.T) {
	dir := t.TempDir()
	apk := filepath.Join(dir, "share.apk")
	data := randomBytes(t, 5000)
	os.WriteFile(apk, data, 0o644)
	os.WriteFile(apk+".json", []byte(`{"version_code": 3, "version_name": "0.3.0"}`), 0o644)
	e := newEnvWith(t, `"app": {"apk_file": "`+apk+`", "android_package": "com.eschgi.share", "link_scheme": "com.eschgi.share"}`)

	info := e.do(nil, "GET", "/api/app", "", nil, nil).json(t)
	assertShape(t, "app", readFixture(t, "api/app.json")["response"], info)
	sum := sha256.Sum256(data)
	if a := info["apk"].(map[string]any); a["sha256"] != hex.EncodeToString(sum[:]) || a["version_code"] != 3.0 || a["size"] != 5000.0 {
		t.Errorf("apk: %v", a)
	}
	r := e.do(nil, "GET", "/download/share.apk", "", nil, nil)
	if r.status != http.StatusOK || !bytes.Equal(r.body, data) || r.header.Get("Content-Type") != "application/vnd.android.package-archive" {
		t.Errorf("download: %d, %d bytes, %s", r.status, len(r.body), r.header.Get("Content-Type"))
	}
	if v := newEnv(t).do(nil, "GET", "/api/app", "", nil, nil).json(t); v["apk"] != nil {
		t.Errorf("without an APK: %v", v)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
