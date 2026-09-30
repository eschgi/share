// Package ids makes the random identifiers and secret tokens used across the server.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"regexp"
	"strings"
)

// lowerBase32 is RFC 4648 base32 in lower case, so ids are URL-safe, case-stable and never
// contain look-alike separators.
var lowerBase32 = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

var idPattern = regexp.MustCompile(`^[a-z2-7]{26}$`)

// New returns a 128-bit random id: 26 lower-case base32 characters. File ids are upload ids,
// so these also end up in tus URLs.
func New() string {
	var b [16]byte
	rand.Read(b[:]) // never returns an error; crashes the program on failure
	return lowerBase32.EncodeToString(b[:])
}

// Valid reports whether s has the shape of an id made by New.
func Valid(s string) bool { return idPattern.MatchString(s) }

// Token prefixes tell apart the three kinds of bearer secrets, and make them easy to spot
// in logs or by secret scanners.
const (
	PrefixPinSession = "shp_"
	PrefixDevice     = "shd_"
	PrefixInvite     = "shi_"
)

// tokenLen is the length of the random part: 32 bytes in unpadded base64url.
const tokenLen = 43

// NewToken returns a new secret with the given prefix and the hash to store instead of it.
func NewToken(prefix string) (token string, hash []byte) {
	var b [32]byte
	rand.Read(b[:])
	token = prefix + base64.RawURLEncoding.EncodeToString(b[:])
	return token, HashToken(token)
}

// HashToken is what the database keeps for a token. Tokens carry 256 random bits, so a
// plain SHA-256 is enough; there is nothing to brute-force.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// TokenHasPrefix reports whether token looks like a token of this kind. It only checks the
// shape; the database decides whether it is real.
func TokenHasPrefix(token, prefix string) bool {
	if !strings.HasPrefix(token, prefix) || len(token) != len(prefix)+tokenLen {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token[len(prefix):])
	return err == nil
}
