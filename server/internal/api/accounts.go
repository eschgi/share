package api

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
)

// Server tells an app where the server can be reached. The address at home (home_url, which
// the app calls local_url) is optional: plain http, or https with Share's own certificate,
// whose fingerprints the app pins.
type Server struct {
	PublicURL       string   `json:"public_url"`
	LocalURL        *string  `json:"local_url"`
	LocalCertSHA256 []string `json:"local_cert_sha256"`
}

func (a *API) server() Server {
	s := Server{PublicURL: a.Cfg.PublicURL, LocalCertSHA256: []string{}}
	if a.Cfg.Home != nil {
		s.LocalURL = &a.Cfg.HomeURL
		if a.Cfg.Home.Scheme == "https" && a.Local != nil {
			s.LocalCertSHA256 = []string{a.Local.Fingerprint()}
		}
	}
	return s
}

// UserInfo describes a person to themselves.
type UserInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Role        string  `json:"role"`
	Username    *string `json:"username"`
	HasPassword bool    `json:"has_password"`
}

func userInfo(u db.User) UserInfo {
	info := UserInfo{ID: u.ID, Name: u.Name, Role: u.Role, HasPassword: u.PasswordHash != ""}
	if u.Username != "" {
		info.Username = &u.Username
	}
	return info
}

// DeviceInfo is a signed-in phone or browser.
type DeviceInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Client   string `json:"client"`    // "app" or "web"
	HomeOnly bool   `json:"home_only"` // a browser that signed in at home; it works only there
}

func deviceInfo(dv db.Device) DeviceInfo {
	return DeviceInfo{ID: dv.ID, Name: dv.Name, Client: dv.Client, HomeOnly: dv.HomeOnly}
}

// SignedInResponse is the app's answer to a login or an accepted invite. The token is shown
// once.
type SignedInResponse struct {
	Token  string     `json:"token"`
	User   UserInfo   `json:"user"`
	Device DeviceInfo `json:"device"`
	Server Server     `json:"server"`
}

// signedIn answers a login or an accepted invite. A browser gets its key as an HttpOnly
// cookie, never in the body, and loses its PIN cookie: what it sent with the PIN is the
// person's now.
func (a *API) signedIn(w http.ResponseWriter, r *http.Request, s *auth.SignedIn) {
	if s.Device.Client == db.ClientWeb {
		auth.SetAccountCookie(w, r, s.Token)
		auth.ClearSessionCookie(w, r)
		httpx.WriteJSON(w, http.StatusOK, Me{User: userInfo(s.User), Device: deviceInfo(s.Device)})
		return
	}
	httpx.WriteJSON(w, http.StatusOK, SignedInResponse{
		Token: s.Token, User: userInfo(s.User), Device: deviceInfo(s.Device), Server: a.server(),
	})
}

// clientOf is the client a sign-in is for: the app, unless it says web.
func clientOf(c string) string {
	if c == "" {
		return db.ClientApp
	}
	return c
}

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	DeviceName string `json:"device_name"`
	Client     string `json:"client,omitempty"` // "app" (also when missing) or "web"
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	res, err := a.Auth.Login(r.Context(), r, req.Username, req.Password, req.DeviceName, clientOf(req.Client))
	var wrong *auth.WrongLoginError
	var locked *auth.LockedError
	var input *auth.InputError
	switch {
	case err == nil:
		a.signedIn(w, r, res)
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+input.Field+" "+input.Problem+".")
	case errors.As(err, &locked):
		httpx.WriteErrorDetail(w, http.StatusTooManyRequests, httpx.ErrorDetail{
			Code: "login_locked", Message: "Too many wrong passwords. Wait a moment and try again.",
			RetryAfterSeconds: httpx.Seconds(locked.RetryAfter),
		})
	case errors.As(err, &wrong):
		httpx.WriteErrorDetail(w, http.StatusUnauthorized, httpx.ErrorDetail{
			Code: "login_wrong", Message: "Wrong username or password.", AttemptsLeft: &wrong.AttemptsLeft,
		})
	default:
		internal(w, "login", err)
	}
}

type inviteRequest struct {
	Token string `json:"token"`
}

type acceptRequest struct {
	Token      string `json:"token"`
	DeviceName string `json:"device_name,omitempty"`
	Client     string `json:"client,omitempty"` // "app" (also when missing) or "web"
}

// InvitePeek is what an invite is for, shown before accepting it.
type InvitePeek struct {
	Inviter   *string   `json:"inviter"` // null if it was made on the server's console
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	AddsPhone bool      `json:"adds_phone"` // for someone who has an account already
}

func (a *API) peekInvite(w http.ResponseWriter, r *http.Request) {
	var req inviteRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	info, err := a.Auth.PeekInvite(r.Context(), r, req.Token)
	if err != nil {
		inviteError(w, err)
		return
	}
	peek := InvitePeek{Name: info.Invite.Name, Role: info.Invite.Role, ExpiresAt: info.Invite.ExpiresAt, AddsPhone: info.Invite.UserID != ""}
	if info.Inviter != "" {
		peek.Inviter = &info.Inviter
	}
	httpx.WriteJSON(w, http.StatusOK, peek)
}

func (a *API) acceptInvite(w http.ResponseWriter, r *http.Request) {
	var req acceptRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	res, err := a.Auth.AcceptInvite(r.Context(), r, req.Token, req.DeviceName, clientOf(req.Client))
	var input *auth.InputError
	switch {
	case err == nil:
		a.signedIn(w, r, res)
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+input.Field+" "+input.Problem+".")
	default:
		inviteError(w, err)
	}
}

func inviteError(w http.ResponseWriter, err error) {
	var locked *auth.LockedError
	switch {
	case errors.As(err, &locked):
		httpx.WriteErrorDetail(w, http.StatusTooManyRequests, httpx.ErrorDetail{
			Code: "invite_locked", Message: "Too many unknown invites. Wait a moment and try again.",
			RetryAfterSeconds: httpx.Seconds(locked.RetryAfter),
		})
	case errors.Is(err, auth.ErrInviteUnknown):
		httpx.WriteError(w, http.StatusNotFound, "invite_unknown", "This invite doesn't exist. Check the link.")
	case errors.Is(err, auth.ErrInviteUsed):
		httpx.WriteError(w, http.StatusGone, "invite_used", "This invite was used already. Ask for a new one.")
	case errors.Is(err, auth.ErrInviteExpired):
		httpx.WriteError(w, http.StatusGone, "invite_expired", "This invite has expired. Ask for a new one.")
	case errors.Is(err, auth.ErrInviteRevoked):
		httpx.WriteError(w, http.StatusGone, "invite_revoked", "This invite was withdrawn.")
	default:
		internal(w, "invite", err)
	}
}

// device authenticates a request that only a signed-in phone or browser may make. A browser's
// cookie is set again on the first request of each day, so it doesn't run out while in use.
func (a *API) device(w http.ResponseWriter, r *http.Request) (*auth.Principal, bool) {
	p, err := a.Auth.Authenticate(r.Context(), r)
	if err != nil {
		httpx.WriteAuthError(w, r, err)
		return nil, false
	}
	if p.Kind != auth.KindDevice {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "Sign in to do this; a PIN can only send files.")
		return nil, false
	}
	if p.RenewCookie {
		auth.RenewAccountCookie(w, r)
	}
	return p, true
}

// logout signs the calling phone or browser out. A browser's sign-out always works: whatever
// state its session is in, it ends, and the cookie goes.
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") == "" {
		if err := a.Auth.EndBrowserSession(r.Context(), r); err != nil {
			internal(w, "logout", err)
			return
		}
		auth.ClearAccountCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	if err := a.Auth.SignOut(r.Context(), p); err != nil {
		internal(w, "logout", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Me is the signed-in person and phone or browser.
type Me struct {
	User   UserInfo   `json:"user"`
	Device DeviceInfo `json:"device"`
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	u, err := a.Auth.DB.UserByID(r.Context(), p.UserID)
	if err != nil {
		internal(w, "me", err)
		return
	}
	dv, err := a.Auth.DB.DeviceByID(r.Context(), p.DeviceID)
	if err != nil {
		internal(w, "me", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Me{User: userInfo(u), Device: deviceInfo(dv)})
}

// MyDevices is the caller's own phones and browsers that are signed in, most recently used
// first, so a forgotten one can be signed out without an admin.
type MyDevices struct {
	Devices []PhoneInfo `json:"devices"`
}

func (a *API) myDevices(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	devices, err := a.Auth.DB.DevicesOf(r.Context(), p.UserID)
	if err != nil {
		internal(w, "my devices", err)
		return
	}
	out := MyDevices{Devices: []PhoneInfo{}}
	for _, dv := range devices {
		out.Devices = append(out.Devices, phoneInfo(dv, p.DeviceID))
	}
	httpx.WriteJSON(w, http.StatusOK, out)
}

type passwordRequest struct {
	Username        string `json:"username"`
	Password        string `json:"password"`
	CurrentPassword string `json:"current_password,omitempty"`
}

func (a *API) setPassword(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	var req passwordRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	err := a.Auth.SetPassword(r.Context(), p, req.Username, req.CurrentPassword, req.Password)
	var input *auth.InputError
	var wrong *auth.WrongPasswordError
	var locked *auth.LockedError
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+input.Field+" "+input.Problem+".")
	case errors.As(err, &locked):
		httpx.WriteErrorDetail(w, http.StatusTooManyRequests, httpx.ErrorDetail{
			Code: "login_locked", Message: "Too many wrong passwords. Wait a moment and try again.",
			RetryAfterSeconds: httpx.Seconds(locked.RetryAfter),
		})
	case errors.As(err, &wrong):
		httpx.WriteErrorDetail(w, http.StatusForbidden, httpx.ErrorDetail{
			Code: "password_wrong", Message: "The current password is wrong.", AttemptsLeft: &wrong.AttemptsLeft,
		})
	case errors.Is(err, auth.ErrUsernameTaken):
		httpx.WriteError(w, http.StatusConflict, "username_taken", "Someone else has that username.")
	default:
		internal(w, "password", err)
	}
}

func (a *API) deleteMe(w http.ResponseWriter, r *http.Request) {
	p, ok := a.device(w, r)
	if !ok {
		return
	}
	switch err := a.Auth.DeleteAccount(r.Context(), p); {
	case err == nil:
		if p.ViaCookie {
			auth.ClearAccountCookie(w, r)
		}
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, db.ErrLastAdmin):
		httpx.WriteError(w, http.StatusConflict, "last_admin", "You are the only admin. Make someone else admin first.")
	default:
		internal(w, "delete account", err)
	}
}

func (a *API) serverInfo(w http.ResponseWriter, r *http.Request) {
	if _, err := a.Auth.Authenticate(r.Context(), r); err != nil {
		httpx.WriteAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, a.server())
}

func internal(w http.ResponseWriter, what string, err error) {
	log.Printf("api: %s: %v", what, err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
}
