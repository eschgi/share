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

func (s *Service) newPin(kind, createdBy string) (db.Pin, error) {
	if kind != db.PinPermanent && kind != db.PinDay {
		return db.Pin{}, fmt.Errorf("unknown PIN kind %q", kind)
	}
	now := s.Now()
	p := db.Pin{ID: ids.New(), Kind: kind, CreatedBy: createdBy, CreatedAt: now}
	if kind == db.PinDay {
		exp := now.Add(DayPinLifetime)
		p.ExpiresAt = &exp
	}
	return p, nil
}

// CreatePin makes a PIN with a fresh random code. A 24-hour PIN stops working a day after it
// was made.
func (s *Service) CreatePin(ctx context.Context, kind, createdBy string) (db.Pin, error) {
	p, err := s.newPin(kind, createdBy)
	if err != nil {
		return p, err
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

// CreatePinCode makes a PIN with a code the person chose (typed as they like: spaces,
// dashes and small letters are fine). It returns ErrPINFormat for something that can't be a
// PIN and ErrPinTaken for a code that was used before.
func (s *Service) CreatePinCode(ctx context.Context, kind, input, createdBy string) (db.Pin, error) {
	code, ok := NormalizeCode(input)
	if !ok {
		return db.Pin{}, ErrPINFormat
	}
	p, err := s.newPin(kind, createdBy)
	if err != nil {
		return p, err
	}
	p.Code = code
	if err := s.DB.InsertPin(ctx, p); errors.Is(err, db.ErrConflict) {
		return db.Pin{}, ErrPinTaken
	} else if err != nil {
		return db.Pin{}, err
	}
	return p, nil
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
// and a new PIN of the same kind takes its place; codes are never reused.
func (s *Service) NewCode(ctx context.Context, id, createdBy string) (db.Pin, error) {
	old, err := s.DB.PinByID(ctx, id)
	if err != nil {
		return db.Pin{}, err
	}
	if !old.LiveAt(s.Now()) {
		return db.Pin{}, errors.New("that PIN has already ended")
	}
	p, err := s.CreatePin(ctx, old.Kind, createdBy)
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
