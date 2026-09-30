package auth

import (
	"context"
	"time"

	"github.com/eschgi/share/server/internal/db"
)

// Principal kinds.
const (
	KindPin    = "pin"    // unlocked with a PIN: may only send
	KindDevice = "device" // a signed-in phone
)

// Principal is who a request acts for.
type Principal struct {
	Kind string

	// PIN sessions.
	PinSessionID string
	PinID        string
	PinKind      string
	PinExpiresAt *time.Time

	// Signed-in devices.
	UserID   string
	DeviceID string
	Role     string
}

// Owns reports whether an upload belongs to this principal: the same PIN session, or the
// same person (any of their phones).
func (p *Principal) Owns(f db.File) bool {
	switch p.Kind {
	case KindPin:
		return f.PinSessionID != "" && f.PinSessionID == p.PinSessionID
	case KindDevice:
		return f.UserID != "" && f.UserID == p.UserID
	}
	return false
}

// Key identifies the principal for per-principal limits.
func (p *Principal) Key() string {
	if p.Kind == KindPin {
		return "pin:" + p.PinSessionID
	}
	return "user:" + p.UserID
}

type principalKey struct{}

// WithPrincipal stores p in ctx.
func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the principal stored by WithPrincipal.
func FromContext(ctx context.Context) (*Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(*Principal)
	return p, ok && p != nil
}
