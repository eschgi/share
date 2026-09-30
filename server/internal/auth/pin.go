package auth

import (
	"crypto/rand"
	"strings"
)

// PinAlphabet has no look-alikes (no 0/O, no 1/I), so a PIN survives being read out loud.
// Its 32 symbols make every character exactly 5 random bits.
const PinAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// PinLength is the number of characters in a PIN.
const PinLength = 5

// GenerateCode returns a random PIN code.
func GenerateCode() string {
	var b [PinLength]byte
	rand.Read(b[:])
	for i := range b {
		b[i] = PinAlphabet[b[i]&31] // 256 is a multiple of 32, so no bias
	}
	return string(b[:])
}

// NormalizeCode turns what someone typed into a code: capitals or small letters both work,
// and spaces and dashes are ignored. ok is false if it can't be a code at all, which doesn't
// count as a wrong try.
func NormalizeCode(input string) (code string, ok bool) {
	var b strings.Builder
	for _, r := range strings.ToUpper(input) {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			continue
		case r < 128 && strings.IndexByte(PinAlphabet, byte(r)) >= 0:
			b.WriteRune(r)
		default:
			return "", false
		}
	}
	if b.Len() != PinLength {
		return "", false
	}
	return b.String(), true
}
