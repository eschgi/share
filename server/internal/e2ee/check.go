package e2ee

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// A check comes before a device passes keys on: to another device of its person, or folder keys
// to another person (docs/e2ee-plan.md, contract/crypto/check.json). Both screens show a code
// of 6 digits, which the person compares. The asking device first sends a commitment to a
// random nonce, the other side answers with a nonce of its own, and only then does the asking
// device reveal its nonce. So neither side's nonce can be chosen after seeing the other's, and
// whoever relays them can't try out keys until one gives the same code.
const (
	PurposeCheck = "share-e2ee-v1/check" // the commitment to the asking device's nonce
	PurposeCode  = "share-e2ee-v1/code"  // the code, from both nonces and the public key that gets the keys
	NonceSize    = 32
)

// Commitment is what the asking device sends first: SHA-256 of the purpose and its nonce.
func Commitment(nonce []byte) []byte {
	sum := sha256.Sum256(append([]byte(PurposeCheck), nonce...))
	return sum[:]
}

// CheckCode is the code both screens show: the first 4 bytes of SHA-256 of the purpose, the
// asking device's nonce, the other side's nonce and the public key that gets the keys, as a
// big-endian number, modulo a million, in 6 digits.
func CheckCode(askerNonce, answerNonce, publicKey []byte) string {
	h := sha256.New()
	h.Write([]byte(PurposeCode))
	h.Write(askerNonce)
	h.Write(answerNonce)
	h.Write(publicKey)
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(h.Sum(nil)[:4])%1_000_000)
}
