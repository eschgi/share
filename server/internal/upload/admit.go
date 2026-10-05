package upload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/eschgi/share/server/internal/auth"
	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/e2ee"
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

// Enc is how a client says that an upload is encrypted (docs/e2ee-plan.md): the version of the
// folder's key that its file key is sealed for, that sealed key and the header its bytes start
// with, base64url, and its plain size. The upload's size is the encrypted size.
type Enc struct {
	Version   int    `json:"version"`
	Key       string `json:"key"`
	Header    string `json:"header"`
	PlainSize *int64 `json:"plain_size"`
}

// parseEnc reads tus's enc metadata, JSON like S3's field.
func parseEnc(meta string) (*Enc, error) {
	if meta == "" {
		return nil, nil
	}
	var e Enc
	if err := json.Unmarshal([]byte(meta), &e); err != nil {
		return nil, &Refusal{http.StatusBadRequest, "bad_request", "The upload's enc isn't JSON."}
	}
	return &e, nil
}

// encryption checks how an upload of size bytes into a folder is encrypted. An encrypted
// folder takes only encrypted uploads, sealed for its key's newest version; a folder that was
// never encrypted takes only plain ones.
func (a *admission) encryption(ctx context.Context, folderID string, size int64, e *Enc) (*db.Enc, error) {
	f, err := a.db.FolderByID(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if e == nil {
		if f.Encrypted {
			return nil, &Refusal{http.StatusConflict, "encryption_required", "This folder's new files are encrypted; encrypt this one too."}
		}
		return nil, nil
	}
	if f.KeyVersion == 0 {
		return nil, &Refusal{http.StatusConflict, "not_encrypted", "This folder has no key to encrypt for."}
	}
	if e.Version != f.KeyVersion {
		return nil, &Refusal{http.StatusConflict, "key_outdated", "The folder's key has a newer version; seal the file key for that one."}
	}
	key, err := base64.RawURLEncoding.DecodeString(e.Key)
	if err != nil || len(key) != e2ee.FileKeySize+e2ee.SealOverhead {
		return nil, &Refusal{http.StatusBadRequest, "bad_request", "The upload's sealed file key is malformed."}
	}
	header, err := base64.RawURLEncoding.DecodeString(e.Header)
	if err == nil {
		_, err = e2ee.ParseHeader(header)
	}
	if err != nil {
		return nil, &Refusal{http.StatusBadRequest, "bad_request", "The upload's header is malformed."}
	}
	if e.PlainSize == nil || *e.PlainSize < 0 || e2ee.EncryptedSize(*e.PlainSize) != size {
		return nil, &Refusal{http.StatusBadRequest, "bad_request", "The upload's size isn't its plain size, encrypted."}
	}
	return &db.Enc{Version: e.Version, Key: key, Header: header, PlainSize: *e.PlainSize}, nil
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
