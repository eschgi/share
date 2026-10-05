package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/db/dbtest"
	"github.com/eschgi/share/server/internal/ids"
)

func TestNormalizeCodeMatchesContract(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/pin_codes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Alphabet string
		Length   int
		Cases    []struct {
			Input string
			Code  *string
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Alphabet != PinAlphabet || fixture.Length != PinLength {
		t.Fatalf("contract alphabet/length %q/%d differ from the server's", fixture.Alphabet, fixture.Length)
	}
	for _, c := range fixture.Cases {
		got, ok := NormalizeCode(c.Input)
		if c.Code == nil {
			if ok {
				t.Errorf("NormalizeCode(%q) = %q, want no code", c.Input, got)
			}
		} else if !ok || got != *c.Code {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q", c.Input, got, ok, *c.Code)
		}
	}
}

func TestUsernamesMatchContract(t *testing.T) {
	raw, err := os.ReadFile("../../../contract/usernames.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Pattern        string
		Valid, Invalid []string
		Suggestions    []struct{ Name, Username string }
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Pattern != usernamePattern.String() {
		t.Errorf("contract pattern %s, the server's %s", fixture.Pattern, usernamePattern)
	}
	for _, u := range fixture.Valid {
		if err := CheckUsername(u); err != nil {
			t.Errorf("%q: %v, but the contract calls it valid", u, err)
		}
	}
	for _, u := range fixture.Invalid {
		if CheckUsername(u) == nil {
			t.Errorf("%q is accepted, but the contract calls it invalid", u)
		}
	}
	for _, s := range fixture.Suggestions {
		if s.Username != "" && CheckUsername(s.Username) != nil {
			t.Errorf("the suggestion %q for %q can't be used", s.Username, s.Name)
		}
	}
}

func TestNewPassword(t *testing.T) {
	seen := map[string]bool{}
	counts := map[rune]int{}
	for range 20000 {
		p := NewPassword()
		if len(p) != 19 || strings.Count(p, "-") != 3 || p[4] != '-' || p[9] != '-' || p[14] != '-' {
			t.Fatalf("NewPassword() = %q", p)
		}
		for _, c := range strings.ReplaceAll(p, "-", "") {
			if !strings.ContainsRune("23456789abcdefghjkmnpqrstuvwxyz", c) {
				t.Fatalf("%q has %q", p, c)
			}
			counts[c]++
		}
		if seen[p] {
			t.Fatalf("%q twice", p)
		}
		seen[p] = true
	}
	// 320000 characters, 10323 ± 100 of each. With a byte modulo 31, the first 8 would come
	// 11250 times.
	for c, n := range counts {
		if n < 9820 || n > 10820 {
			t.Errorf("%q came %d times of 320000", c, n)
		}
	}
}

func TestGenerateCode(t *testing.T) {
	for range 200 {
		c := GenerateCode()
		if n, ok := NormalizeCode(c); !ok || n != c {
			t.Fatalf("GenerateCode() = %q is not a normalized code", c)
		}
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "[fe80::1%eth0]:5000"
	if got := ClientIP(r).String(); got != "fe80::1" {
		t.Errorf("the peer: ClientIP = %s", got)
	}
	if got := ClientIP(WithClientIP(r, netip.MustParseAddr("203.0.113.7"))).String(); got != "203.0.113.7" {
		t.Errorf("named by a proxy: ClientIP = %s", got)
	}
	if got := IPKey(netip.MustParseAddr("2001:db8:1:2:3:4:5:6")); got != "2001:db8:1:2::/64" {
		t.Errorf("IPKey(v6) = %s", got)
	}
}

func TestForwardedFor(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("172.30.0.0/24")}
	for _, tc := range []struct {
		name  string
		lines []string
		want  string // "" when the header doesn't name the visitor
	}{
		{"one entry", []string{"203.0.113.9"}, "203.0.113.9"},
		{"the visitor's own entry before it", []string{"6.6.6.6, 203.0.113.9"}, "203.0.113.9"},
		{"the proxy's own line after the visitor's", []string{"6.6.6.6", "203.0.113.9"}, "203.0.113.9"},
		{"a trusted hop in between", []string{"6.6.6.6, 203.0.113.9, 172.30.0.5"}, "203.0.113.9"},
		{"only trusted hops", []string{"172.30.0.7, 127.0.0.1"}, "172.30.0.7"},
		{"made up further left", []string{"not-an-ip, 172.30.0.7"}, "172.30.0.7"},
		{"made up on the right", []string{"203.0.113.9, garbage"}, ""},
		{"empty", []string{""}, ""},
		{"none", nil, ""},
		{"v6 with brackets and port", []string{"[2001:db8::1]:443"}, "2001:db8::1"},
		{"v4 with port", []string{"192.0.2.1:5678"}, "192.0.2.1"},
		{"v4 in v6", []string{"::ffff:192.0.2.1"}, "192.0.2.1"},
		{"v6 in brackets", []string{"[2001:db8::2]"}, "2001:db8::2"},
	} {
		got, ok := ForwardedFor(tc.lines, proxies)
		if s := got.String(); ok != (tc.want != "") || ok && s != tc.want {
			t.Errorf("%s: ForwardedFor = %s, %v; want %q", tc.name, s, ok, tc.want)
		}
	}
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time      { return c.t }
func (c *fakeClock) Add(d time.Duration) { c.t = c.t.Add(d) }
func newTestService(t *testing.T) (*Service, *fakeClock) {
	t.Helper()
	d := dbtest.Open(t, t.TempDir())
	clock := &fakeClock{time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}
	if _, _, err := d.EnsureFirstFolder(context.Background(), db.Folder{ID: ids.New(), Name: "Share", CreatedBy: "first-start"}, clock.t); err != nil {
		t.Fatal(err)
	}
	return NewService(d, clock.Now), clock
}

// spec is a PIN of the given kind into the first folder.
func spec(t *testing.T, s *Service, kind string) PinSpec {
	t.Helper()
	live, err := s.DB.LiveFolders(context.Background())
	if err != nil || len(live) == 0 {
		t.Fatalf("LiveFolders = %v, %v", live, err)
	}
	return PinSpec{Kind: kind, FolderID: live[0].ID}
}

// unlockReq is a request from one browser (client id) at one address.
func unlockReq(client, ip string) *http.Request {
	r := httptest.NewRequest("POST", "/api/pin/unlock", nil)
	r.RemoteAddr = net.JoinHostPort(ip, "4000")
	if client != "" {
		r.Header.Set(HeaderClient, client)
	}
	return r
}

func TestUnlockAndAuthenticate(t *testing.T) {
	s, clock := newTestService(t)
	ctx := context.Background()
	pin, err := s.CreatePin(ctx, spec(t, s, db.PinDay), "cli")
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Unlock(ctx, unlockReq("browser-aaaaaaaaaaaa", "203.0.113.1"), strings.ToLower(pin.Code), "web")
	if err != nil {
		t.Fatal(err)
	}
	if res.ExpiresAt == nil || !res.ExpiresAt.Equal(clock.Now().Add(24*time.Hour)) {
		t.Fatalf("a 24-hour PIN's session expires at %v", res.ExpiresAt)
	}
	authed := func() (*Principal, error) {
		r := httptest.NewRequest("GET", "/api/session", nil)
		r.Header.Set("Authorization", "Bearer "+res.Token)
		return s.Authenticate(ctx, r)
	}
	p, err := authed()
	if err != nil || p.Kind != KindPin || p.PinID != pin.ID {
		t.Fatalf("Authenticate = %+v, %v", p, err)
	}
	clock.Add(24*time.Hour - time.Second)
	if _, err := authed(); err != nil {
		t.Fatalf("a second before the PIN ends: %v", err)
	}
	clock.Add(time.Second)
	if _, err := authed(); !errors.Is(err, ErrSessionEnded) {
		t.Fatalf("after 24 hours: err = %v, want ErrSessionEnded", err)
	}
}

func TestEndingAPinEndsItsSessions(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	pin, _ := s.CreatePin(ctx, spec(t, s, db.PinPermanent), "cli")
	res, err := s.Unlock(ctx, unlockReq("", "203.0.113.1"), pin.Code, "app")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EndPin(ctx, pin.ID); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookiePin, Value: res.Token})
	if _, err := s.Authenticate(ctx, r); !errors.Is(err, ErrSessionEnded) {
		t.Fatalf("err = %v, want ErrSessionEnded", err)
	}
	// Trying the ended code again is a wrong try, but says why.
	_, err = s.Unlock(ctx, unlockReq("", "203.0.113.1"), pin.Code, "web")
	var wrong *WrongPINError
	if !errors.As(err, &wrong) || !wrong.Ended {
		t.Fatalf("unlocking an ended PIN: err = %v", err)
	}
}

func TestWrongPinLimits(t *testing.T) {
	s, clock := newTestService(t)
	ctx := context.Background()
	if _, err := s.CreatePin(ctx, spec(t, s, db.PinPermanent), "cli"); err != nil {
		t.Fatal(err)
	}
	phone := "phone-aaaaaaaaaaaaaaaa"
	for i := 1; i <= 4; i++ {
		_, err := s.Unlock(ctx, unlockReq(phone, "203.0.113.1"), "22222", "web")
		var wrong *WrongPINError
		if !errors.As(err, &wrong) || wrong.AttemptsLeft != 5-i {
			t.Fatalf("wrong try %d: err = %v", i, err)
		}
	}
	_, err := s.Unlock(ctx, unlockReq(phone, "203.0.113.1"), "22222", "web")
	var locked *LockedError
	if !errors.As(err, &locked) || locked.RetryAfter != 10*time.Minute {
		t.Fatalf("5th wrong try: err = %v", err)
	}
	// Malformed input doesn't count, and a locked phone stays locked even with a good PIN.
	if _, err := s.Unlock(ctx, unlockReq(phone, "203.0.113.1"), "abc", "web"); !errors.Is(err, ErrPINFormat) {
		t.Fatalf("malformed: err = %v", err)
	}
	// Another guest on the same Wi-Fi is not locked out by that phone.
	if _, err := s.Unlock(ctx, unlockReq("other-guest-aaaaaaaaaa", "203.0.113.1"), "22222", "web"); errors.As(err, &locked) {
		t.Fatal("a second phone behind the same IP is locked by the first one's tries")
	}
	clock.Add(10 * time.Minute)
	if _, err := s.Unlock(ctx, unlockReq(phone, "203.0.113.1"), "22222", "web"); errors.As(err, &locked) {
		t.Fatal("still locked after 10 minutes")
	}
}

func TestUnlockTakesOverUploadsOfAnEndedSession(t *testing.T) {
	s, clock := newTestService(t)
	ctx := context.Background()
	first, _ := s.CreatePin(ctx, spec(t, s, db.PinDay), "cli")
	res, err := s.Unlock(ctx, unlockReq("", "203.0.113.1"), first.Code, "web")
	if err != nil {
		t.Fatal(err)
	}
	f := db.File{ID: ids.New(), Name: "video.mp4", Size: 100, CreatedAt: clock.Now(), UpdatedAt: clock.Now(),
		PinID: first.ID, PinSessionID: res.Session.ID, FolderID: first.FolderID}
	if err := s.DB.InsertReceiving(ctx, f); err != nil {
		t.Fatal(err)
	}
	clock.Add(25 * time.Hour) // the first PIN has ended mid-upload
	second, _ := s.CreatePin(ctx, spec(t, s, db.PinDay), "cli")
	r := unlockReq("", "203.0.113.1")
	r.AddCookie(&http.Cookie{Name: CookiePin, Value: res.Token})
	res2, err := s.Unlock(ctx, r, second.Code, "web")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Moved != 1 {
		t.Fatalf("Moved = %d, want 1", res2.Moved)
	}
	got, _ := s.DB.FileByID(ctx, f.ID)
	if got.PinSessionID != res2.Session.ID {
		t.Fatalf("the upload still belongs to %s", got.PinSessionID)
	}
}

func TestNewCodeEndsTheOldPin(t *testing.T) {
	s, _ := newTestService(t)
	ctx := context.Background()
	old, _ := s.CreatePin(ctx, spec(t, s, db.PinPermanent), "cli")
	fresh, err := s.NewCode(ctx, old.ID, "cli")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Code == old.Code || fresh.Kind != db.PinPermanent {
		t.Fatalf("new PIN %+v", fresh)
	}
	if got, _ := s.FindPin(ctx, strings.ToLower(old.Code)); got.EndedAt == nil {
		t.Fatal("the old code still works")
	}
}
