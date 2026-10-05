package api

import (
	"context"
	"net/http"
	"time"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/storage"
)

// FileURL is a link for fetching a file from the bucket, for anyone who has it until it ends.
type FileURL struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// fileURL answers the link of a file in the bucket, for the app: it fetches and plays the file
// from there without its key. The same people may ask as for /content.
func (a *API) fileURL(w http.ResponseWriter, r *http.Request) {
	_, f, ok := a.readyFile(w, r)
	if !ok {
		return
	}
	link, expires, err := a.bucketLink(r.Context(), f)
	if err != nil {
		internal(w, "link", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, FileURL{URL: link, ExpiresAt: expires})
}

// redirectToBucket sends a browser on to the file's link in the bucket. Nobody may keep the
// answer: the link works for anyone who has it.
func (a *API) redirectToBucket(w http.ResponseWriter, r *http.Request, f db.File) {
	link, _, err := a.bucketLink(r.Context(), f)
	if err != nil {
		internal(w, "download", err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Location", link)
	w.WriteHeader(http.StatusFound)
}

// bucketLink is a link that fetches the file under its own name and type.
func (a *API) bucketLink(ctx context.Context, f db.File) (string, time.Time, error) {
	mime := f.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	return a.S3.GetURL(ctx, a.S3.Key(f.ID), storage.ContentDisposition(f.Name), mime)
}

// storageMode is where the files are, for the clients: "disk" or "s3".
func (a *API) storageMode() string {
	if a.S3 != nil {
		return "s3"
	}
	return "disk"
}
