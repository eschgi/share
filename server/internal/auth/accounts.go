package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

const (
	// InviteLifetime is how long an invite works, once.
	InviteLifetime = 24 * time.Hour
	// FirstStart is who made the invite the server prints when nobody has an account yet.
	FirstStart = "first-start"
	// firstStartLifetime gives the owner a week to use the first invite.
	firstStartLifetime = 7 * 24 * time.Hour
)

// Errors for accounts and invites. The API layer turns them into error codes.
var (
	ErrSignedOut     = errors.New("this phone was signed out")
	ErrInviteUnknown = errors.New("no such invite")
	ErrInviteUsed    = errors.New("this invite was used already")
	ErrInviteExpired = errors.New("this invite has expired")
	ErrInviteRevoked = errors.New("this invite was withdrawn")
	ErrPasswordWrong = errors.New("the current password is wrong")
	ErrUsernameTaken = errors.New("that username is taken")
	ErrDevicesOnly   = errors.New("only signed-in phones can do this")
)

// WrongLoginError is a wrong username or password.
type WrongLoginError struct{ AttemptsLeft int }

func (e *WrongLoginError) Error() string {
	return fmt.Sprintf("wrong username or password, %d tries left", e.AttemptsLeft)
}

// InputError is a field with an unusable value.
type InputError struct{ Field, Problem string }

func (e *InputError) Error() string { return e.Field + " " + e.Problem }

// SignedIn is a phone that just signed in: its token, shown once, its person and itself.
type SignedIn struct {
	Token  string
	User   db.User
	Device db.Device
}

// Login signs a phone in with a username and password.
func (s *Service) Login(ctx context.Context, r *http.Request, username, password, deviceName string) (*SignedIn, error) {
	username = strings.TrimSpace(username)
	now := s.Now()
	userKey := "user:" + strings.ToLower(username)
	ipKey := "ip:" + IPKey(ClientIP(r, s.Proxies, s.ClientIPHeader))
	for _, c := range []struct {
		check func(string, time.Time) (bool, time.Duration)
		key   string
	}{{s.loginPerUser.Check, userKey}, {s.loginPerIP.Check, ipKey}} {
		if blocked, retry := c.check(c.key, now); blocked {
			return nil, &LockedError{RetryAfter: retry}
		}
	}

	u, err := s.DB.UserByUsername(ctx, username)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	hash := u.PasswordHash
	if username == "" || hash == "" { // no such person, or no password: take as long anyway
		hash, u = decoyHash(), db.User{}
	}
	ok, err := CheckPassword(ctx, hash, password)
	if err != nil {
		return nil, err
	}
	if !ok || u.ID == "" {
		left, blocked := s.loginPerUser.Fail(userKey, now)
		leftIP, blockedIP := s.loginPerIP.Fail(ipKey, now)
		if b := max(blocked, blockedIP); b > 0 {
			return nil, &LockedError{RetryAfter: b}
		}
		return nil, &WrongLoginError{AttemptsLeft: min(left, leftIP)}
	}
	s.loginPerUser.Reset(userKey)
	return s.signIn(ctx, u, deviceName, now)
}

func (s *Service) signIn(ctx context.Context, u db.User, deviceName string, now time.Time) (*SignedIn, error) {
	token, hash := ids.NewToken(ids.PrefixDevice)
	dv := db.Device{ID: ids.New(), UserID: u.ID, Name: cleanName(deviceName, "Phone"), CreatedAt: now, LastSeenAt: now}
	if err := s.DB.InsertDevice(ctx, dv, hash); err != nil {
		return nil, err
	}
	return &SignedIn{Token: token, User: u, Device: dv}, nil
}

// CreateInvite makes an invite and returns its token, which is shown once. For a new person
// give their name and role; to add a phone for someone with an account, give their user id.
func (s *Service) CreateInvite(ctx context.Context, name, role, forUserID, createdBy string, lifetime time.Duration) (string, db.Invite, error) {
	now := s.Now()
	in := db.Invite{ID: ids.New(), UserID: forUserID, CreatedBy: createdBy, CreatedAt: now, ExpiresAt: now.Add(lifetime)}
	if forUserID != "" {
		u, err := s.DB.UserByID(ctx, forUserID)
		if err != nil {
			return "", in, err
		}
		in.Name, in.Role = u.Name, u.Role
	} else {
		in.Name, in.Role = cleanName(name, ""), role
		if in.Name == "" {
			return "", in, &InputError{"name", "is needed"}
		}
		if role != db.RoleAdmin && role != db.RoleMember {
			return "", in, &InputError{"role", "must be admin or member"}
		}
	}
	token, hash := ids.NewToken(ids.PrefixInvite)
	if err := s.DB.InsertInvite(ctx, in, hash); err != nil {
		return "", in, err
	}
	return token, in, nil
}

// InviteInfo is what someone sees before accepting an invite.
type InviteInfo struct {
	Invite  db.Invite
	Inviter string // the name of who made it; empty if it came from the server's console
}

// PeekInvite shows what an invite is for, without using it.
func (s *Service) PeekInvite(ctx context.Context, r *http.Request, token string) (*InviteInfo, error) {
	in, err := s.findInvite(ctx, r, token)
	if err != nil {
		return nil, err
	}
	info := &InviteInfo{Invite: in}
	if u, err := s.DB.UserByID(ctx, in.CreatedBy); err == nil {
		info.Inviter = u.Name
	}
	return info, nil
}

// AcceptInvite uses an invite: the phone is signed in, as a new person or as the one the
// invite adds a phone for.
func (s *Service) AcceptInvite(ctx context.Context, r *http.Request, token, deviceName string) (*SignedIn, error) {
	in, err := s.findInvite(ctx, r, token)
	if err != nil {
		return nil, err
	}
	now := s.Now()
	var u db.User
	if in.UserID != "" {
		if u, err = s.DB.UserByID(ctx, in.UserID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				return nil, ErrInviteRevoked // the person was removed a moment ago
			}
			return nil, err
		}
	} else {
		u = db.User{ID: ids.New(), Name: in.Name, Role: in.Role, CreatedAt: now, CreatedBy: in.CreatedBy}
	}
	deviceToken, hash := ids.NewToken(ids.PrefixDevice)
	dv := db.Device{ID: ids.New(), UserID: u.ID, Name: cleanName(deviceName, "Phone"), CreatedAt: now, LastSeenAt: now}
	if err := s.DB.UseInvite(ctx, in.ID, u, dv, hash, now); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return nil, ErrInviteUsed // someone was quicker, a moment ago
		}
		return nil, err
	}
	return &SignedIn{Token: deviceToken, User: u, Device: dv}, nil
}

// findInvite checks an invite token. Unknown tokens count against the caller's address, so
// nobody can try tokens at scale (they're unguessable anyway).
func (s *Service) findInvite(ctx context.Context, r *http.Request, token string) (db.Invite, error) {
	now := s.Now()
	ipKey := "ip:" + IPKey(ClientIP(r, s.Proxies, s.ClientIPHeader))
	if blocked, retry := s.invitePerIP.Check(ipKey, now); blocked {
		return db.Invite{}, &LockedError{RetryAfter: retry}
	}
	unknown := func() (db.Invite, error) {
		if _, b := s.invitePerIP.Fail(ipKey, now); b > 0 {
			return db.Invite{}, &LockedError{RetryAfter: b}
		}
		return db.Invite{}, ErrInviteUnknown
	}
	if !ids.TokenHasPrefix(token, ids.PrefixInvite) {
		return unknown()
	}
	in, err := s.DB.InviteByToken(ctx, ids.HashToken(token))
	if errors.Is(err, db.ErrNotFound) {
		return unknown()
	}
	if err != nil {
		return in, err
	}
	switch {
	case in.UsedAt != nil:
		return in, ErrInviteUsed
	case in.RevokedAt != nil:
		return in, ErrInviteRevoked
	case !now.Before(in.ExpiresAt):
		return in, ErrInviteExpired
	}
	return in, nil
}

// FirstStartInvite makes the invite for the first admin while nobody has an account, and
// returns its token; once someone has, it returns "". An earlier first-start invite is
// replaced, since only its hash was kept and it can't be shown again.
func (s *Service) FirstStartInvite(ctx context.Context) (string, error) {
	n, err := s.DB.UserCount(ctx)
	if err != nil || n > 0 {
		return "", err
	}
	if err := s.DB.RevokeInvitesBy(ctx, FirstStart, s.Now()); err != nil {
		return "", err
	}
	token, _, err := s.CreateInvite(ctx, "Admin", db.RoleAdmin, "", FirstStart, firstStartLifetime)
	return token, err
}

// SignOut signs the calling phone out.
func (s *Service) SignOut(ctx context.Context, p *Principal) error {
	if p.Kind != KindDevice {
		return ErrDevicesOnly
	}
	return s.DB.RevokeDevice(ctx, p.DeviceID, s.Now())
}

// DeleteAccount removes the caller's account and signs all their phones out. The files they
// sent stay. It returns db.ErrLastAdmin for the last admin.
func (s *Service) DeleteAccount(ctx context.Context, p *Principal) error {
	if p.Kind != KindDevice {
		return ErrDevicesOnly
	}
	return s.DB.DeleteUser(ctx, p.UserID)
}

var usernamePattern = regexp.MustCompile(`^[\p{L}\p{N}._-]{3,32}$`)

// SetPassword gives the caller a username and password, for signing in on other phones. With
// a password already set, the current one is needed.
func (s *Service) SetPassword(ctx context.Context, p *Principal, username, current, password string) error {
	if p.Kind != KindDevice {
		return ErrDevicesOnly
	}
	u, err := s.DB.UserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if !usernamePattern.MatchString(username) {
		return &InputError{"username", "needs 3 to 32 letters, digits, dots, dashes or underscores"}
	}
	if n := utf8.RuneCountInString(password); n < 8 || n > 200 {
		return &InputError{"password", "needs at least 8 characters"}
	}
	if u.PasswordHash != "" {
		ok, err := CheckPassword(ctx, u.PasswordHash, current)
		if err != nil {
			return err
		}
		if !ok {
			return ErrPasswordWrong
		}
	}
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return err
	}
	if err := s.DB.SetLogin(ctx, u.ID, username, hash); errors.Is(err, db.ErrConflict) {
		return ErrUsernameTaken
	} else if err != nil {
		return err
	}
	return nil
}

// SetLoginFor gives someone a username and password from the server's console.
func (s *Service) SetLoginFor(ctx context.Context, userID, username, password string) error {
	if !usernamePattern.MatchString(username) {
		return &InputError{"username", "needs 3 to 32 letters, digits, dots, dashes or underscores"}
	}
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return err
	}
	if err := s.DB.SetLogin(ctx, userID, username, hash); errors.Is(err, db.ErrConflict) {
		return ErrUsernameTaken
	} else if err != nil {
		return err
	}
	return nil
}

func (s *Service) authenticateDevice(ctx context.Context, token string) (*Principal, error) {
	dv, u, err := s.DB.DeviceByToken(ctx, ids.HashToken(token))
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrSignedOut // revoked with the person's account, or never valid
	}
	if err != nil {
		return nil, err
	}
	if dv.RevokedAt != nil {
		return nil, ErrSignedOut
	}
	now := s.Now()
	if now.Sub(dv.LastSeenAt) >= touchEvery {
		if err := s.DB.TouchDevice(ctx, dv.ID, now); err != nil {
			return nil, err
		}
	}
	return &Principal{Kind: KindDevice, UserID: u.ID, DeviceID: dv.ID, Role: u.Role}, nil
}

// cleanName trims a display name to one line of at most 60 characters.
func cleanName(s, fallback string) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) > 60 {
		s = string([]rune(s)[:60])
	}
	if s == "" {
		return fallback
	}
	return s
}
