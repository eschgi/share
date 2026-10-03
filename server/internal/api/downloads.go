package api

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"io"
	"net/http"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/checksum"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/storage"
	"github.com/eschgi/share/server/internal/zipstream"
)

// Several files as one download: the website asks for a selection and learns its size and
// paths, then fetches it with a plain link, so the browser's own download list shows it and
// resumes it after a dropped connection. Saving into a folder uses the same paths, file by
// file.

// maxDownloadFiles is how many files one ZIP may have; for more, go day by day.
const maxDownloadFiles = 10_000

type downloadRequest struct {
	IDs []string `json:"ids"`
}

// DownloadFile is one file of a download, with its path inside the ZIP or the folder.
type DownloadFile struct {
	ID   string `json:"id"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// DownloadInfo is a ZIP ready to be fetched from GET /api/downloads/{id}.
type DownloadInfo struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Size  int64          `json:"size"`
	Count int            `json:"count"`
	Files []DownloadFile `json:"files"` // in the archive's order
}

func (a *API) createDownload(w http.ResponseWriter, r *http.Request) {
	p, folders, ok := a.viewer(w, r)
	if !ok {
		return
	}
	var req downloadRequest
	if !httpx.DecodeJSONLimit(w, r, &req, 512<<10) {
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxDownloadFiles {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", fmt.Sprintf("Send between 1 and %d ids.", maxDownloadFiles))
		return
	}
	for _, id := range req.IDs {
		if !ids.Valid(id) {
			httpx.WriteError(w, http.StatusBadRequest, "bad_request", "That isn't a file id: "+id)
			return
		}
	}
	files, err := a.Auth.DB.ReadyFiles(r.Context(), req.IDs)
	if err != nil {
		internal(w, "download", err)
		return
	}
	files = inFolders(files, folders)
	if len(files) == 0 {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "None of these files is in the library.")
		return
	}
	dirs := zipDirs(files, folders)
	zipOrder(files, dirs)
	z, err := a.zipOf(files, dirs)
	if err != nil {
		internal(w, "download", err)
		return
	}
	sel := a.Downloads.Add(p.Key(), z.ids(), a.zipName(files, folders), dirs != nil)
	info := DownloadInfo{ID: sel.ID, Name: sel.Name, Size: z.archive.Size(), Count: len(files), Files: make([]DownloadFile, len(files))}
	for i, f := range files {
		info.Files[i] = DownloadFile{ID: f.ID, Path: z.paths[i], Size: f.Size}
	}
	httpx.WriteJSON(w, http.StatusCreated, info)
}

// download sends a selection as one ZIP. Its bytes depend only on the files' names, sizes and
// times, so http.ServeContent can answer Range and If-Range like for a single file; a file
// deleted meanwhile is left out, which changes the ETag, so a resumed download starts over.
func (a *API) download(w http.ResponseWriter, r *http.Request) {
	p, folders, ok := a.viewer(w, r)
	if !ok {
		return
	}
	sel, ok := a.Downloads.Get(r.PathValue("id"), p.Key())
	if !ok {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such download, or it's over a day old. Choose the files again.")
		return
	}
	files, err := a.Auth.DB.ReadyFiles(r.Context(), sel.FileIDs)
	if err != nil {
		internal(w, "download", err)
		return
	}
	files = inFolders(files, folders)
	if len(files) == 0 {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "None of these files is in the library any more.")
		return
	}
	var dirs map[string]string
	if sel.ByFolder {
		dirs = folderDirs(folders)
	}
	z, err := a.zipOf(files, dirs)
	if err != nil {
		internal(w, "download", err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", contentDisposition(sel.Name))
	h.Set("Cache-Control", "private, no-store, no-transform")
	h.Set("Content-Security-Policy", "sandbox")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("ETag", z.etag)
	if r.Method != http.MethodHead {
		release, ok := a.Downloads.Stream(p.Key())
		if !ok {
			h.Set("Retry-After", "30")
			httpx.WriteError(w, http.StatusServiceUnavailable, "busy", "Too many downloads at once; this one can start in a moment.")
			return
		}
		defer release()
	}
	src := &zipSource{ctx: r.Context(), files: files, crcs: a.Checksums, lib: a.Lib}
	reader := z.archive.Reader(r.Context(), src)
	defer reader.Close()
	http.ServeContent(newDeadlineWriter(w), r, "", time.Time{}, reader)
}

// inFolders keeps the files that lie in one of the folders.
func inFolders(files []db.File, folders []db.Folder) []db.File {
	seen := folderDirs(folders)
	out := files[:0]
	for _, f := range files {
		if _, ok := seen[f.FolderID]; ok {
			out = append(out, f)
		}
	}
	return out
}

// folderDirs maps folder ids to their directories.
func folderDirs(folders []db.Folder) map[string]string {
	out := make(map[string]string, len(folders))
	for _, f := range folders {
		out[f.ID] = f.Dir
	}
	return out
}

// zipDirs gives, when the files are from several folders, the directory each one's path in
// the ZIP starts with: its folder's, as on the server's drive. Files from one folder need
// none (nil): their paths start with the day.
func zipDirs(files []db.File, folders []db.Folder) map[string]string {
	for _, f := range files[1:] {
		if f.FolderID != files[0].FolderID {
			return folderDirs(folders)
		}
	}
	return nil
}

// zipPath is where a file goes in a ZIP, before names that differ only in case are numbered.
func zipPath(f db.File, dirs map[string]string) string {
	if dirs == nil {
		return f.RelPath
	}
	return path.Join(dirs[f.FolderID], f.RelPath)
}

// zipOrder puts the files whose checksum is known first, by path, and the others after them,
// the smallest first: the ZIP starts at once, and the missing checksums are worked out while
// the first files go out.
func zipOrder(files []db.File, dirs map[string]string) {
	sort.SliceStable(files, func(i, j int) bool {
		ki, kj := files[i].CRC32 != nil, files[j].CRC32 != nil
		switch {
		case ki != kj:
			return ki
		case ki:
			return zipPath(files[i], dirs) < zipPath(files[j], dirs)
		default:
			return files[i].Size < files[j].Size
		}
	})
}

// zip is the layout of one selection's archive.
type zip struct {
	files   []db.File
	paths   []string
	archive *zipstream.Archive
	etag    string
}

func (z *zip) ids() []string {
	out := make([]string, len(z.files))
	for i, f := range z.files {
		out[i] = f.ID
	}
	return out
}

// zipOf lays out the archive of files, in their order. Each file goes into its day's folder,
// as on the server's drive, inside its folder's directory when dirs has them. Names that
// differ only in case are numbered: the server tells them apart, but Windows and macOS don't.
func (a *API) zipOf(files []db.File, dirs map[string]string) (*zip, error) {
	z := &zip{files: files, paths: make([]string, len(files))}
	taken := map[string]bool{}
	entries := make([]zipstream.Entry, len(files))
	h := sha256.New()
	io.WriteString(h, "z1")
	for i, f := range files {
		p := zipPath(f, dirs)
		dir, name := path.Split(p)
		for n := 2; taken[strings.ToLower(p)]; n++ {
			p = dir + storage.Numbered(name, n)
		}
		taken[strings.ToLower(p)] = true
		z.paths[i] = p
		modified := f.CreatedAt
		if f.ClientModifiedAt != nil {
			modified = *f.ClientModifiedAt
		} else if f.UploadedAt != nil {
			modified = *f.UploadedAt
		}
		entries[i] = zipstream.Entry{Name: p, Size: f.Size, Modified: modified}
		fmt.Fprintf(h, "\x00%s\x00%s\x00%d\x00%d", f.ID, p, f.Size, modified.Unix())
	}
	archive, err := zipstream.New(entries, a.Cfg.Location)
	if err != nil {
		return nil, err
	}
	z.archive = archive
	z.etag = `"z1-` + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(h.Sum(nil)))[:26] + `"`
	return z, nil
}

// zipName is the ZIP's file name: the server's name, or the folder's when the files are all
// from one folder and the caller sees others too, and the day the files arrived, or today if
// they came on several days.
func (a *API) zipName(files []db.File, folders []db.Folder) string {
	day := files[0].UploadDay
	for _, f := range files[1:] {
		if f.UploadDay != day {
			day = storage.Day(a.Now(), a.Cfg.Location)
			break
		}
	}
	name := a.Cfg.Name
	if len(folders) > 1 && zipDirs(files, folders) == nil {
		for _, f := range folders {
			if f.ID == files[0].FolderID {
				name = f.Name
			}
		}
	}
	return storage.SanitizeName(name + " " + day + ".zip")
}

// zipSource gives an archive its files from the library. The first checksum it is asked for
// also starts working out the missing ones after it, in the archive's order, so they are ready
// by the time the archive gets to them.
type zipSource struct {
	ctx   context.Context
	files []db.File
	crcs  *checksum.Store
	lib   *storage.Library
	ahead sync.Once
}

func (z *zipSource) CRC32(ctx context.Context, i int) (uint32, error) {
	z.ahead.Do(func() { go z.readAhead(i) })
	return z.crcs.Get(ctx, z.files[i])
}

func (z *zipSource) readAhead(from int) {
	for k := range z.files {
		f := z.files[(from+k)%len(z.files)]
		if f.CRC32 != nil {
			continue
		}
		if _, err := z.crcs.Get(z.ctx, f); err != nil && z.ctx.Err() != nil {
			return // the download is over
		}
	}
}

func (z *zipSource) Open(i int) (zipstream.File, error) {
	return z.lib.Open(z.ctx, z.files[i])
}
