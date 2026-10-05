package e2ee

import (
	"crypto/rand"
	"errors"
	"strings"
)

// The recovery code is 20 random bytes, written as 32 characters of Crockford's base32 in
// groups of four: digits and capitals without I, L, O and U. Reading it back takes small
// letters too, O for 0, and I and L for 1, and ignores dashes and spaces.
const (
	RecoveryCodeSize = 20
	crockford        = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
)

// NewRecoveryCode makes a recovery code: the secret and how it is written.
func NewRecoveryCode() (secret []byte, code string) {
	secret = make([]byte, RecoveryCodeSize)
	if _, err := rand.Read(secret); err != nil {
		panic(err)
	}
	return secret, FormatRecoveryCode(secret)
}

// FormatRecoveryCode writes a recovery code's secret.
func FormatRecoveryCode(secret []byte) string {
	var b strings.Builder
	var acc, bits uint
	n := 0
	for _, c := range secret {
		acc, bits = acc<<8|uint(c), bits+8
		for bits >= 5 {
			if n > 0 && n%4 == 0 {
				b.WriteByte('-')
			}
			b.WriteByte(crockford[acc>>(bits-5)&31])
			bits -= 5
			n++
		}
	}
	return b.String()
}

// ParseRecoveryCode reads a recovery code back into its secret.
func ParseRecoveryCode(code string) ([]byte, error) {
	var out []byte
	var acc, bits uint
	n := 0
	for _, r := range strings.ToUpper(code) {
		switch r {
		case '-', ' ':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		v := strings.IndexRune(crockford, r)
		if v < 0 {
			return nil, errors.New("e2ee: a recovery code has only digits and letters")
		}
		acc, bits = acc<<5|uint(v), bits+5
		if bits >= 8 {
			out = append(out, byte(acc>>(bits-8)))
			bits -= 8
		}
		n++
	}
	if n != RecoveryCodeSize*8/5 {
		return nil, errors.New("e2ee: a recovery code has 32 characters")
	}
	return out, nil
}
