package upload

import (
	"context"
	"errors"
	"net/http"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
)

// Refusal is a new upload the server doesn't take, with the answer the client gets.
type Refusal struct {
	Status  int
	Code    string
	Message string
}

func (r *Refusal) Error() string { return r.Message }

// admission checks a new upload the same way however it arrives: its size, the room on the
// drive, the sender's unfinished uploads and the folder it goes into.
type admission struct {
	cfg  Config
	db   *db.DB
	free func() (int64, error) // free bytes on the storage drive; nil without a drive of our own
}

// admit returns the folder a new upload of size bytes goes into, or a *Refusal. chosen is the
// folder the sender said, in the request's field.
func (a *admission) admit(ctx context.Context, p *auth.Principal, size int64, chosen, field string) (string, error) {
	if a.cfg.MaxFileSize > 0 && size > a.cfg.MaxFileSize {
		return "", &Refusal{http.StatusRequestEntityTooLarge, "too_large", "This file is larger than the server accepts."}
	}
	if a.free != nil {
		free, err := a.free()
		if err != nil {
			return "", err
		}
		outstanding, err := a.db.OutstandingBytes(ctx)
		if err != nil {
			return "", err
		}
		if free-outstanding-a.cfg.MinFreeSpace < size {
			return "", &Refusal{http.StatusRequestEntityTooLarge, "no_space", "The server's drive is full."}
		}
	}
	n, err := a.db.UnfinishedCount(ctx, p.PinSessionID, p.UserID)
	if err != nil {
		return "", err
	}
	if n >= a.cfg.MaxUnfinished {
		return "", &Refusal{http.StatusForbidden, "too_many_uploads", "Too many unfinished uploads; let some finish first."}
	}

	folderID, err := a.folderFor(ctx, p, chosen)
	switch {
	case errors.Is(err, errNoFolder):
		return "", &Refusal{http.StatusForbidden, "no_folder", "There is no folder you could send into."}
	case errors.Is(err, errFolderGone):
		return "", &Refusal{http.StatusNotFound, "folder_gone", "That folder is gone, or you don't see it any more."}
	case errors.Is(err, errFolderUnsaid):
		return "", &Refusal{http.StatusBadRequest, "bad_request", "Say which folder the file goes into (" + field + ")."}
	}
	return folderID, err
}

var (
	errNoFolder     = errors.New("no folder to send into")
	errFolderGone   = errors.New("that folder is gone, or not the sender's")
	errFolderUnsaid = errors.New("no folder said")
)

// folderFor is the folder an upload goes into: a PIN's into the PIN's folder, someone signed
// in into the one they chose, among those they see.
func (a *admission) folderFor(ctx context.Context, p *auth.Principal, chosen string) (string, error) {
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
		folders, err = a.db.LiveFolders(ctx)
	} else {
		folders, err = a.db.FoldersOf(ctx, p.UserID)
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
