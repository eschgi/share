package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/httpx"
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
// admins every folder, members the ones they were given.
func (a *API) viewer(w http.ResponseWriter, r *http.Request) (*auth.Principal, []db.Folder, bool) {
	p, ok := a.device(w, r)
	if !ok {
		return nil, nil, false
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
	if p.Role == db.RoleAdmin {
		return a.Auth.DB.LiveFolders(ctx)
	}
	return a.Auth.DB.FoldersOf(ctx, p.UserID)
}

func folderIDs(folders []db.Folder) []string {
	out := make([]string, len(folders))
	for i, f := range folders {
		out[i] = f.ID
	}
	return out
}

// oldestFolder is where things go when nobody says which folder: the oldest one.
func (a *API) oldestFolder(ctx context.Context) (db.Folder, error) {
	live, err := a.Auth.DB.LiveFolders(ctx)
	if err != nil {
		return db.Folder{}, err
	}
	if len(live) == 0 {
		return db.Folder{}, errors.New("there is no folder")
	}
	return live[0], nil
}

func (a *API) folders(w http.ResponseWriter, r *http.Request) {
	_, folders, ok := a.viewer(w, r)
	if !ok {
		return
	}
	list, err := a.folderInfos(r, folders)
	if err != nil {
		internal(w, "folders", err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, Folders{Folders: list})
}

// folderInfos describes folders: what they hold comes from the cache while the library is
// unchanged; who sees them is counted each time.
func (a *API) folderInfos(r *http.Request, folders []db.Folder) ([]FolderInfo, error) {
	ctx := r.Context()
	stats, covers, err := a.folderContents(ctx, folders)
	if err != nil {
		return nil, err
	}
	people, err := a.Auth.DB.CountFolderPeople(ctx, a.Now())
	if err != nil {
		return nil, err
	}
	names, err := a.userNames(r)
	if err != nil {
		return nil, err
	}
	out := make([]FolderInfo, len(folders))
	for i, f := range folders {
		s := stats[f.ID]
		out[i] = FolderInfo{
			ID: f.ID, Name: f.Name, Files: s.Files, Bytes: s.Bytes, Senders: s.Senders, CreatedAt: f.CreatedAt,
			People: people.Admins + people.Members[f.ID], AdminsOnly: people.Members[f.ID] == 0,
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
