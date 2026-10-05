package upload

import (
	"context"
	"errors"
	"log"
	"net/http"
	"slices"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/s3"
	"github.com/eschgi/share/server/internal/storage"
)

// firstURLs is how many links for parts the start of an upload comes with.
const firstURLs = 10

// S3Handler takes uploads into a bucket. The client sends the parts to the bucket itself, with
// links from here; this handler starts the multipart upload, says which parts the bucket has,
// and finishes it. Upload ids are file ids, as with tus.
type S3Handler struct {
	admission
	auth   *auth.Service
	lib    *storage.Library
	bucket *s3.Bucket
	now    func() time.Time
}

// NewS3Handler builds the handler for a library in a bucket.
func NewS3Handler(cfg Config, a *auth.Service, lib *storage.Library, now func() time.Time) *S3Handler {
	return &S3Handler{admission: admission{cfg: cfg, db: lib.DB}, auth: a, lib: lib, bucket: lib.S3(), now: now}
}

// Register adds the endpoints under /api/s3/uploads.
func (h *S3Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/s3/uploads", h.guard(h.create))
	mux.HandleFunc("POST /api/s3/uploads/{id}/parts", h.guard(h.owned(h.parts)))
	mux.HandleFunc("GET /api/s3/uploads/{id}", h.guard(h.owned(h.status)))
	mux.HandleFunc("POST /api/s3/uploads/{id}/complete", h.guard(h.owned(h.complete)))
	mux.HandleFunc("DELETE /api/s3/uploads/{id}", h.guard(h.owned(h.cancel)))
}

type s3Handler func(w http.ResponseWriter, r *http.Request, p *auth.Principal, f db.File)

// guard lets only a PIN session or a signed-in phone or browser in, like tus.
func (h *S3Handler) guard(next func(http.ResponseWriter, *http.Request, *auth.Principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		p, err := h.auth.Authenticate(r.Context(), r)
		if err != nil {
			httpx.WriteAuthError(w, r, err)
			return
		}
		next(w, r, p)
	}
}

// owned loads the caller's upload. Someone else's looks exactly like a missing one.
func (h *S3Handler) owned(next s3Handler) func(http.ResponseWriter, *http.Request, *auth.Principal) {
	return func(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
		id := r.PathValue("id")
		if !ids.Valid(id) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such upload.")
			return
		}
		f, err := h.db.FileByID(r.Context(), id)
		if err == nil && !p.Owns(f) {
			err = db.ErrNotFound
		}
		if errors.Is(err, db.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such upload.")
			return
		}
		if err != nil {
			internal(w, id, err)
			return
		}
		next(w, r, p, f)
	}
}

// PartURL is a link for sending one part.
type PartURL struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
}

// S3Upload is a new upload: how it is cut into parts, and the links for the first ones.
type S3Upload struct {
	ID        string    `json:"id"`
	PartSize  int64     `json:"part_size"`
	Parts     int       `json:"parts"`
	URLs      []PartURL `json:"urls"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *S3Handler) create(w http.ResponseWriter, r *http.Request, p *auth.Principal) {
	var req struct {
		Name           string `json:"name"`
		Size           *int64 `json:"size"`
		LastModifiedMS int64  `json:"last_modified_ms"`
		Folder         string `json:"folder"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.Size == nil || *req.Size < 0 || req.Name == "" {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Say the file's name and size.")
		return
	}
	ctx := r.Context()
	size := *req.Size
	folderID, err := h.admit(ctx, p, size, req.Folder, "folder")
	var refusal *Refusal
	if errors.As(err, &refusal) {
		httpx.WriteError(w, refusal.Status, refusal.Code, refusal.Message)
		return
	}
	if err != nil {
		internal(w, "new", err)
		return
	}
	now := h.now()
	name := storage.SanitizeName(req.Name)
	f := db.File{
		ID: ids.New(), Name: name, Size: size, Kind: storage.GuessKind(name), FolderID: folderID,
		CreatedAt: now, UpdatedAt: now, ClientModifiedAt: millis(req.LastModifiedMS),
		PinID: p.PinID, PinSessionID: p.PinSessionID, UserID: p.UserID, DeviceID: p.DeviceID,
		S3PartSize: storage.S3PartSize(size, h.cfg.ChunkSize),
	}
	// The bucket's upload comes first, so a row never waits without one; an empty file needs
	// none and is finished by complete.
	if size > 0 {
		if f.S3UploadID, err = h.lib.StartS3Upload(ctx, f.ID, name); err != nil {
			unavailable(w, f.ID, err)
			return
		}
	}
	if err := h.db.InsertReceiving(ctx, f); err != nil {
		if f.S3UploadID != "" {
			h.bucket.Abort(context.WithoutCancel(ctx), h.bucket.Key(f.ID), f.S3UploadID)
		}
		internal(w, f.ID, err)
		return
	}
	n := storage.S3PartCount(size, f.S3PartSize)
	numbers := make([]int, 0, min(n, firstURLs))
	for i := 1; i <= min(n, firstURLs); i++ {
		numbers = append(numbers, i)
	}
	urls, expires, err := h.partURLs(ctx, f, numbers)
	if err != nil {
		internal(w, f.ID, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, S3Upload{ID: f.ID, PartSize: f.S3PartSize, Parts: n, URLs: urls, ExpiresAt: expires})
}

// PartURLs are fresh links for some parts of an upload.
type PartURLs struct {
	URLs      []PartURL `json:"urls"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *S3Handler) parts(w http.ResponseWriter, r *http.Request, p *auth.Principal, f db.File) {
	var req struct {
		Parts []int `json:"parts"`
	}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if f.State != db.StateReceiving {
		httpx.WriteError(w, http.StatusConflict, "s3_upload_finished", "This upload is finished already.")
		return
	}
	n := storage.S3PartCount(f.Size, f.S3PartSize)
	sorted := slices.Sorted(slices.Values(req.Parts))
	if len(sorted) == 0 || len(sorted) > 100 || sorted[0] < 1 || sorted[len(sorted)-1] > n || len(slices.Compact(sorted)) != len(req.Parts) {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "Ask for 1 to 100 different parts of this upload.")
		return
	}
	urls, expires, err := h.partURLs(r.Context(), f, req.Parts)
	if err != nil {
		internal(w, f.ID, err)
		return
	}
	// Asking for links counts as activity: an upload that is busy isn't dropped as idle.
	if err := h.db.SetReceived(context.WithoutCancel(r.Context()), f.ID, f.Received, h.now()); err != nil {
		log.Printf("upload %s: %v", f.ID, err)
	}
	httpx.WriteJSON(w, http.StatusOK, PartURLs{URLs: urls, ExpiresAt: expires})
}

func (h *S3Handler) partURLs(ctx context.Context, f db.File, numbers []int) ([]PartURL, time.Time, error) {
	urls := make([]PartURL, 0, len(numbers))
	var expires time.Time
	for _, n := range numbers {
		size := storage.S3PartLen(n, f.Size, f.S3PartSize)
		u, exp, err := h.bucket.PartURL(ctx, h.bucket.Key(f.ID), f.S3UploadID, n, size)
		if err != nil {
			return nil, time.Time{}, err
		}
		urls = append(urls, PartURL{Number: n, URL: u, Size: size})
		expires = exp
	}
	if expires.IsZero() {
		expires = time.Now().Add(s3.PartURLExpiry) // an empty file, without parts
	}
	return urls, expires, nil
}

// S3UploadStatus says how far an upload is: which parts the bucket has, or that it is done.
type S3UploadStatus struct {
	ID        string `json:"id"`
	State     string `json:"state"` // receiving, finishing or complete
	Size      int64  `json:"size"`
	PartSize  int64  `json:"part_size"`
	Parts     int    `json:"parts"`
	DoneParts []int  `json:"done_parts"`
}

func (h *S3Handler) status(w http.ResponseWriter, r *http.Request, p *auth.Principal, f db.File) {
	n := storage.S3PartCount(f.Size, f.S3PartSize)
	st := S3UploadStatus{ID: f.ID, State: "receiving", Size: f.Size, PartSize: f.S3PartSize, Parts: n, DoneParts: []int{}}
	all := func() []int {
		done := make([]int, n)
		for i := range done {
			done[i] = i + 1
		}
		return done
	}
	switch f.State {
	case db.StateReady, db.StateTrashed:
		st.State, st.DoneParts = "complete", all()
	case db.StateFinalizing:
		st.State, st.DoneParts = "finishing", all()
	case db.StateReceiving:
		if f.Size == 0 {
			break
		}
		got, err := h.bucket.Parts(r.Context(), h.bucket.Key(f.ID), f.S3UploadID)
		if errors.Is(err, s3.ErrNoUpload) {
			// Finished by a complete whose answer got lost, or dropped by the bucket.
			if !h.finish(w, r, f) {
				return
			}
			st.State, st.DoneParts = "complete", all()
			break
		}
		if err != nil {
			unavailable(w, f.ID, err)
			return
		}
		for i := 1; i <= n; i++ {
			if part, ok := got[i]; ok && part.Size == storage.S3PartLen(i, f.Size, f.S3PartSize) {
				st.DoneParts = append(st.DoneParts, i)
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, st)
}

func (h *S3Handler) complete(w http.ResponseWriter, r *http.Request, p *auth.Principal, f db.File) {
	var req struct{}
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if f.State == db.StateReceiving || f.State == db.StateFinalizing {
		if !h.finish(w, r, f) {
			return
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"id": f.ID})
}

// finish finalizes an upload, also when the client goes away meanwhile; false if it has
// answered the request with why not.
func (h *S3Handler) finish(w http.ResponseWriter, r *http.Request, f db.File) bool {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Minute)
	defer cancel()
	switch err := h.lib.Finalize(ctx, f.ID); {
	case err == nil:
		return true
	case errors.Is(err, storage.ErrIncomplete):
		httpx.WriteError(w, http.StatusConflict, "s3_parts_missing", "Parts of this upload are missing or have the wrong size; ask which, and send them.")
	case errors.Is(err, storage.ErrUploadGone):
		if err := h.lib.Terminate(ctx, f.ID); err != nil && !errors.Is(err, storage.ErrFinished) {
			log.Printf("upload %s: dropping: %v", f.ID, err)
		}
		httpx.WriteError(w, http.StatusNotFound, "not_found", "The bucket doesn't have this upload any more; start the file again.")
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such upload.")
	default:
		unavailable(w, f.ID, err)
	}
	return false
}

func (h *S3Handler) cancel(w http.ResponseWriter, r *http.Request, p *auth.Principal, f db.File) {
	switch err := h.lib.Terminate(context.WithoutCancel(r.Context()), f.ID); {
	case errors.Is(err, storage.ErrFinished):
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "This file is already in the library.")
	case err != nil:
		internal(w, f.ID, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// unavailable answers when the bucket can't help right now; the client tries again.
func unavailable(w http.ResponseWriter, id string, err error) {
	log.Printf("upload %s: the bucket: %v", id, err)
	w.Header().Set("Retry-After", "30")
	httpx.WriteError(w, http.StatusServiceUnavailable, "s3_unavailable", "The bucket can't be reached right now; try again in a while.")
}

func internal(w http.ResponseWriter, id string, err error) {
	log.Printf("upload %s: %v", id, err)
	httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
}
