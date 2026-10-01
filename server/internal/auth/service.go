// Package auth decides who a request is: PIN unlocking and PIN sessions, signed-in phones,
// passwords and invites. It also owns the rate limits for guessing.
package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/ratelimit"
)

// Cookie and header names. Browsers keep __Host- cookies only from secure pages; over plain
// http at home (PlainHTTP) the cookies have the same names without the prefix.
const (
	CookiePin    = "__Host-share_pin" // the PIN session of the website
	CookieClient = "__Host-share_cid" // a random browser id, used only to count wrong PINs
	HeaderClient = "Share-Client"     // the same id, sent by the app
)

// plainName is a cookie's name over plain http: secure cookies can't be set there.
func plainName(name string) string { return strings.TrimPrefix(name, "__Host-") }

// cookieName is the name a cookie has on this request's connection.
func cookieName(r *http.Request, name string) string {
	if PlainHTTP(r) {
		return plainName(name)
	}
	return name
}

type plainKey struct{}

// WithPlainHTTP marks a request that came over plain http, from a home network or this
// machine, so that its cookies are set without Secure.
func WithPlainHTTP(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), plainKey{}, true))
}

// PlainHTTP reports whether a request came over plain http (see WithPlainHTTP).
func PlainHTTP(r *http.Request) bool {
	plain, _ := r.Context().Value(plainKey{}).(bool)
	return plain
}

const (
	// DayPinLifetime is how long a 24-hour PIN works.
	DayPinLifetime = 24 * time.Hour
	// permanentSessionIdle ends a permanent-PIN session nobody used for this long; the
	// cookie itself lasts as long.
	permanentSessionIdle = 400 * 24 * time.Hour
	// touchEvery limits last-seen writes to one per session per interval.
	touchEvery = 10 * time.Minute
)

// Errors returned by Unlock and Authenticate. The API layer turns them into error codes.
var (
	ErrPINFormat    = errors.New("that isn't a PIN")
	ErrUnauthorized = errors.New("not signed in")
	ErrSessionEnded = errors.New("the PIN this session came from has ended")
)

// WrongPINError is a PIN that doesn't exist or no longer works.
type WrongPINError struct {
	AttemptsLeft int
	Ended        bool // the code existed but its PIN has ended
}

func (e *WrongPINError) Error() string {
	if e.Ended {
		return "that PIN has ended"
	}
	return fmt.Sprintf("wrong PIN, %d tries left", e.AttemptsLeft)
}

// LockedError means too many wrong PINs; try again after RetryAfter.
type LockedError struct{ RetryAfter time.Duration }

func (e *LockedError) Error() string {
	return fmt.Sprintf("too many wrong PINs, wait %v", e.RetryAfter)
}

// Service implements authentication against the database.
type Service struct {
	DB             *db.DB
	Now            func() time.Time
	Proxies        []netip.Prefix
	ClientIPHeader string

	// Wrong-PIN limits: per browser or app, per IP address (one guest can't lock out a whole
	// party behind the same router), and overall against distributed guessing.
	perClient *ratelimit.Limiter
	perIP     *ratelimit.Limiter
	global    *ratelimit.Limiter

	// Wrong passwords per username and per IP address, and unknown invites per IP address.
	loginPerUser *ratelimit.Limiter
	loginPerIP   *ratelimit.Limiter
	invitePerIP  *ratelimit.Limiter
}

// NewService returns a Service with the standard limits.
func NewService(d *db.DB, now func() time.Time, proxies []netip.Prefix, clientIPHeader string) *Service {
	return &Service{
		DB: d, Now: now, Proxies: proxies, ClientIPHeader: clientIPHeader,
		perClient: ratelimit.New(5, 10*time.Minute, 10*time.Minute),
		perIP:     ratelimit.New(30, 10*time.Minute, 10*time.Minute),
		global:    ratelimit.New(300, time.Hour, 10*time.Minute),

		loginPerUser: ratelimit.New(10, 10*time.Minute, 10*time.Minute),
		loginPerIP:   ratelimit.New(30, 10*time.Minute, 10*time.Minute),
		invitePerIP:  ratelimit.New(20, 10*time.Minute, 10*time.Minute),
	}
}

// PruneLimits lets the limiters forget old entries.
func (s *Service) PruneLimits() {
	now := s.Now()
	s.perClient.Prune(now)
	s.perIP.Prune(now)
	s.global.Prune(now)
	s.loginPerUser.Prune(now)
	s.loginPerIP.Prune(now)
	s.invitePerIP.Prune(now)
}

// UnlockResult is a new PIN session.
type UnlockResult struct {
	Token     string
	Session   db.PinSession
	ExpiresAt *time.Time // when the session stops working; nil for a permanent PIN
	Moved     int64      // unfinished uploads taken over from an earlier session
}

// Unlock checks a PIN and starts a session for it. client is "web" or "app". If the request
// still carries an earlier session (its PIN may have ended mid-upload), that session's
// unfinished uploads move to the new one so they can continue.
func (s *Service) Unlock(ctx context.Context, r *http.Request, input, client string) (*UnlockResult, error) {
	if client != "web" && client != "app" {
		return nil, fmt.Errorf("client must be web or app")
	}
	code, ok := NormalizeCode(input)
	if !ok {
		return nil, ErrPINFormat
	}
	now := s.Now()
	clientKey := s.clientKey(r)
	ipKey := "ip:" + IPKey(ClientIP(r, s.Proxies, s.ClientIPHeader))

	for _, c := range []struct {
		l   *ratelimit.Limiter
		key string
	}{{s.perClient, clientKey}, {s.perIP, ipKey}, {s.global, "all"}} {
		if c.key == "" {
			continue
		}
		if blocked, retry := c.l.Check(c.key, now); blocked {
			return nil, &LockedError{RetryAfter: retry}
		}
	}

	pin, err := s.DB.PinByCode(ctx, code)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	if err != nil || !pin.LiveAt(now) {
		return nil, s.failure(clientKey, ipKey, now, err == nil)
	}
	if clientKey != "" {
		s.perClient.Reset(clientKey)
	}

	token, hash := ids.NewToken(ids.PrefixPinSession)
	sess := db.PinSession{ID: ids.New(), PinID: pin.ID, Client: client, CreatedAt: now, LastSeenAt: now, Pin: pin}
	if err := s.DB.InsertPinSession(ctx, sess, hash); err != nil {
		return nil, err
	}
	res := &UnlockResult{Token: token, Session: sess, ExpiresAt: pin.ExpiresAt}

	if old := bearerOrCookie(r); old != "" && ids.TokenHasPrefix(old, ids.PrefixPinSession) {
		if prev, err := s.DB.PinSessionByToken(ctx, ids.HashToken(old)); err == nil && prev.ID != sess.ID {
			if res.Moved, err = s.DB.MoveReceivingUploads(ctx, prev.ID, sess.ID, pin.ID, now); err != nil {
				return nil, err
			}
			if err := s.DB.RevokePinSession(ctx, prev.ID, now); err != nil {
				return nil, err
			}
		}
	}
	return res, nil
}

func (s *Service) failure(clientKey, ipKey string, now time.Time, ended bool) error {
	left := -1
	var blockedFor time.Duration
	note := func(l int, b time.Duration) {
		if left < 0 || l < left {
			left = l
		}
		blockedFor = max(blockedFor, b)
	}
	if clientKey != "" {
		note(s.perClient.Fail(clientKey, now))
	}
	note(s.perIP.Fail(ipKey, now))
	if _, b := s.global.Fail("all", now); b > 0 {
		blockedFor = max(blockedFor, b)
	}
	if blockedFor > 0 {
		return &LockedError{RetryAfter: blockedFor}
	}
	return &WrongPINError{AttemptsLeft: left, Ended: ended}
}

// clientKey identifies the browser or app for the per-client limit, if it sent a usable id.
func (s *Service) clientKey(r *http.Request) string {
	if id := ClientID(r); id != "" {
		return "c:" + id
	}
	return ""
}

// ClientID returns the random id the browser (cookie) or app (header) identifies itself with.
func ClientID(r *http.Request) string {
	v := r.Header.Get(HeaderClient)
	if v == "" {
		if c, err := r.Cookie(cookieName(r, CookieClient)); err == nil {
			v = c.Value
		}
	}
	if !validClientID(v) {
		return ""
	}
	return v
}

func validClientID(v string) bool {
	if len(v) < 16 || len(v) > 64 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(v)
	return err == nil
}

// Authenticate returns the principal of a request, from its bearer token or session cookie.
func (s *Service) Authenticate(ctx context.Context, r *http.Request) (*Principal, error) {
	token := bearerOrCookie(r)
	switch {
	case token == "":
		return nil, ErrUnauthorized
	case ids.TokenHasPrefix(token, ids.PrefixPinSession):
		return s.authenticatePin(ctx, token)
	case ids.TokenHasPrefix(token, ids.PrefixDevice):
		return s.authenticateDevice(ctx, token)
	default:
		return nil, ErrUnauthorized
	}
}

func (s *Service) authenticatePin(ctx context.Context, token string) (*Principal, error) {
	sess, err := s.DB.PinSessionByToken(ctx, ids.HashToken(token))
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrUnauthorized
	}
	if err != nil {
		return nil, err
	}
	now := s.Now()
	if sess.RevokedAt != nil || !sess.Pin.LiveAt(now) {
		return nil, ErrSessionEnded
	}
	if sess.Pin.Kind == db.PinPermanent && now.Sub(sess.LastSeenAt) > permanentSessionIdle {
		return nil, ErrSessionEnded
	}
	if now.Sub(sess.LastSeenAt) >= touchEvery {
		if err := s.DB.TouchPinSession(ctx, sess.ID, now); err != nil {
			return nil, err
		}
	}
	return &Principal{
		Kind:         KindPin,
		PinSessionID: sess.ID,
		PinID:        sess.PinID,
		PinKind:      sess.Pin.Kind,
		PinExpiresAt: sess.Pin.ExpiresAt,
	}, nil
}

// EndSession signs a PIN session out.
func (s *Service) EndSession(ctx context.Context, p *Principal) error {
	if p.Kind != KindPin {
		return nil
	}
	return s.DB.RevokePinSession(ctx, p.PinSessionID, s.Now())
}

// bearerOrCookie returns the request's token: an Authorization bearer (the app) or the
// session cookie (the website).
func bearerOrCookie(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		if t, ok := strings.CutPrefix(h, "Bearer "); ok {
			return strings.TrimSpace(t)
		}
		return ""
	}
	if c, err := r.Cookie(cookieName(r, CookiePin)); err == nil {
		return c.Value
	}
	return ""
}

// SetSessionCookie stores a website PIN session. It lasts as long as the session can.
func SetSessionCookie(w http.ResponseWriter, r *http.Request, token string, expiresAt *time.Time, now time.Time) {
	maxAge := int(permanentSessionIdle / time.Second)
	if expiresAt != nil {
		maxAge = max(int(expiresAt.Sub(now)/time.Second), 1)
	}
	http.SetCookie(w, cookie(r, CookiePin, token, maxAge))
}

// ClearSessionCookie removes the website PIN session.
func ClearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, cookie(r, CookiePin, "", -1))
}

// EnsureClientCookie gives a browser its random id for the wrong-PIN limit, once.
func EnsureClientCookie(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName(r, CookieClient)); err == nil && validClientID(c.Value) {
		return
	}
	random, _ := ids.NewToken("")
	http.SetCookie(w, cookie(r, CookieClient, random[:22], int(permanentSessionIdle/time.Second)))
}

// cookie is one of the website's cookies, Secure unless the request came over plain http.
func cookie(r *http.Request, name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name: cookieName(r, name), Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: !PlainHTTP(r), SameSite: http.SameSiteStrictMode,
	}
}
