package api

import (
	"context"
	"errors"

	"github.com/eschgi/share/server/internal/db"
)

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
