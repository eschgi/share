package e2ee

// What a changed database can't fake (docs/e2ee-plan.md): the root key, which is the recovery
// key, signs every version of a folder's key, every folder that sends plain and the key of a new
// recovery code, and phones and browsers check those signatures before they send. The person's
// note keeps the root and what their devices have seen, locked with a key from the person's own
// private key. ECDSA on P-256 with SHA-256; a signature is r‖s.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
)

const (
	SignFolderKey = "share-e2ee-v1/sign/folder-key" // a version of a folder's key
	SignPlain     = "share-e2ee-v1/sign/plain"      // a folder that sends plain, under its name
	SignRoot      = "share-e2ee-v1/sign/root"       // a new recovery code's key, signed with the old one

	PurposeRoot      = "share-e2ee-v1/root"       // the root's private key, sealed for an admin's person key
	PurposeNote      = "share-e2ee-v1/note"       // the person's note, locked with a key from their private key
	PurposePinSecret = "share-e2ee-v1/pin-secret" // a PIN link's secret, locked with a key from a folder's private key

	SignatureSize   = 64
	FingerprintSize = 16 // of the root, in a PIN's link
)

// RootContext is what the root's private key is bound to when sealed for an admin, and its public
// key when locked with an invite's or a PIN's link.
var RootContext = []byte("root")

// FolderKeyMessage is what the root signs for a version of a folder's key.
func FolderKeyMessage(folderID string, version int, public []byte) []byte {
	return append(append([]byte(SignFolderKey), FolderContext(folderID, version)...), public...)
}

// PlainMessage is what the root signs for a folder that sends plain: never encrypted (version 0)
// or switched off. Its newest version, so that turning encryption on again, which makes a new
// one, ends it; and its name, so that another folder can't take it on with the name.
func PlainMessage(folderID string, version int, name string) []byte {
	return append(append(append([]byte(SignPlain), FolderContext(folderID, version)...), '\n'), name...)
}

// RootMessage is what the old root signs for the key of a new recovery code.
func RootMessage(public []byte) []byte {
	return append([]byte(SignRoot), public...)
}

// Sign signs message with a P-256 private key: ECDSA with SHA-256, as r‖s in 64 bytes.
func Sign(private, message []byte) ([]byte, error) {
	k, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), private)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(message)
	r, s, err := ecdsa.Sign(rand.Reader, k, digest[:])
	if err != nil {
		return nil, err
	}
	sig := make([]byte, SignatureSize)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return sig, nil
}

// Verify reports whether signature is public's signature of message.
func Verify(public, message, signature []byte) bool {
	if len(signature) != SignatureSize {
		return false
	}
	k, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), public)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(message)
	return ecdsa.Verify(k, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:]))
}

// Fingerprint is the root's fingerprint in a PIN's link: the first 16 bytes of SHA-256.
func Fingerprint(public []byte) []byte {
	sum := sha256.Sum256(public)
	return sum[:FingerprintSize]
}

// NoteKey is the key a person's note is locked with: HKDF-SHA256 of their private key.
func NoteKey(personPrivate []byte) []byte { return SecretKey(personPrivate, PurposeNote) }

// PinSecretKey is the key a PIN link's secret is locked with: HKDF-SHA256 of a version of the
// folder's private key, so that only someone who holds it can make one.
func PinSecretKey(folderPrivate []byte) []byte { return SecretKey(folderPrivate, PurposePinSecret) }

// NoteContext is what a person's note is bound to.
func NoteContext(userID string) []byte { return []byte("note:" + userID) }
