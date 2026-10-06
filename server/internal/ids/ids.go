// Package ids makes the random identifiers and secret tokens used across the server.
package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// New returns a new id: a UUID version 7 (RFC 9562) in its usual lower-case text form, which
// PostgreSQL keeps as a uuid. It starts with the time in milliseconds, so ids sort in the order
// they were made, and ends with 74 random bits. File ids are upload ids, so these also end up in
// tus URLs.
func New() string {
	var b [16]byte
	rand.Read(b[6:]) // never returns an error; crashes the program on failure
	ms := uint64(time.Now().UnixMilli())
	for i := range 6 {
		b[i] = byte(ms >> (40 - 8*i))
	}
	b[6] = b[6]&0x0f | 0x70 // version 7
	b[8] = b[8]&0x3f | 0x80 // RFC 9562's variant
	var s [36]byte
	hex.Encode(s[0:8], b[0:4])
	hex.Encode(s[9:13], b[4:6])
	hex.Encode(s[14:18], b[6:8])
	hex.Encode(s[19:23], b[8:10])
	hex.Encode(s[24:36], b[10:16])
	s[8], s[13], s[18], s[23] = '-', '-', '-', '-'
	return string(s[:])
}

// Valid reports whether s has the shape of an id: a UUID in lower case.
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
