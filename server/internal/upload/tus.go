// Package upload receives files over tus. tusd does the protocol; this package puts a guard
// in front of it (authentication, ownership, tombstones for finished uploads, load limits)
// and hooks that tie uploads to the library.
package upload

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"
	xslog "golang.org/x/exp/slog"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/storage"
)

// BasePath is where tus lives.
const BasePath = "/tus/"

// Config holds the limits.
type Config struct {
	MaxFileSize   int64 // bytes; 0 = no limit
	MinFreeSpace  int64 // bytes that must stay free on the drive
	MaxUnfinished int   // unfinished uploads per principal
	PerPrincipal  int   // PATCH requests at once per principal
	Global        int   // PATCH requests at once in total
	QueueWait     time.Duration
	// FreeSpace reports free bytes on the storage drive; nil asks the drive. Tests replace it.
	FreeSpace func() (int64, error)
}

// DefaultConfig is sized for a small machine: a few parallel streams per phone, not too many
// overall.
func DefaultConfig() Config {
	return Config{MaxUnfinished: 200, PerPrincipal: 4, Global: 16, QueueWait: 15 * time.Second}
}

// TusHandler serves BasePath.
type TusHandler struct {
	cfg  Config
	auth *auth.Service
	lib  *storage.Library
	db   *db.DB
	now  func() time.Time
	free func() (int64, error) // free bytes on the storage drive

	routes http.Handler
	global chan struct{}
	mu     sync.Mutex
	per    map[string]chan struct{}
}

// NewTusHandler builds the tus handler.
func NewTusHandler(cfg Config, a *auth.Service, lib *storage.Library, now func() time.Time) (*TusHandler, error) {
	store := filestore.New(lib.Layout.UploadsDir())
	composer := handler.NewStoreComposer()
	composer.UseCore(store)
	composer.UseTerminater(store)
	memorylocker.New().UseIn(composer)

	h := &TusHandler{
		cfg: cfg, auth: a, lib: lib, db: lib.DB, now: now,
		free:   cfg.FreeSpace,
		global: make(chan struct{}, cfg.Global),
		per:    map[string]chan struct{}{},
	}
	if h.free == nil {
		h.free = func() (int64, error) {
			info, err := storage.Stat(lib.Layout.StorageDir)
			return info.Free, err
		}
	}
	tus, err := handler.NewUnroutedHandler(handler.Config{
		BasePath:                   BasePath,
		StoreComposer:              composer,
		MaxSize:                    cfg.MaxFileSize,
		DisableDownload:            true,
		DisableConcatenation:       true,
		Cors:                       &handler.CorsConfig{Disable: true},
		NetworkTimeout:             60 * time.Second,
		Logger:                     xslog.New(xslog.NewTextHandler(log.Writer(), &xslog.HandlerOptions{Level: xslog.LevelWarn})),
		PreUploadCreateCallback:    h.preCreate,
		PreFinishResponseCallback:  h.preFinish,
		PreUploadTerminateCallback: h.preTerminate,
	})
	if err != nil {
		return nil, err
	}
	// tusd reads the upload id from the path with the base path already removed, just like
	// its own routed handler, which is meant to sit behind http.StripPrefix.
	routes := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch id := strings.Trim(r.URL.Path, "/"); {
		case id == "" && r.Method == http.MethodPost:
			tus.PostFile(w, r)
		case id != "" && r.Method == http.MethodHead:
			tus.HeadFile(w, r)
		case id != "" && r.Method == http.MethodPatch:
			tus.PatchFile(w, r)
		case id != "" && r.Method == http.MethodDelete:
			tus.DelFile(w, r)
		default:
			httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Not a tus request.")
		}
	})
	h.routes = http.StripPrefix(BasePath, tus.Middleware(routes))
	return h, nil
}

// ServeHTTP is the guard in front of tusd.
func (h *TusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// tusd would turn a POST into a PATCH or DELETE after our method checks.
	r.Header.Del("X-HTTP-Method-Override")
	w.Header().Set("Cache-Control", "no-store")

	if r.Method == http.MethodOptions {
		h.routes.ServeHTTP(w, r) // capability discovery; tusd answers it without touching uploads
		return
	}
	p, err := h.auth.Authenticate(r.Context(), r)
	if err != nil {
		httpx.WriteAuthError(w, r, err)
		return
	}
	ctx := auth.WithPrincipal(r.Context(), p)
	r = r.WithContext(ctx)

	id := strings.TrimPrefix(r.URL.Path, BasePath)
	switch r.Method {
	case http.MethodPost:
		if id != "" {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such upload.")
			return
		}
	case http.MethodHead, http.MethodPatch, http.MethodDelete:
		f, ok := h.ownedUpload(w, r, p, id)
		if !ok {
			return
		}
		if f.State == db.StateReady || f.State == db.StateTrashed {
			h.tombstone(w, r, f)
			return
		}
	default:
		w.Header().Set("Allow", "POST, HEAD, PATCH, DELETE, OPTIONS")
		httpx.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Not a tus request.")
		return
	}

	if r.Method == http.MethodPatch {
		release, ok := h.acquire(ctx, p.Key())
		if !ok {
			w.Header().Set("Retry-After", "5")
			httpx.WriteError(w, http.StatusServiceUnavailable, "busy", "The server is busy with other uploads; this one will continue shortly.")
			return
		}
		defer release()
	}

	rec := &recorder{ResponseWriter: w}
	h.routes.ServeHTTP(rec, r)
	if r.Method == http.MethodPatch && rec.status == http.StatusNoContent {
		if off, err := strconv.ParseInt(rec.Header().Get("Upload-Offset"), 10, 64); err == nil {
			if err := h.db.SetReceived(context.WithoutCancel(ctx), id, off, h.now()); err != nil {
				log.Printf("upload %s: recording progress: %v", id, err)
			}
		}
	}
}

// ownedUpload loads the caller's upload for id. Someone else's upload looks exactly like a
// missing one. An upload whose bytes have all arrived is finished first, so that a client
// whose final response got lost learns it is done instead of starting over.
func (h *TusHandler) ownedUpload(w http.ResponseWriter, r *http.Request, p *auth.Principal, id string) (db.File, bool) {
	ctx := r.Context()
	if !ids.Valid(id) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such upload.")
		return db.File{}, false
	}
	f, err := h.db.FileByID(ctx, id)
	if err == nil && !p.Owns(f) {
		err = db.ErrNotFound
	}
	if errors.Is(err, db.ErrNotFound) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such upload.")
		return db.File{}, false
	}
	if err != nil {
		log.Printf("upload %s: %v", id, err)
		httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
		return db.File{}, false
	}
	if f.State == db.StateReceiving || f.State == db.StateFinalizing {
		switch err := h.lib.Finalize(ctx, id); {
		case err == nil:
			if f, err = h.db.FileByID(ctx, id); err != nil {
				httpx.WriteError(w, http.StatusInternalServerError, "internal", "Something went wrong on the server.")
				return db.File{}, false
			}
		case errors.Is(err, storage.ErrIncomplete):
		default:
			log.Printf("upload %s: finishing: %v", id, err)
		}
	}
	return f, true
}

// tombstone answers tus requests for an upload that is already in the library: it is
// complete, whatever the client remembers.
func (h *TusHandler) tombstone(w http.ResponseWriter, r *http.Request, f db.File) {
	size := strconv.FormatInt(f.Size, 10)
	w.Header().Set("Tus-Resumable", "1.0.0")
	w.Header().Set("Upload-Offset", size)
	w.Header().Set("Upload-Length", size)
	switch r.Method {
	case http.MethodHead:
		w.WriteHeader(http.StatusOK)
	case http.MethodPatch:
		if r.Header.Get("Upload-Offset") == size {
			io.Copy(io.Discard, io.LimitReader(r.Body, 1)) // nothing more is expected
			w.WriteHeader(http.StatusNoContent)
			return
		}
		httpx.WriteError(w, http.StatusConflict, "offset_mismatch", "This upload is already complete.")
	case http.MethodDelete:
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "This file is already in the library.")
	}
}

// acquire waits for a free PATCH slot for the principal and overall.
func (h *TusHandler) acquire(ctx context.Context, key string) (release func(), ok bool) {
	h.mu.Lock()
	sem := h.per[key]
	if sem == nil {
		sem = make(chan struct{}, h.cfg.PerPrincipal)
		h.per[key] = sem
	}
	h.mu.Unlock()

	timer := time.NewTimer(h.cfg.QueueWait)
	defer timer.Stop()
	select {
	case sem <- struct{}{}:
	case <-timer.C:
		return nil, false
	case <-ctx.Done():
		return nil, false
	}
	select {
	case h.global <- struct{}{}:
	case <-timer.C:
		<-sem
		return nil, false
	case <-ctx.Done():
		<-sem
		return nil, false
	}
	return func() { <-h.global; <-sem }, true
}

// PruneQueues forgets the slot counters of principals with nothing running.
func (h *TusHandler) PruneQueues() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for k, sem := range h.per {
		if len(sem) == 0 {
			delete(h.per, k)
		}
	}
}

func (h *TusHandler) preCreate(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	ctx := hook.Context
	p, ok := auth.FromContext(ctx)
	if !ok {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusUnauthorized, "unauthorized", "Enter a PIN or sign in first.")
	}
	info := hook.Upload
	if info.SizeIsDeferred {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusBadRequest, "length_required", "The file size must be known when the upload starts.")
	}
	if h.cfg.MaxFileSize > 0 && info.Size > h.cfg.MaxFileSize {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusRequestEntityTooLarge, "too_large", "This file is larger than the server accepts.")
	}
	free, err := h.free()
	if err != nil {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, err
	}
	outstanding, err := h.db.OutstandingBytes(ctx)
	if err != nil {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, err
	}
	if free-outstanding-h.cfg.MinFreeSpace < info.Size {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusRequestEntityTooLarge, "no_space", "The server's drive is full.")
	}
	n, err := h.db.UnfinishedCount(ctx, p.PinSessionID, p.UserID)
	if err != nil {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, err
	}
	if n >= h.cfg.MaxUnfinished {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusForbidden, "too_many_uploads", "Too many unfinished uploads; let some finish first.")
	}

	folderID, err := h.folderFor(ctx, p, info.MetaData["folder"])
	if errors.Is(err, errNoFolder) {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusForbidden, "no_folder", "There is no folder you could send into.")
	}
	if errors.Is(err, errFolderGone) {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusNotFound, "folder_gone", "That folder is gone, or you don't see it any more.")
	}
	if errors.Is(err, errFolderUnsaid) {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, tusError(http.StatusBadRequest, "bad_request", "Say which folder the file goes into (Upload-Metadata folder).")
	}
	if err != nil {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, err
	}

	name := storage.SanitizeName(firstNonEmpty(info.MetaData["filename"], info.MetaData["name"]))
	fileType := firstNonEmpty(info.MetaData["filetype"], info.MetaData["type"])
	now := h.now()
	f := db.File{
		ID: ids.New(), Name: name, Size: info.Size, Kind: storage.GuessKind(name), FolderID: folderID,
		CreatedAt: now, UpdatedAt: now, ClientModifiedAt: parseMillis(info.MetaData["lastModified"]),
		PinID: p.PinID, PinSessionID: p.PinSessionID, UserID: p.UserID, DeviceID: p.DeviceID,
	}
	if err := h.db.InsertReceiving(ctx, f); err != nil {
		return handler.HTTPResponse{}, handler.FileInfoChanges{}, err
	}
	return handler.HTTPResponse{}, handler.FileInfoChanges{
		ID:       f.ID,
		MetaData: handler.MetaData{"filename": name, "filetype": fileType},
	}, nil
}

var (
	errNoFolder     = errors.New("no folder to send into")
	errFolderGone   = errors.New("that folder is gone, or not the sender's")
	errFolderUnsaid = errors.New("no folder said")
)

// folderFor is the folder an upload goes into: a PIN's into the PIN's folder, someone signed
// in into the one they chose, among those they see.
func (h *TusHandler) folderFor(ctx context.Context, p *auth.Principal, chosen string) (string, error) {
	if p.Kind == auth.KindPin {
		if p.PinFolderID == "" {
			return "", errNoFolder
		}
		return p.PinFolderID, nil
	}
	if chosen == "" {
		return "", errFolderUnsaid
	}
	var folders []db.Folder
	var err error
	if p.Role == db.RoleAdmin {
		folders, err = h.db.LiveFolders(ctx)
	} else {
		folders, err = h.db.FoldersOf(ctx, p.UserID)
	}
	if err != nil {
		return "", err
	}
	if len(folders) == 0 {
		return "", errNoFolder
	}
	for _, f := range folders {
		if f.ID == chosen {
			return f.ID, nil
		}
	}
	return "", errFolderGone
}

func (h *TusHandler) preFinish(hook handler.HookEvent) (handler.HTTPResponse, error) {
	if err := h.lib.Finalize(hook.Context, hook.Upload.ID); err != nil {
		log.Printf("upload %s: finishing: %v", hook.Upload.ID, err)
		return handler.HTTPResponse{}, tusError(http.StatusInternalServerError, "finalize_failed", "The file arrived but couldn't be stored; the upload will retry.")
	}
	return handler.HTTPResponse{Header: handler.HTTPHeader{"Share-File-Id": hook.Upload.ID}}, nil
}

func (h *TusHandler) preTerminate(hook handler.HookEvent) (handler.HTTPResponse, error) {
	return handler.HTTPResponse{}, h.db.DeleteFileRow(hook.Context, hook.Upload.ID)
}

// tusError is a tusd error with our JSON error body.
func tusError(status int, code, message string) handler.Error {
	body, _ := json.Marshal(map[string]httpx.ErrorDetail{"error": {Code: code, Message: message}})
	return handler.Error{
		ErrorCode: code,
		Message:   message,
		HTTPResponse: handler.HTTPResponse{
			StatusCode: status,
			Body:       string(body),
			Header:     handler.HTTPHeader{"Content-Type": "application/json"},
		},
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseMillis(v string) *time.Time {
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return nil
	}
	t := time.UnixMilli(n).UTC()
	return &t
}

// recorder notes the response status and rewrites tusd's absolute Location into a path.
// Behind cloudflared tusd would build http:// URLs, and clients resolve the path against the
// address they are using anyway (the app switches between local and public).
type recorder struct {
	http.ResponseWriter
	status int
}

func (rw *recorder) WriteHeader(code int) {
	if rw.status == 0 {
		rw.status = code
		if loc := rw.Header().Get("Location"); loc != "" {
			if u, err := url.Parse(loc); err == nil && u.Path != "" {
				rw.Header().Set("Location", u.Path)
			}
		}
	}
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *recorder) Write(b []byte) (int, error) {
	if rw.status == 0 {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Unwrap lets tusd's http.ResponseController reach the connection for its deadlines.
func (rw *recorder) Unwrap() http.ResponseWriter { return rw.ResponseWriter }
