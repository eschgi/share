package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/eschgi/share/server/internal/db"
	"github.com/eschgi/share/server/internal/ids"
)

// ErrPinTaken means a PIN had that code already; codes are never used twice.
var ErrPinTaken = errors.New("that PIN code is taken")

// ErrFolderGone means the folder a PIN or a file should go into doesn't exist (any more).
var ErrFolderGone = errors.New("that folder doesn't exist any more")

// PinSpec says what PIN to make.
type PinSpec struct {
	Kind     string // db.PinPermanent or db.PinDay
	Code     string // as typed (spaces, dashes and small letters are fine); "" for a random one
	FolderID string // the folder it sends into
	// ShowsFolder: guests with the PIN also see and download what is in the folder.
	ShowsFolder bool
	// Secret: for a folder that is encrypted, its keys locked with the secret of the PIN's link.
	Secret *db.PinSecret
}

// CreatePin makes a PIN that sends into a folder, with the code asked for or a fresh random
// one. A 24-hour PIN stops working a day after it was made. It returns ErrPINFormat for a
// code that can't be a PIN, ErrPinTaken for one that was used before, and ErrFolderGone for a
// folder that doesn't exist or is deleted.
func (s *Service) CreatePin(ctx context.Context, spec PinSpec, createdBy string) (db.Pin, error) {
	if spec.Kind != db.PinPermanent && spec.Kind != db.PinDay {
		return db.Pin{}, fmt.Errorf("unknown PIN kind %q", spec.Kind)
	}
	folder, err := s.DB.FolderByID(ctx, spec.FolderID)
	if errors.Is(err, db.ErrNotFound) || (err == nil && folder.DeletedAt != nil) {
		return db.Pin{}, ErrFolderGone
	}
	if err != nil {
		return db.Pin{}, err
	}
	now := s.Now()
	p := db.Pin{ID: ids.New(), Kind: spec.Kind, CreatedBy: createdBy, CreatedAt: now, FolderID: folder.ID, ShowsFolder: spec.ShowsFolder, Secret: spec.Secret}
	if spec.Kind == db.PinDay {
		exp := now.Add(DayPinLifetime)
		p.ExpiresAt = &exp
	}
	if spec.Code != "" {
		code, ok := NormalizeCode(spec.Code)
		if !ok {
			return db.Pin{}, ErrPINFormat
		}
		p.Code = code
		if err := s.DB.InsertPin(ctx, p); errors.Is(err, db.ErrConflict) {
			return db.Pin{}, ErrPinTaken
		} else if err != nil {
			return db.Pin{}, err
		}
		return p, nil
	}
	// 2^25 codes: collisions with earlier codes are rare, but they are never reused.
	for range 50 {
		p.Code = GenerateCode()
		err := s.DB.InsertPin(ctx, p)
		if errors.Is(err, db.ErrConflict) {
			continue
		}
		return p, err
	}
	return db.Pin{}, errors.New("could not find an unused PIN code")
}

// SuggestCode returns a random code no PIN has had yet, for the new-PIN screen to show.
func (s *Service) SuggestCode(ctx context.Context) (string, error) {
	for range 50 {
		code := GenerateCode()
		_, err := s.DB.PinByCode(ctx, code)
		if errors.Is(err, db.ErrNotFound) {
			return code, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", errors.New("could not find an unused PIN code")
}

// NewCode replaces a PIN's code. The old PIN ends (so its sessions and links stop working)
// and a new PIN like it, into the same folder, takes its place; codes are never reused. The
// secret of its link, if it has one, stays.
func (s *Service) NewCode(ctx context.Context, id, createdBy string) (db.Pin, error) {
	old, err := s.DB.PinByID(ctx, id)
	if err != nil {
		return db.Pin{}, err
	}
	if !old.LiveAt(s.Now()) {
		return db.Pin{}, errors.New("that PIN has already ended")
	}
	secret, err := s.DB.PinSecretOf(ctx, old.ID)
	if err != nil {
		return db.Pin{}, err
	}
	p, err := s.CreatePin(ctx, PinSpec{Kind: old.Kind, FolderID: old.FolderID, ShowsFolder: old.ShowsFolder, Secret: secret}, createdBy)
	if err != nil {
		return db.Pin{}, err
	}
	if err := s.DB.EndPin(ctx, old.ID, s.Now()); err != nil {
		return db.Pin{}, err
	}
	return p, nil
}

// EndPin ends a PIN now.
func (s *Service) EndPin(ctx context.Context, id string) error {
	return s.DB.EndPin(ctx, id, s.Now())
}

// FindPin looks a PIN up by id or by code, whichever was given.
func (s *Service) FindPin(ctx context.Context, idOrCode string) (db.Pin, error) {
	if ids.Valid(idOrCode) {
		return s.DB.PinByID(ctx, idOrCode)
	}
	code, ok := NormalizeCode(idOrCode)
	if !ok {
		return db.Pin{}, db.ErrNotFound
	}
	return s.DB.PinByCode(ctx, code)
}
