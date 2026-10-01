package api

import (
	"errors"
	"log"
	"net/http"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
)

// homeProof shows a phone that the server at its address at home is its own, before the phone
// sends its key there over plain http. Only over plain http from a home network: through
// Cloudflare's tunnel an impostor somewhere else could fetch an answer from this server and
// pass it on as its own.
func (a *API) homeProof(w http.ResponseWriter, r *http.Request) {
	if !auth.PlainHTTP(r) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "Only at home, over plain http.")
		return
	}
	var req struct {
		Device string `json:"device"`
		Nonce  string `json:"nonce"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Device == "" || len(req.Nonce) < 16 || len(req.Nonce) > 128 {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Send a device id and a nonce of 16 to 128 characters.")
		return
	}
	hash, err := a.Auth.DB.DeviceTokenHash(r.Context(), req.Device)
	if errors.Is(err, db.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such phone.")
		return
	}
	if err != nil {
		log.Printf("api: home proof: %v", err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"proof": auth.HomeProof(hash, req.Nonce)})
}
