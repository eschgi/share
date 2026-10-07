package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/e2ee"
	"github.com/eschgi/share/server/internal/httpx"
	"github.com/eschgi/share/server/internal/ids"
	"github.com/eschgi/share/server/internal/storage"
)

// FolderInfo is a folder of the library: what it holds and how many see it.
type FolderInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Files      int       `json:"files"`
	Bytes      int64     `json:"bytes"`
	Senders    int       `json:"senders"`     // the people and PIN sessions that sent its files
	People     int       `json:"people"`      // who sees it: the admins, its members and open invites
	AdminsOnly bool      `json:"admins_only"` // no member sees it
	Cover      *FileInfo `json:"cover"`       // its newest photo or video with a thumbnail
	CreatedAt  time.Time `json:"created_at"`
	Encrypted  bool      `json:"encrypted"`   // new files must be encrypted
	KeyVersion *int      `json:"key_version"` // the newest version of its key; null if it was never encrypted
	// PlainSignature is the root's signature that it sends plain, for its newest key version and
	// its name; null without one.
	PlainSignature B64 `json:"plain_signature"`
}

// Folders lists the folders the caller sees, the oldest first.
type Folders struct {
	Folders []FolderInfo `json:"folders"`
}

// folderCache keeps what the folders hold until the library changes.
type folderCache struct {
	mu      sync.Mutex
	version int64
	stats   map[string]db.FolderStat
	covers  map[string]*db.File
}

// viewer authenticates someone who may look at the library and returns the folders they see:
// admins every folder, members the ones they were given, guests with a PIN that shows its
// folder that folder.
func (a *API) viewer(w http.ResponseWriter, r *http.Request) (*auth.Principal, []db.Folder, bool) {
	p, err := a.Auth.Authenticate(r.Context(), r)
	if err != nil {
		httpx.WriteAuthError(w, r, err)
		return nil, nil, false
	}
	if p.Kind == auth.KindPin && !p.PinShowsFolder {
		httpx.WriteError(w, http.StatusForbidden, "forbidden", "Sign in to do this; a PIN can only send files.")
		return nil, nil, false
	}
	if p.RenewCookie {
		auth.RenewAccountCookie(w, r)
	}
	folders, err := a.visibleFolders(r.Context(), p)
	if err != nil {
		internal(w, "folders", err)
		return nil, nil, false
	}
	return p, folders, true
}

// visibleFolders are the folders p sees, the oldest first.
func (a *API) visibleFolders(ctx context.Context, p *auth.Principal) ([]db.Folder, error) {
	switch {
	case p.Kind == auth.KindPin:
		f, err := a.Auth.DB.FolderByID(ctx, p.PinFolderID)
		if errors.Is(err, db.ErrNotFound) || (err == nil && f.DeletedAt != nil) {
			return nil, nil
		}
		return []db.Folder{f}, err
	case p.Role == db.RoleAdmin:
		return a.Auth.DB.LiveFolders(ctx)
	}
	return a.Auth.DB.FoldersOf(ctx, p.UserID)
}

// senderNames maps user ids to names, for the "from" of files: none for guests, who don't
// learn who sent what.
func (a *API) senderNames(r *http.Request, p *auth.Principal) (map[string]string, error) {
	if p.Kind == auth.KindPin {
		return nil, nil
	}
	return a.userNames(r)
}

func folderIDs(folders []db.Folder) []string {
	out := make([]string, len(folders))
	for i, f := range folders {
		out[i] = f.ID
	}
	return out
}

func (a *API) folders(w http.ResponseWriter, r *http.Request) {
	p, folders, ok := a.viewer(w, r)
	if !ok {
		return
	}
	list, err := a.folderInfos(r, p, folders)
	if err != nil {
		internal(w, "folders", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Folders{Folders: list})
}

// folderInfos describes folders to p: what they hold comes from the cache while the library
// is unchanged; who sees them is counted each time.
func (a *API) folderInfos(r *http.Request, p *auth.Principal, folders []db.Folder) ([]FolderInfo, error) {
	ctx := r.Context()
	stats, covers, err := a.folderContents(ctx, folders)
	if err != nil {
		return nil, err
	}
	people, err := a.Auth.DB.CountFolderPeople(ctx, a.Now())
	if err != nil {
		return nil, err
	}
	names, err := a.senderNames(r, p)
	if err != nil {
		return nil, err
	}
	out := make([]FolderInfo, len(folders))
	for i, f := range folders {
		s := stats[f.ID]
		out[i] = FolderInfo{
			ID: f.ID, Name: f.Name, Files: s.Files, Bytes: s.Bytes, Senders: s.Senders, CreatedAt: f.CreatedAt,
			People: people.Admins + people.Members[f.ID], AdminsOnly: people.Members[f.ID] == 0, Encrypted: f.Encrypted,
			PlainSignature: f.PlainSignature,
		}
		if f.KeyVersion > 0 {
			v := f.KeyVersion
			out[i].KeyVersion = &v
		}
		if c := covers[f.ID]; c != nil {
			info := fileInfo(*c, names)
			out[i].Cover = &info
		}
	}
	return out, nil
}

// folderContents counts the files of every folder and finds the covers of folders, once per
// version of the library.
func (a *API) folderContents(ctx context.Context, folders []db.Folder) (map[string]db.FolderStat, map[string]*db.File, error) {
	c := &a.folderCache
	c.mu.Lock()
	defer c.mu.Unlock()
	version, err := a.Auth.DB.LibraryVersion(ctx)
	if err != nil {
		return nil, nil, err
	}
	if c.stats == nil || c.version != version {
		stats, err := a.Auth.DB.FolderStats(ctx)
		if err != nil {
			return nil, nil, err
		}
		c.version, c.stats, c.covers = version, stats, map[string]*db.File{}
	}
	for _, f := range folders {
		if _, known := c.covers[f.ID]; known {
			continue
		}
		cover, err := a.Auth.DB.FolderCover(ctx, f.ID)
		switch {
		case errors.Is(err, db.ErrNotFound):
			c.covers[f.ID] = nil
		case err != nil:
			return nil, nil, err
		default:
			c.covers[f.ID] = &cover
		}
	}
	return c.stats, c.covers, nil
}

// folderRequest names a folder. Once there is a root, a new folder comes with its id and the
// root's signature of it: its key's first version, or its plain statement (version 0); so does a
// folder that sends plain for its new name.
type folderRequest struct {
	ID             string  `json:"id,omitempty"`
	Name           string  `json:"name"`
	Key            *newKey `json:"key,omitempty"`
	PlainSignature B64     `json:"plain_signature,omitempty"`
}

// folderInfo describes one folder to an admin, as GET /api/folders would.
func (a *API) folderInfo(r *http.Request, p *auth.Principal, f db.Folder) (FolderInfo, error) {
	list, err := a.folderInfos(r, p, []db.Folder{f})
	if err != nil {
		return FolderInfo{}, err
	}
	return list[0], nil
}

func (a *API) createFolder(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	var req folderRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	if req.ID != "" && !ids.Valid(req.ID) {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "A folder's id is a UUID in small letters.")
		return
	}
	if req.Key != nil && req.ID == "" {
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "A folder that comes with a key needs its id, which the key's signature names.")
		return
	}
	nf := storage.NewFolder{ID: req.ID, Name: req.Name, By: p.UserID, PlainSignature: req.PlainSignature}
	if req.Key != nil {
		k, ok := req.Key.check(w, req.ID, 1, p.UserID)
		if !ok {
			return
		}
		nf.Key = &k
	}
	f, err := a.Lib.CreateFolder(r.Context(), nf)
	if !a.folderError(w, "create folder", err) {
		return
	}
	a.writeFolder(w, r, p, http.StatusCreated, f)
}

func (a *API) renameFolder(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	var req folderRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	f, err := a.Lib.RenameFolder(r.Context(), r.PathValue("id"), req.Name, req.PlainSignature)
	if !a.folderError(w, "rename folder", err) {
		return
	}
	a.writeFolder(w, r, p, http.StatusOK, f)
}

func (a *API) deleteFolder(w http.ResponseWriter, r *http.Request) {
	p, ok := a.admin(w, r)
	if !ok {
		return
	}
	files, err := a.Lib.DeleteFolder(r.Context(), r.PathValue("id"), p.UserID)
	if !a.folderError(w, "delete folder", err) {
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Changed{Changed: len(files)})
}

func (a *API) writeFolder(w http.ResponseWriter, r *http.Request, p *auth.Principal, status int, f db.Folder) {
	info, err := a.folderInfo(r, p, f)
	if err != nil {
		internal(w, "folder", err)
		return
	}
	httpx.WriteJSON(w, status, info)
}

// folderError answers a failed change of a folder; it reports whether there was no error.
func (a *API) folderError(w http.ResponseWriter, what string, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, storage.ErrBadFolderName):
		httpx.WriteError(w, http.StatusBadRequest, "bad_request", "A folder needs a name, and it can't be a date like 2026-09-27.")
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such folder.")
	case errors.Is(err, db.ErrConflict):
		httpx.WriteError(w, http.StatusConflict, "folder_name_taken", "Another folder has that name.")
	case errors.Is(err, db.ErrBusy):
		httpx.WriteError(w, http.StatusConflict, "folder_busy", "The folder's files are still moving after its last rename. Try again in a few minutes.")
	case errors.Is(err, db.ErrLastFolder):
		httpx.WriteError(w, http.StatusConflict, "last_folder", "The last folder can't go: everything is sent into a folder.")
	case signatureError(w, err):
	default:
		internal(w, what, err)
	}
	return false
}

// folderPerson gives a person a folder (sees) or takes it away. Admins see every folder
// whatever this says; it counts again if they become members.
func (a *API) folderPerson(sees bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.admin(w, r); !ok {
			return
		}
		folder, ok := a.liveFolder(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		if _, err := a.Auth.DB.UserByID(ctx, r.PathValue("user")); errors.Is(err, db.ErrNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such person.")
			return
		} else if err != nil {
			internal(w, "folder person", err)
			return
		}
		if err := a.Auth.DB.SetFolderPerson(ctx, folder.ID, r.PathValue("user"), sees); err != nil {
			internal(w, "folder person", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// folderInvite lets an open invite for a new member give a folder (gets), or not.
func (a *API) folderInvite(gets bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.admin(w, r); !ok {
			return
		}
		folder, ok := a.liveFolder(w, r)
		if !ok {
			return
		}
		err := a.Auth.DB.SetFolderInvite(r.Context(), folder.ID, r.PathValue("invite"), gets, a.Now())
		switch {
		case errors.Is(err, db.ErrNotFound):
			httpx.WriteError(w, http.StatusNotFound, "not_found", "No such invite for a new member, or it was used or has ended.")
		case err != nil:
			internal(w, "folder invite", err)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}
}

// liveFolder loads the folder a request is about, if it isn't deleted.
func (a *API) liveFolder(w http.ResponseWriter, r *http.Request) (db.Folder, bool) {
	f, err := a.Auth.DB.FolderByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, db.ErrNotFound) || (err == nil && f.DeletedAt != nil) {
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such folder.")
		return f, false
	}
	if err != nil {
		internal(w, "folder", err)
		return f, false
	}
	return f, true
}

type moveRequest struct {
	IDs    []string `json:"ids"`
	Folder string   `json:"folder"`
	// Keys: for encrypted files, their file keys sealed for the target folder's newest key.
	Keys []struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
		Key     B64    `json:"key"`
	} `json:"keys,omitempty"`
}

// moveFiles moves files to another folder: who sees them changes with the folder.
func (a *API) moveFiles(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.admin(w, r); !ok {
		return
	}
	var req moveRequest
	if !httpx.DecodeJSON(w, r, &req) {
		return
	}
	fileIDs, ok := checkFileIDs(w, req.IDs)
	if !ok {
		return
	}
	keys := map[string]db.Enc{}
	for _, k := range req.Keys {
		if len(k.Key) != e2ee.FileKeySize+e2ee.SealOverhead {
			badKey(w, "sealed file key")
			return
		}
		keys[k.ID] = db.Enc{Version: k.Version, Key: k.Key}
	}
	moved, err := a.Lib.MoveFiles(r.Context(), fileIDs, req.Folder, keys)
	switch {
	case errors.Is(err, db.ErrNotFound):
		httpx.WriteError(w, http.StatusNotFound, "not_found", "No such folder.")
		return
	case errors.Is(err, storage.ErrNotEncrypted):
		httpx.WriteError(w, http.StatusConflict, "not_encrypted", "Encrypted files can only go into a folder that was encrypted too.")
		return
	case errors.Is(err, storage.ErrKeysNeeded):
		httpx.WriteError(w, http.StatusConflict, "key_outdated", "Seal the encrypted files' keys for the folder's newest key.")
		return
	}
	if err != nil {
		internal(w, "move", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Changed{Changed: len(moved)})
}
