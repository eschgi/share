// Package e2ee is Share's end-to-end encryption as the contract defines it (docs/e2ee-plan.md,
// contract/crypto). The website and the app encrypt and decrypt; the server never does, but
// checks the keys and sizes clients send, and its tests and the contract's vectors come from
// here.
//
// Keys are P-256. Sealing a key for the holder of a key pair is HPKE in base mode
// (RFC 9180: DHKEM(P-256, HKDF-SHA256), HKDF-SHA256, AES-256-GCM); locking something with a
// secret, a password or a file's key is AES-256-GCM with a random nonce.
package e2ee

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hpke"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Sizes of keys and of what sealing and locking add.
const (
	PublicKeySize  = 65 // uncompressed: 0x04, then X and Y
	PrivateKeySize = 32
	FileKeySize    = 32
	SealOverhead   = PublicKeySize + tagSize
	LockOverhead   = nonceSize + tagSize
	nonceSize      = 12
	tagSize        = 16
)

// Purposes: HPKE's info when sealing, HKDF's info when deriving a key.
const (
	PurposeFile     = "share-e2ee-v1/file"     // a file's key, sealed for its folder's key
	PurposeFolder   = "share-e2ee-v1/folder"   // a folder's private key, for a person or the recovery key
	PurposePerson   = "share-e2ee-v1/person"   // a person's private key, for one of their phones or browsers
	PurposeContent  = "share-e2ee-v1/content"  // a file's contents
	PurposeThumb    = "share-e2ee-v1/thumb"    // a file's thumbnail
	PurposeInvite   = "share-e2ee-v1/invite"   // keys locked with an invite link's secret
	PurposePin      = "share-e2ee-v1/pin"      // folder keys locked with a PIN link's secret
	PurposeRecovery = "share-e2ee-v1/recovery" // the recovery key, locked with the recovery code
)

// FolderContext is what a folder key, and a file key sealed for it, are bound to.
func FolderContext(folderID string, version int) []byte {
	return fmt.Appendf(nil, "folder:%s:%d", folderID, version)
}

// PersonContext is what a person's private key is bound to.
func PersonContext(userID string) []byte { return []byte("person:" + userID) }

// RecoveryContext is what the recovery key is bound to.
var RecoveryContext = []byte("recovery")

var errOpen = errors.New("e2ee: can't be opened")

// CheckPublicKey reports whether b is a P-256 public key in the form clients send.
func CheckPublicKey(b []byte) error {
	if _, err := ecdh.P256().NewPublicKey(b); err != nil {
		return fmt.Errorf("e2ee: not a P-256 public key: %w", err)
	}
	return nil
}

// GenerateKey makes a key pair: its private and public key, as they are sealed and sent.
func GenerateKey() (private, public []byte) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	return k.Bytes(), k.PublicKey().Bytes()
}

// PublicKey is the public key of a private one.
func PublicKey(private []byte) ([]byte, error) {
	k, err := ecdh.P256().NewPrivateKey(private)
	if err != nil {
		return nil, err
	}
	return k.PublicKey().Bytes(), nil
}

func suite() (hpke.KEM, hpke.KDF, hpke.AEAD) {
	return hpke.DHKEM(ecdh.P256()), hpke.HKDFSHA256(), hpke.AES256GCM()
}

// Seal encrypts plaintext so that only the holder of public's private key can read it, for
// purpose and bound to aad: HPKE's encapsulated key, then the ciphertext.
func Seal(public []byte, purpose string, aad, plaintext []byte) ([]byte, error) {
	kem, kdf, aead := suite()
	pk, err := kem.NewPublicKey(public)
	if err != nil {
		return nil, err
	}
	enc, s, err := hpke.NewSender(pk, kdf, aead, []byte(purpose))
	if err != nil {
		return nil, err
	}
	ct, err := s.Seal(aad, plaintext)
	if err != nil {
		return nil, err
	}
	return append(enc, ct...), nil
}

// Open decrypts what Seal made, with the private key, for the same purpose and aad.
func Open(private []byte, purpose string, aad, sealed []byte) ([]byte, error) {
	if len(sealed) < SealOverhead {
		return nil, errOpen
	}
	kem, kdf, aead := suite()
	sk, err := kem.NewPrivateKey(private)
	if err != nil {
		return nil, err
	}
	r, err := hpke.NewRecipient(sealed[:PublicKeySize], sk, kdf, aead, []byte(purpose))
	if err != nil {
		return nil, errOpen
	}
	pt, err := r.Open(aad, sealed[PublicKeySize:])
	if err != nil {
		return nil, errOpen
	}
	return pt, nil
}

func gcm(key []byte) cipher.AEAD {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err) // keys are always 32 bytes here
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return g
}

// Lock encrypts plaintext with a 32-byte key, bound to aad: a random nonce, then the
// ciphertext.
func Lock(key, aad, plaintext []byte) []byte {
	return lockWith(rand.Reader, key, aad, plaintext)
}

func lockWith(random io.Reader, key, aad, plaintext []byte) []byte {
	nonce := make([]byte, nonceSize, nonceSize+len(plaintext)+tagSize)
	if _, err := io.ReadFull(random, nonce); err != nil {
		panic(err)
	}
	return gcm(key).Seal(nonce, nonce, plaintext, aad)
}

// Unlock decrypts what Lock made.
func Unlock(key, aad, locked []byte) ([]byte, error) {
	if len(locked) < LockOverhead {
		return nil, errOpen
	}
	pt, err := gcm(key).Open(nil, locked[:nonceSize], locked[nonceSize:], aad)
	if err != nil {
		return nil, errOpen
	}
	return pt, nil
}

// SecretKey is the key a link's secret or the recovery code locks with, for purpose.
func SecretKey(secret []byte, purpose string) []byte {
	k, err := hkdfKey(secret, nil, purpose)
	if err != nil {
		panic(err)
	}
	return k
}

// hkdfKey is HKDF-SHA256's 32-byte key from secret, with salt, for purpose.
func hkdfKey(secret, salt []byte, purpose string) ([]byte, error) {
	return hkdf.Key(sha256.New, secret, salt, purpose, 32)
}

// PasswordIterations is PBKDF2's work for a password lock, as OWASP recommends for
// HMAC-SHA256.
const PasswordIterations = 600_000

const saltSize = 16

// PasswordLock locks plaintext with a password: the salt, the iterations (4 bytes, big
// endian), then Lock's output with the key PBKDF2-HMAC-SHA256 derives.
func PasswordLock(password string, aad, plaintext []byte) []byte {
	return passwordLockWith(rand.Reader, password, PasswordIterations, aad, plaintext)
}

func passwordLockWith(random io.Reader, password string, iterations int, aad, plaintext []byte) []byte {
	head := make([]byte, saltSize, saltSize+4)
	if _, err := io.ReadFull(random, head); err != nil {
		panic(err)
	}
	key := passwordKey(password, head, iterations)
	head = binary.BigEndian.AppendUint32(head, uint32(iterations))
	return append(head, lockWith(random, key, aad, plaintext)...)
}

func passwordKey(password string, salt []byte, iterations int) []byte {
	k, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		panic(err)
	}
	return k
}

// PasswordUnlock decrypts what PasswordLock made. It refuses fewer iterations than a tenth
// of today's, and more than ten times as many.
func PasswordUnlock(password string, aad, locked []byte) ([]byte, error) {
	if len(locked) < saltSize+4+LockOverhead {
		return nil, errOpen
	}
	iterations := int(binary.BigEndian.Uint32(locked[saltSize:]))
	if iterations < PasswordIterations/10 || iterations > PasswordIterations*10 {
		return nil, errOpen
	}
	return Unlock(passwordKey(password, locked[:saltSize], iterations), aad, locked[saltSize+4:])
}

// CheckPasswordLock reports whether b looks like a password lock of a private key.
func CheckPasswordLock(b []byte) error {
	if len(b) != saltSize+4+LockOverhead+PrivateKeySize {
		return errors.New("e2ee: not a password lock of a key")
	}
	if n := binary.BigEndian.Uint32(b[saltSize:]); n < PasswordIterations/10 || n > PasswordIterations*10 {
		return errors.New("e2ee: a password lock needs 60 000 to 6 000 000 iterations")
	}
	return nil
}
