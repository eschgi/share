package auth

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// passwordIterations follows OWASP's advice for PBKDF2 with SHA-256. On a small machine one
// check takes a few hundred milliseconds, so at most two run at once.
const passwordIterations = 600_000

var hashing = make(chan struct{}, 2)

// HashPassword returns the stored form of a password: pbkdf2-sha256$<iterations>$<salt>$<key>.
func HashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, 16)
	rand.Read(salt)
	key, err := derive(ctx, password, salt, passwordIterations)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// CheckPassword reports whether password matches a stored hash.
func CheckPassword(ctx context.Context, stored, password string) (bool, error) {
	parts := strings.Split(stored, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false, errors.New("unknown password hash format")
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false, errors.New("bad password hash")
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false, errors.New("bad password hash")
	}
	key, err := derive(ctx, password, salt, iter)
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(key, want) == 1, nil
}

func derive(ctx context.Context, password string, salt []byte, iter int) ([]byte, error) {
	select {
	case hashing <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-hashing }()
	return pbkdf2.Key(sha256.New, password, salt, iter, 32)
}

// NewPassword makes up a password for someone, for an admin to hand over: 16 characters in
// four groups, without look-alikes, easy to read out (79 bits).
func NewPassword() string {
	const alphabet = "23456789abcdefghjkmnpqrstuvwxyz"
	const limit = 256 - 256%len(alphabet) // bytes from here on would favour the first characters
	var out strings.Builder
	var b [1]byte
	for n := 0; n < 16; {
		rand.Read(b[:])
		if int(b[0]) >= limit {
			continue
		}
		if n > 0 && n%4 == 0 {
			out.WriteByte('-')
		}
		out.WriteByte(alphabet[int(b[0])%len(alphabet)])
		n++
	}
	return out.String()
}

// decoyHash is checked when a username doesn't exist, so that answer takes as long as a
// wrong password and doesn't tell which usernames exist.
var decoyHash = sync.OnceValue(func() string {
	h, _ := HashPassword(context.Background(), "no such user")
	return h
})
