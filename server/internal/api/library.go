package api

import (
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/storage"
)

// FileInfo is a file in the library.
type FileInfo struct {
	ID         string    `json:"id"`
	Folder     string    `json:"folder"` // the id of the folder it lies in
	Name       string    `json:"name"`
	Size       int64     `json:"size"`
	Mime       string    `json:"mime"`
	Kind       string    `json:"kind"`
	Day        string    `json:"day"`
	UploadedAt time.Time `json:"uploaded_at"`
	UpdatedAt  time.Time `json:"updated_at"` // changes with the thumbnail, for caches
	Width      *int64    `json:"width"`
	Height     *int64    `json:"height"`
	DurationMS *int64    `json:"duration_ms"`
	HasThumb   bool      `json:"has_thumb"`
	From       *string   `json:"from"` // who sent it, if they have an account; null for a PIN
}

func fileInfo(f db.File, names map[string]string) FileInfo {
	info := FileInfo{
		ID: f.ID, Folder: f.FolderID, Name: f.Name, Size: f.Size, Mime: f.Mime, Kind: f.Kind, Day: f.UploadDay, UpdatedAt: f.UpdatedAt,
		Width: f.Width, Height: f.Height, DurationMS: f.DurationMS,
		HasThumb: f.Thumb == db.ThumbClient || f.Thumb == db.ThumbServer,
	}
	if f.UploadedAt != nil {
		info.UploadedAt = *f.UploadedAt
	}
	if name, ok := names[f.UserID]; ok {
		info.From = &name
	}
	return info
}

// userNames maps user ids to names, for the "from" of files.
func (a *API) userNames(r *http.Request) (map[string]string, error) {
	users, err := a.Auth.DB.Users(r.Context())
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(users))
	for _, u := range users {
		names[u.ID] = u.Name
	}
	return names, nil
}

// filter reads folder, kind, q and day from the query. Without a folder it shows all the
// folders the caller sees; a folder they don't see is answered with 404.
func filter(w http.ResponseWriter, r *http.Request, folders []db.Folder) (db.LibraryFilter, bool) {
	q := r.URL.Query()
	f := db.LibraryFilter{Kind: q.Get("kind"), Query: strings.TrimSpace(q.Get("q")), Day: q.Get("day")}
	if want := q.Get("folder"); want != "" {
		if !slices.ContainsFunc(folders, func(x db.Folder) bool { return x.ID == want }) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such folder.")
			return f, false
		}
		f.Folders = []string{want}
	} else {
		f.Folders = folderIDs(folders)
	}
	switch f.Kind {
	case "", db.KindPhoto, db.KindVideo, db.KindDocument:
	default:
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "kind must be photo, video or document.")
		return f, false
	}
	if f.Day != "" {
		if _, err := time.Parse(time.DateOnly, f.Day); err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "day must look like 2026-09-27.")
			return f, false
		}
	}
	if len(f.Query) > 200 {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "q is too long.")
		return f, false
	}
	return f, true
}

// Day is one upload day of the library.
type Day struct {
	Day   string `json:"day"`
	Count int    `json:"count"`
	Bytes int64  `json:"bytes"`
}

// Library is the overview: the version (it grows with every change) and the days.
type Library struct {
	Version int64 `json:"version"`
	Days    []Day `json:"days"`
}

func (a *API) library(w http.ResponseWriter, r *http.Request) {
	_, folders, ok := a.viewer(w, r)
	if !ok {
		return
	}
	f, ok := filter(w, r, folders)
	if !ok {
		return
	}
	f.Day = ""
	version, err := a.Auth.DB.LibraryVersion(r.Context())
	if err != nil {
		internal(w, "library", err)
		return
	}
	days, err := a.Auth.DB.LibraryDays(r.Context(), f)
	if err != nil {
		internal(w, "library", err)
		return
	}
	lib := Library{Version: version, Days: []Day{}}
	for _, d := range days {
		lib.Days = append(lib.Days, Day{Day: d.Day, Count: d.Count, Bytes: d.Bytes})
	}
	httpx.WriteJSON(w, http.StatusOK, lib)
}

// FilePage is one page of files, newest first; next_cursor continues it, null at the end.
type FilePage struct {
	Files      []FileInfo `json:"files"`
	NextCursor *string    `json:"next_cursor"`
}

func (a *API) files(w http.ResponseWriter, r *http.Request) {
	p, folders, ok := a.viewer(w, r)
	if !ok {
		return
	}
	f, ok := filter(w, r, folders)
	if !ok {
		return
	}
	limit := 200
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "limit must be 1 to 500.")
			return
		}
		limit = n
	}
	var after *db.Position
	if c := r.URL.Query().Get("cursor"); c != "" {
		p, err := decodeCursor(c)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "That cursor isn't one of ours.")
			return
		}
		after = &p
	}
	files, err := a.Auth.DB.LibraryFiles(r.Context(), f, after, limit+1)
	if err != nil {
		internal(w, "files", err)
		return
	}
	names, err := a.senderNames(r, p)
	if err != nil {
		internal(w, "files", err)
		return
	}
	page := FilePage{Files: []FileInfo{}}
	for i, file := range files {
		if i == limit {
			c := encodeCursor(files[i-1])
			page.NextCursor = &c
			break
		}
		page.Files = append(page.Files, fileInfo(file, names))
	}
	httpx.WriteJSON(w, http.StatusOK, page)
}

func encodeCursor(f db.File) string {
	var at int64
	if f.UploadedAt != nil {
		at = f.UploadedAt.UnixMilli()
	}
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(at, 10) + "." + f.ID))
}

func decodeCursor(c string) (db.Position, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return db.Position{}, err
	}
	at, id, ok := strings.Cut(string(raw), ".")
	ms, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || !ids.Valid(id) {
		return db.Position{}, errors.New("bad cursor")
	}
	return db.Position{UploadedAt: time.UnixMilli(ms).UTC(), ID: id}, nil
}

// FileIDs are all matching files, for "Select all", with their total size.
type FileIDs struct {
	IDs   []string `json:"ids"`
	Bytes int64    `json:"bytes"`
}

func (a *API) fileIDs(w http.ResponseWriter, r *http.Request) {
	_, folders, ok := a.viewer(w, r)
	if !ok {
		return
	}
	f, ok := filter(w, r, folders)
	if !ok {
		return
	}
	list, total, err := a.Auth.DB.LibraryIDs(r.Context(), f)
	if err != nil {
		internal(w, "file ids", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, FileIDs{IDs: list, Bytes: total})
}

// readyFile finds a file in the library for someone who may see it, answering the request if
// it can't.
func (a *API) readyFile(w http.ResponseWriter, r *http.Request) (*auth.Principal, db.File, bool) {
	return a.libraryFile(w, r, false)
}

// libraryFile loads the file a request is about, if it is in a folder the caller sees. With
// trash, admins also get deleted files: Recently deleted shows their thumbnails.
func (a *API) libraryFile(w http.ResponseWriter, r *http.Request, trash bool) (*auth.Principal, db.File, bool) {
	p, folders, ok := a.viewer(w, r)
	if !ok {
		return nil, db.File{}, false
	}
	id := r.PathValue("id")
	if !ids.Valid(id) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such file.")
		return nil, db.File{}, false
	}
	f, err := a.Auth.DB.FileByID(r.Context(), id)
	seen := slices.ContainsFunc(folders, func(x db.Folder) bool { return x.ID == f.FolderID })
	visible := (f.State == db.StateReady && seen) || (trash && f.State == db.StateTrashed && p.Role == db.RoleAdmin)
	if errors.Is(err, db.ErrNotFound) || (err == nil && !visible) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such file.")
		return nil, f, false
	}
	if err != nil {
		internal(w, "file", err)
		return nil, f, false
	}
	return p, f, true
}

func (a *API) file(w http.ResponseWriter, r *http.Request) {
	p, f, ok := a.readyFile(w, r)
	if !ok {
		return
	}
	names, err := a.senderNames(r, p)
	if err != nil {
		internal(w, "file", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, fileInfo(f, names))
}

// content sends a file as it was uploaded. Downloads resume with Range (and If-Range, the ETag
// being the file id), and nothing along the way may cache or change them.
func (a *API) content(w http.ResponseWriter, r *http.Request) {
	_, f, ok := a.readyFile(w, r)
	if !ok {
		return
	}
	file, err := a.Lib.OpenFile(r.Context(), f)
	if errors.Is(err, os.ErrNotExist) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "The file is missing on the server's drive.")
		return
	}
	if err != nil {
		internal(w, "download", err)
		return
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		internal(w, "download", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", f.Mime)
	if f.Mime == "" {
		h.Set("Content-Type", "application/octet-stream")
	}
	h.Set("Content-Disposition", storage.ContentDisposition(f.Name))
	h.Set("Cache-Control", "private, no-store, no-transform")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", `"`+f.ID+`"`)
	http.ServeContent(newDeadlineWriter(w), r, "", st.ModTime(), file)
}

func (a *API) thumb(w http.ResponseWriter, r *http.Request) {
	_, f, ok := a.libraryFile(w, r, true)
	if !ok {
		return
	}
	if f.Thumb != db.ThumbClient && f.Thumb != db.ThumbServer {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "This file has no thumbnail.")
		return
	}
	file, err := os.Open(a.Thumbs.Path(f.ID))
	if errors.Is(err, os.ErrNotExist) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "This file has no thumbnail.")
		return
	}
	if err != nil {
		internal(w, "thumbnail", err)
		return
	}
	defer file.Close()
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", `"`+f.ID+"-"+strconv.FormatInt(f.UpdatedAt.UnixMilli(), 36)+`"`)
	http.ServeContent(w, r, "", f.UpdatedAt, file)
}

// writeTimeout is how long one piece of a download may take to go out. The server has no
// overall write timeout, since big downloads on slow lines take long; this ends connections
// to phones that stopped reading.
const writeTimeout = 60 * time.Second

type deadlineWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
}

func newDeadlineWriter(w http.ResponseWriter) deadlineWriter {
	return deadlineWriter{w, http.NewResponseController(w)}
}

func (d deadlineWriter) Write(p []byte) (int, error) {
	d.rc.SetWriteDeadline(time.Now().Add(writeTimeout))
	return d.ResponseWriter.Write(p)
}

// ReadFrom sends a file in 4 MiB pieces, with a fresh deadline for each. It keeps the file
// itself in view of the connection, so the kernel's sendfile can still do the copying.
// Anything else, such as a ZIP, goes out piece by piece as it is read.
func (d deadlineWriter) ReadFrom(src io.Reader) (int64, error) {
	limit := int64(-1)
	if lr, ok := src.(*io.LimitedReader); ok {
		limit, src = lr.N, lr.R
	}
	if _, file := src.(*os.File); !file {
		return d.copyRead(src, limit)
	}
	var total int64
	for limit < 0 || total < limit {
		n := int64(4 << 20)
		if limit >= 0 {
			n = min(n, limit-total)
		}
		d.rc.SetWriteDeadline(time.Now().Add(writeTimeout))
		c, err := io.Copy(d.ResponseWriter, &io.LimitedReader{R: src, N: n})
		total += c
		if err != nil || c < n {
			return total, err
		}
	}
	return total, nil
}

// copyRead sends what src gives, 256 KiB at a time, and sets the deadline only once a piece
// has been read: reading may wait, e.g. for a checksum, and that time isn't the line's fault.
func (d deadlineWriter) copyRead(src io.Reader, limit int64) (int64, error) {
	buf := make([]byte, 256<<10)
	var total int64
	for limit < 0 || total < limit {
		b := buf
		if limit >= 0 && int64(len(b)) > limit-total {
			b = b[:limit-total]
		}
		n, rerr := src.Read(b)
		if n > 0 {
			d.rc.SetWriteDeadline(time.Now().Add(writeTimeout))
			w, werr := d.ResponseWriter.Write(b[:n])
			total += int64(w)
			if werr != nil {
				return total, werr
			}
		}
		if errors.Is(rerr, io.EOF) {
			return total, nil
		}
		if rerr != nil {
			return total, rerr
		}
	}
	return total, nil
}
