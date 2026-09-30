// Package httpx has the small HTTP helpers every handler shares: JSON responses and the one
// error format of the API.
package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/eschgi/share/server/internal/auth"
)

// ErrorDetail is the body of every error response: {"error": {"code": …, "message": …}}.
// The codes are listed in contract/errors.json.
type ErrorDetail struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds *int   `json:"retry_after_seconds,omitempty"`
	AttemptsLeft      *int   `json:"attempts_left,omitempty"`
}

// WriteJSON sends v as JSON. API responses are never cached, neither by browsers nor by
// Cloudflare.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("http: writing response: %v", err)
	}
}

// WriteError sends an error in the API format.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteErrorDetail(w, status, ErrorDetail{Code: code, Message: message})
}

// WriteErrorDetail sends an error with optional details. A retry delay also goes into the
// Retry-After header.
func WriteErrorDetail(w http.ResponseWriter, status int, d ErrorDetail) {
	if d.RetryAfterSeconds != nil {
		w.Header().Set("Retry-After", strconv.Itoa(*d.RetryAfterSeconds))
	}
	WriteJSON(w, status, map[string]ErrorDetail{"error": d})
}

// Seconds rounds a duration up to whole seconds, at least 1.
func Seconds(d time.Duration) *int {
	s := max(int((d+time.Second-1)/time.Second), 1)
	return &s
}

// WriteAuthError answers a request that isn't allowed in.
func WriteAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrSessionEnded):
		WriteError(w, http.StatusUnauthorized, "session_ended", "The PIN you used has ended. Enter a new PIN to continue.")
	case errors.Is(err, auth.ErrUnauthorized):
		WriteError(w, http.StatusUnauthorized, "unauthorized", "Enter a PIN or sign in first.")
	default:
		log.Printf("http: authenticating: %v", err)
		WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
	}
}

// MaxJSONBody is the largest JSON request body accepted.
const MaxJSONBody = 64 << 10

// DecodeJSON reads a JSON request body into dst. It requires the JSON content type (which
// browsers can't send cross-site without asking first) and rejects unknown fields. On
// failure it has already answered the request.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		WriteError(w, http.StatusUnsupportedMediaType, "bad_request", "Send the request body as application/json.")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("Invalid request body: %v", err))
		return false
	}
	return true
}
