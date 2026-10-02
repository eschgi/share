// Package api is the JSON API. Handlers are thin: they decode, call the service that owns
// the rule, and encode. Every response shape has a fixture in contract/api.
package api

import (
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/config"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/localtls"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/thumbs"
)

// Version is the API version, sent in /api/info so apps can tell what the server speaks.
const Version = 1

// API holds what the handlers need.
type API struct {
	Cfg         *config.Config
	Auth        *auth.Service
	ServerID    string
	MaxFileSize int64 // the effective limit: config and drive together; 0 = none
	Lib         *storage.Library
	Thumbs      *thumbs.Store
	Local       *localtls.Loader // Share's own certificate on the https port; nil without one
	APK         *APK
	Now         func() time.Time
}

// Register adds the API routes to mux.
func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/info", a.info)
	mux.HandleFunc("GET /api/app", a.appInfo)
	mux.HandleFunc("GET /download/share.apk", a.downloadAPK)

	mux.HandleFunc("POST /api/home/proof", a.homeProof)

	mux.HandleFunc("POST /api/pin/unlock", a.unlock)
	mux.HandleFunc("GET /api/session", a.session)
	mux.HandleFunc("POST /api/session/end", a.endSession)

	mux.HandleFunc("POST /api/auth/login", a.login)
	mux.HandleFunc("POST /api/auth/logout", a.logout)
	mux.HandleFunc("POST /api/invites/peek", a.peekInvite)
	mux.HandleFunc("POST /api/invites/accept", a.acceptInvite)
	mux.HandleFunc("GET /api/me", a.me)
	mux.HandleFunc("GET /api/me/devices", a.myDevices)
	mux.HandleFunc("PUT /api/me/password", a.setPassword)
	mux.HandleFunc("POST /api/me/delete", a.deleteMe)
	mux.HandleFunc("GET /api/server", a.serverInfo)

	mux.HandleFunc("GET /api/library", a.library)
	mux.HandleFunc("GET /api/files", a.files)
	mux.HandleFunc("GET /api/files/ids", a.fileIDs)
	mux.HandleFunc("GET /api/files/{id}", a.file)
	mux.HandleFunc("GET /api/files/{id}/content", a.content)
	mux.HandleFunc("GET /api/files/{id}/thumb", a.thumb)
	mux.HandleFunc("PUT /api/files/{id}/thumb", a.putThumb)
	a.registerAdmin(mux)
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

// Session describes the caller's PIN session; for the app's bearer token it may also say
// "device". A browser's account is /api/me.
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
	Server       *Server `json:"server,omitempty"` // for the app: where it can send
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
	var input *auth.InputError
	switch {
	case err == nil:
	case errors.As(err, &input):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The "+input.Field+" "+input.Problem+".")
		return
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
		srv := a.server()
		resp.Token, resp.Server = res.Token, &srv
	} else {
		auth.SetSessionCookie(w, r, res.Token, res.ExpiresAt, a.Now())
	}
	httpx.WriteJSON(w, http.StatusOK, resp)
}

func (a *API) session(w http.ResponseWriter, r *http.Request) {
	p, err := a.Auth.AuthenticatePin(r.Context(), r)
	if err != nil {
		httpx.WriteAuthError(w, r, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Session{Kind: p.Kind, PinKind: p.PinKind, ExpiresAt: p.PinExpiresAt})
}

func (a *API) endSession(w http.ResponseWriter, r *http.Request) {
	p, err := a.Auth.AuthenticatePin(r.Context(), r)
	if err != nil && !errors.Is(err, auth.ErrSessionEnded) {
		httpx.WriteAuthError(w, r, err)
		return
	}
	if p != nil {
		if err := a.Auth.EndSession(r.Context(), p); err != nil {
			log.Printf("api: ending session: %v", err)
		}
	}
	auth.ClearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// putThumb stores the thumbnail the uploader made of a file it just sent. The query may say
// what the uploader measured on the original: width, height (pixels) and duration_ms.
func (a *API) putThumb(w http.ResponseWriter, r *http.Request) {
	p, err := a.Auth.Authenticate(r.Context(), r)
	if err != nil {
		httpx.WriteAuthError(w, r, err)
		return
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "image/jpeg" {
		httpx.WriteError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Send the thumbnail as image/jpeg.")
		return
	}
	var m thumbs.Meta
	q := r.URL.Query()
	for _, f := range []struct {
		name string
		max  int64
		dst  **int64
	}{
		{"width", 1 << 20, &m.Width},
		{"height", 1 << 20, &m.Height},
		{"duration_ms", 1 << 40, &m.DurationMS},
	} {
		v := q.Get(f.name)
		if v == "" {
			continue
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 || n > f.max {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", f.name+" must be a whole number, 0 or more.")
			return
		}
		*f.dst = &n
	}
	id := r.PathValue("id")
	// Who may send it is clear before the picture is read.
	err = a.Thumbs.CanSend(r.Context(), p, id)
	if err == nil {
		var data []byte
		data, err = io.ReadAll(http.MaxBytesReader(w, r.Body, thumbs.MaxBytes))
		if tooBig := (*http.MaxBytesError)(nil); errors.As(err, &tooBig) {
			httpx.WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "A thumbnail can be at most 512 KiB.")
			return
		}
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "The thumbnail didn't arrive in full.")
			return
		}
		err = a.Thumbs.PutSent(r.Context(), p, id, data, m)
	}
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, thumbs.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such file.")
	case errors.Is(err, thumbs.ErrTooLate):
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "Thumbnails can only be sent within a day of the upload.")
	case errors.Is(err, thumbs.ErrInvalid):
		httpx.WriteError(w, http.StatusBadRequest, "bad_thumbnail", "The thumbnail must be a JPEG of at most 1024×1024 pixels.")
	default:
		log.Printf("api: thumbnail: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "The thumbnail couldn't be stored.")
	}
}
