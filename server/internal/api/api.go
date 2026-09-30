// Package api is the JSON API. Handlers are thin: they decode, call the service that owns
// the rule, and encode. Every response shape has a fixture in contract/api.
package api

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/httpx"
)

// Version is the API version, sent in /api/info so apps can tell what the server speaks.
const Version = 1

// API holds what the handlers need.
type API struct {
	Cfg         *config.Config
	Auth        *auth.Service
	ServerID    string
	MaxFileSize int64 // the effective limit: config and drive together; 0 = none
	Now         func() time.Time
}

// Register adds the API routes to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/info", a.info)
	mux.HandleFunc("POST /api/pin/unlock", a.unlock)
	mux.HandleFunc("GET /api/session", a.session)
	mux.HandleFunc("POST /api/session/end", a.endSession)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such API endpoint.")
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("ok\n"))
	})
}

// Info is the public description of the server.
type Info struct {
	ServerID         string   `json:"server_id"`
	Name             string   `json:"name"`
	APIVersion       int      `json:"api_version"`
	Languages        []string `json:"languages"`
	DefaultLanguage  string   `json:"default_language"`
	ChunkSizeBytes   int64    `json:"chunk_size_bytes"`
	MaxFileSizeBytes int64    `json:"max_file_size_bytes"`
}

func (a *API) info(w http.ResponseWriter, r *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, Info{
		ServerID:         a.ServerID,
		Name:             a.Cfg.Name,
		APIVersion:       Version,
		Languages:        a.Cfg.Languages,
		DefaultLanguage:  a.Cfg.DefaultLanguage,
		ChunkSizeBytes:   a.Cfg.ChunkSize(),
		MaxFileSizeBytes: a.MaxFileSize,
	})
}

// Session describes who the caller is.
type Session struct {
	Kind      string     `json:"kind"`               // "pin"
	PinKind   string     `json:"pin_kind,omitempty"` // "permanent" or "day"
	ExpiresAt *time.Time `json:"expires_at"`         // when a 24-hour PIN ends; null otherwise
}

type unlockRequest struct {
	Code   string `json:"code"`
	Client string `json:"client"`
}

// UnlockResponse is the answer to a correct PIN. The app gets the token in the body; the
// website gets it as an HttpOnly cookie instead.
type UnlockResponse struct {
	Token        string  `json:"token,omitempty"`
	Session      Session `json:"session"`
	MovedUploads int64   `json:"moved_uploads"`
}

func (a *API) unlock(w http.ResponseWriter, r *http.Request) {
	var req unlockRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Client == "" {
		req.Client = "web"
	}
	res, err := a.Auth.Unlock(r.Context(), r, req.Code, req.Client)
	var wrong *auth.WrongPINError
	var locked *auth.LockedError
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrPINFormat):
		httpx.WriteError(w, http.StatusBadRequest, "pin_format", "A PIN has 5 letters or numbers.")
		return
	case errors.As(err, &locked):
		httpx.WriteErrorDetail(w, http.StatusTooManyRequests, httpx.ErrorDetail{
			Code: "pin_locked", Message: "Too many wrong PINs. Wait a moment and try again.",
			RetryAfterSeconds: httpx.Seconds(locked.RetryAfter),
		})
		return
	case errors.As(err, &wrong) && wrong.Ended:
		httpx.WriteErrorDetail(w, http.StatusUnauthorized, httpx.ErrorDetail{
			Code: "pin_ended", Message: "This PIN doesn't work any more.", AttemptsLeft: &wrong.AttemptsLeft,
		})
		return
	case errors.As(err, &wrong):
		httpx.WriteErrorDetail(w, http.StatusUnauthorized, httpx.ErrorDetail{
			Code: "pin_wrong", Message: "That PIN didn't work.", AttemptsLeft: &wrong.AttemptsLeft,
		})
		return
	default:
		log.Printf("api: unlock: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
		return
	}

	resp := UnlockResponse{
		Session:      Session{Kind: auth.KindPin, PinKind: res.Session.Pin.Kind, ExpiresAt: res.ExpiresAt},
		MovedUploads: res.Moved,
	}
	if req.Client == "app" {
		resp.Token = res.Token
	} else {
		auth.SetSessionCookie(w, res.Token, res.ExpiresAt, a.Now())
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (a *API) session(w http.ResponseWriter, r *http.Request) {
	p, err := a.Auth.Authenticate(r.Context(), r)
	if err != nil {
		httpx.WriteAuthError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Session{Kind: p.Kind, PinKind: p.PinKind, ExpiresAt: p.PinExpiresAt})
}

func (a *API) endSession(w http.ResponseWriter, r *http.Request) {
	p, err := a.Auth.Authenticate(r.Context(), r)
	if err != nil && !errors.Is(err, auth.ErrSessionEnded) {
		httpx.WriteAuthError(w, err)
		return
	}
	if p != nil {
		if err := a.Auth.EndSession(r.Context(), p); err != nil {
			log.Printf("api: ending session: %v", err)
		}
	}
	auth.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}
