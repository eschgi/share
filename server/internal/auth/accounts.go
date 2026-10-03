package auth

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"slices"
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
	ErrSignedOut     = errors.New("this phone or browser was signed out")
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

// WrongPasswordError is a wrong current password when changing it. It is ErrPasswordWrong,
// with how many tries are left before a pause.
type WrongPasswordError struct{ AttemptsLeft int }

func (e *WrongPasswordError) Error() string {
	return fmt.Sprintf("%v, %d tries left", ErrPasswordWrong, e.AttemptsLeft)
}

func (e *WrongPasswordError) Unwrap() error { return ErrPasswordWrong }

// InputError is a field with an unusable value.
type InputError struct{ Field, Problem string }

func (e *InputError) Error() string { return e.Field + " " + e.Problem }

// SignedIn is a phone or browser that just signed in: its token, shown once, its person and
// itself.
type SignedIn struct {
	Token  string
	User   db.User
	Device db.Device
}

// checkClient accepts the two kinds of devices: the app, and a browser.
func checkClient(client string) error {
	if client != db.ClientApp && client != db.ClientWeb {
		return &InputError{"client", "must be web or app"}
	}
	return nil
}

// Login signs a phone or browser in with a username and password. client is db.ClientApp or
// db.ClientWeb.
func (s *Service) Login(ctx context.Context, r *http.Request, username, password, deviceName, client string) (*SignedIn, error) {
	if err := checkClient(client); err != nil {
		return nil, err
	}
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
	token, keyHash := ids.NewToken(ids.PrefixDevice)
	dv := newDevice(r, u.ID, deviceName, client, now)
	if err := s.DB.InsertDevice(ctx, dv, keyHash); err != nil {
		return nil, err
	}
	s.browserSignedIn(ctx, r, dv, now)
	return &SignedIn{Token: token, User: u, Device: dv}, nil
}

// newDevice is a phone or browser about to be signed in for userID. A browser signing in at
// home gets a session that works only at home (AtHome).
func newDevice(r *http.Request, userID, name, client string, now time.Time) db.Device {
	fallback := "Phone"
	if client == db.ClientWeb {
		fallback = "Browser"
	}
	return db.Device{
		ID: ids.New(), UserID: userID, Name: cleanName(name, fallback), Client: client,
		HomeOnly: client == db.ClientWeb && AtHome(r), CreatedAt: now, LastSeenAt: now,
	}
}

// browserSignedIn tidies up after a browser signed in: the session it had before ends, so
// sessions don't pile up and none can be planted, and what it was still sending with a PIN
// continues as the person's, instead of starting over. The website drops the PIN cookie.
// Nothing here can undo the sign-in, so failures are only logged.
func (s *Service) browserSignedIn(ctx context.Context, r *http.Request, dv db.Device, now time.Time) {
	if dv.Client != db.ClientWeb {
		return
	}
	if err := s.EndBrowserSession(ctx, r); err != nil {
		log.Printf("auth: ending the browser's earlier session: %v", err)
	}
	c, err := r.Cookie(cookieName(r, CookiePin))
	if err != nil || !ids.TokenHasPrefix(c.Value, ids.PrefixPinSession) {
		return
	}
	sess, err := s.DB.PinSessionByToken(ctx, ids.HashToken(c.Value))
	if err != nil {
		return
	}
	if _, err := s.DB.MoveUploadsToPerson(ctx, sess.ID, dv.UserID, dv.ID, now); err != nil {
		log.Printf("auth: handing the browser's uploads to %s: %v", dv.UserID, err)
		return
	}
	if err := s.DB.RevokePinSession(ctx, sess.ID, now); err != nil {
		log.Printf("auth: ending the browser's PIN session: %v", err)
	}
}

// EndBrowserSession signs out the browser whose account cookie a request carries, whatever
// state its session is in. Without such a cookie it does nothing.
func (s *Service) EndBrowserSession(ctx context.Context, r *http.Request) error {
	c, err := r.Cookie(cookieName(r, CookieSession))
	if err != nil || !ids.TokenHasPrefix(c.Value, ids.PrefixDevice) {
		return nil
	}
	dv, _, err := s.DB.DeviceByToken(ctx, ids.HashToken(c.Value))
	if errors.Is(err, db.ErrNotFound) || (err == nil && dv.Client != db.ClientWeb) {
		return nil // gone already, or an app's key that has no business in a cookie
	}
	if err != nil {
		return err
	}
	return s.DB.RevokeDevice(ctx, dv.ID, s.Now())
}

// CreateInvite makes an invite and returns its token, which is shown once. For a new person
// give their name, role and, for a member, the folders they get (possibly none, but not nil);
// to add a phone for someone with an account, give their user id. It returns ErrFolderGone
// if one of the folders doesn't exist or is deleted.
func (s *Service) CreateInvite(ctx context.Context, name, role, forUserID, createdBy string, folders []string, lifetime time.Duration) (string, db.Invite, error) {
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
		if role == db.RoleMember {
			if folders == nil {
				return "", in, &InputError{"folders", "are needed for a member"}
			}
			var err error
			if in.Folders, err = s.liveFolders(ctx, folders); err != nil {
				return "", in, err
			}
		}
	}
	token, hash := ids.NewToken(ids.PrefixInvite)
	if err := s.DB.InsertInvite(ctx, in, hash); err != nil {
		return "", in, err
	}
	return token, in, nil
}

// liveFolders checks that every folder exists and isn't deleted.
func (s *Service) liveFolders(ctx context.Context, folderIDs []string) ([]string, error) {
	out := []string{}
	for _, id := range folderIDs {
		f, err := s.DB.FolderByID(ctx, id)
		if errors.Is(err, db.ErrNotFound) || (err == nil && f.DeletedAt != nil) {
			return nil, ErrFolderGone
		}
		if err != nil {
			return nil, err
		}
		if !slices.Contains(out, f.ID) {
			out = append(out, f.ID)
		}
	}
	return out, nil
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

// AcceptInvite uses an invite: the phone or browser is signed in, as a new person or as the
// one the invite adds a phone or browser for.
func (s *Service) AcceptInvite(ctx context.Context, r *http.Request, token, deviceName, client string) (*SignedIn, error) {
	if err := checkClient(client); err != nil {
		return nil, err
	}
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
	dv := newDevice(r, u.ID, deviceName, client, now)
	if err := s.DB.UseInvite(ctx, in.ID, u, dv, hash, now); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return nil, ErrInviteUsed // someone was quicker, a moment ago
		}
		return nil, err
	}
	s.browserSignedIn(ctx, r, dv, now)
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
	token, _, err := s.CreateInvite(ctx, "Admin", db.RoleAdmin, "", FirstStart, nil, firstStartLifetime)
	return token, err
}

// SignOut signs the calling phone or browser out.
func (s *Service) SignOut(ctx context.Context, p *Principal) error {
	if p.Kind != KindDevice {
		return ErrDevicesOnly
	}
	return s.DB.RevokeDevice(ctx, p.DeviceID, s.Now())
}

// DeleteAccount removes the caller's account and signs all their phones and browsers out. The
// files they sent stay. It returns db.ErrLastAdmin for the last admin.
func (s *Service) DeleteAccount(ctx context.Context, p *Principal) error {
	if p.Kind != KindDevice {
		return ErrDevicesOnly
	}
	return s.DB.DeleteUser(ctx, p.UserID)
}

var usernamePattern = regexp.MustCompile(`^[\p{L}\p{N}._-]{3,32}$`)

// CheckUsername tells whether a username can be used (contract/usernames.json).
func CheckUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return &InputError{"username", "needs 3 to 32 letters, digits, dots, dashes or underscores"}
	}
	return nil
}

// SetPassword gives the caller a username and password, for signing in on other phones and in
// browsers. With a password already set, the current one is needed.
func (s *Service) SetPassword(ctx context.Context, p *Principal, username, current, password string) error {
	if p.Kind != KindDevice {
		return ErrDevicesOnly
	}
	u, err := s.DB.UserByID(ctx, p.UserID)
	if err != nil {
		return err
	}
	username = strings.TrimSpace(username)
	if err := CheckUsername(username); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(password); n < 8 || n > 200 {
		return &InputError{"password", "needs at least 8 characters"}
	}
	if u.PasswordHash != "" {
		now, key := s.Now(), "user:"+u.ID
		if blocked, retry := s.passwordPerUser.Check(key, now); blocked {
			return &LockedError{RetryAfter: retry}
		}
		ok, err := CheckPassword(ctx, u.PasswordHash, current)
		if err != nil {
			return err
		}
		if !ok {
			left, blocked := s.passwordPerUser.Fail(key, now)
			if blocked > 0 {
				return &LockedError{RetryAfter: blocked}
			}
			return &WrongPasswordError{AttemptsLeft: left}
		}
		s.passwordPerUser.Reset(key)
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

// ResetPassword makes up a new password for someone who forgot theirs, for an admin to hand
// over, and returns it with the username to sign in with. username changes the person's
// username; empty keeps theirs, and someone without one needs it. Their phones and browsers
// stay signed in, and pauses after wrong passwords end, so the new one works at once.
func (s *Service) ResetPassword(ctx context.Context, userID, username string) (string, string, error) {
	u, err := s.DB.UserByID(ctx, userID)
	if err != nil {
		return "", "", err
	}
	if username = strings.TrimSpace(username); username == "" {
		username = u.Username
	}
	if username == "" {
		return "", "", &InputError{"username", "is needed: this person has none yet"}
	}
	if err := CheckUsername(username); err != nil {
		return "", "", err
	}
	password := NewPassword()
	hash, err := HashPassword(ctx, password)
	if err != nil {
		return "", "", err
	}
	if err := s.DB.SetLogin(ctx, u.ID, username, hash); errors.Is(err, db.ErrConflict) {
		return "", "", ErrUsernameTaken
	} else if err != nil {
		return "", "", err
	}
	for _, name := range []string{u.Username, username} {
		if name != "" {
			s.loginPerUser.Reset("user:" + strings.ToLower(name))
		}
	}
	s.passwordPerUser.Reset("user:" + u.ID)
	return username, password, nil
}

// authenticateDevice checks a phone's or browser's key; viaCookie says it came in the
// website's account cookie, which only browsers' keys belong in.
func (s *Service) authenticateDevice(ctx context.Context, r *http.Request, token string, viaCookie bool) (*Principal, error) {
	dv, u, err := s.DB.DeviceByToken(ctx, ids.HashToken(token))
	if errors.Is(err, db.ErrNotFound) {
		return nil, ErrSignedOut // revoked with the person's account, or never valid
	}
	if err != nil {
		return nil, err
	}
	if dv.RevokedAt != nil || (viaCookie && dv.Client != db.ClientWeb) {
		return nil, ErrSignedOut
	}
	now := s.Now()
	if dv.HomeOnly && !AtHome(r) {
		// Browsers send this cookie only to the address at home. Seen anywhere else, it was
		// taken there, e.g. by a device with the same address on another network: it ends.
		log.Printf("auth: the browser %s, signed in at home, was used from elsewhere; signing it out", dv.ID)
		if err := s.DB.RevokeDevice(ctx, dv.ID, now); err != nil {
			return nil, err
		}
		return nil, ErrSignedOut
	}
	if dv.Client == db.ClientWeb && now.Sub(dv.LastSeenAt) > WebSessionIdle {
		return nil, ErrSignedOut
	}
	p := &Principal{
		Kind: KindDevice, UserID: u.ID, DeviceID: dv.ID, Role: u.Role,
		Client: dv.Client, HomeOnly: dv.HomeOnly, ViaCookie: viaCookie,
	}
	if now.Sub(dv.LastSeenAt) >= touchEvery {
		if err := s.DB.TouchDevice(ctx, dv.ID, now); err != nil {
			return nil, err
		}
		p.RenewCookie = viaCookie && !sameDay(dv.LastSeenAt, now)
	}
	return p, nil
}

// sameDay reports whether a and b fall on the same day in UTC.
func sameDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()
	return ay == by && am == bm && ad == bd
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
