package e2ee

import (
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// A check comes before a device passes keys on: to another device of its person, or folder keys
// to another person (docs/e2ee-plan.md, contract/crypto/check.json). Both screens show a code
// of 6 digits, which the person compares. Each side brings a one-time key: the asking device first
// sends a commitment to its one, the other side answers with its own, and only then does the
// asking device reveal its key. So neither can be chosen after seeing the other, and whoever
// relays them can't try out keys until one gives the same code. After Allow, the asking device
// hands on the root it trusts, and to a device of its person the person's key pair, locked with a
// key from the secret only the two one-time keys make.
const (
	PurposeCheck   = "share-e2ee-v1/check"   // the commitment to the asking device's one-time key
	PurposeCode    = "share-e2ee-v1/code"    // the code, from both one-time keys and the key that gets the keys
	PurposeConfirm = "share-e2ee-v1/confirm" // what the asking device hands on after Allow
)

// Commitment is what the asking device sends first: SHA-256 of the purpose and its one-time
// public key.
func Commitment(key []byte) []byte {
	sum := sha256.Sum256(append([]byte(PurposeCheck), key...))
	return sum[:]
}

// CheckCode is the code both screens show: the first 4 bytes of SHA-256 of the purpose, the
// asking device's one-time key, the other side's and the public key that gets the keys, as a
// big-endian number, modulo a million, in 6 digits.
func CheckCode(askerKey, answerKey, publicKey []byte) string {
	h := sha256.New()
	h.Write([]byte(PurposeCode))
	h.Write(askerKey)
	h.Write(answerKey)
	h.Write(publicKey)
	return fmt.Sprintf("%06d", binary.BigEndian.Uint32(h.Sum(nil)[:4])%1_000_000)
}

// ConfirmKey is the key what the asking device hands on after Allow is locked with: HKDF-SHA256
// of the ECDH secret of one side's one-time private key and the other side's public key, salted
// with SHA-256 of the asking device's one-time key, the other side's and the key that gets the
// keys.
func ConfirmKey(private, otherPublic, askerKey, answerKey, publicKey []byte) ([]byte, error) {
	sk, err := ecdh.P256().NewPrivateKey(private)
	if err != nil {
		return nil, err
	}
	pk, err := ecdh.P256().NewPublicKey(otherPublic)
	if err != nil {
		return nil, err
	}
	secret, err := sk.ECDH(pk)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	h.Write(askerKey)
	h.Write(answerKey)
	h.Write(publicKey)
	return hkdfKey(secret, h.Sum(nil), PurposeConfirm)
}

// CheckContext is what the asking device's confirmation is bound to.
func CheckContext(id string) []byte { return []byte("check:" + id) }
