package e2ee

import (
	"bytes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
)

// A file's contents are encrypted in chunks of ChunkSize bytes, each with AES-256-GCM, after a
// header: "SHE1", the chunk size (4 bytes, big endian), a random nonce prefix of 7 bytes and a
// zero byte. Chunk i's nonce is the prefix, i (4 bytes, big endian) and 1 for the last chunk,
// else 0, so chunks can't be dropped, reordered or cut off unnoticed. Every chunk is
// ChunkSize long but the last, which has 1 to ChunkSize bytes, or none in an empty file. The
// key comes from the file's key with HKDF, salted with the header.
//
// The same file key, prefix and contents always give the same bytes, so a piece sent again
// after an interruption is the one sent before, and any range of plaintext can be read from
// the chunks around it.
const (
	ChunkSize  = 64 << 10
	HeaderSize = 16
	prefixSize = 7
)

var magic = []byte("SHE1")

// Header is the start of an encrypted file.
type Header [HeaderSize]byte

// NewHeader makes the header of a new encrypted file, with a random nonce prefix.
func NewHeader() Header { return newHeader(rand.Reader) }

func newHeader(random io.Reader) Header {
	var h Header
	copy(h[:], magic)
	binary.BigEndian.PutUint32(h[4:], ChunkSize)
	if _, err := io.ReadFull(random, h[8:8+prefixSize]); err != nil {
		panic(err)
	}
	return h
}

// ParseHeader checks the header of an encrypted file.
func ParseHeader(b []byte) (Header, error) {
	var h Header
	if len(b) != HeaderSize || !bytes.Equal(b[:4], magic) || binary.BigEndian.Uint32(b[4:]) != ChunkSize || b[HeaderSize-1] != 0 {
		return h, errors.New("e2ee: not the header of an encrypted file")
	}
	copy(h[:], b)
	return h, nil
}

// Chunks is how many chunks plainSize bytes take: one at least.
func Chunks(plainSize int64) int64 {
	if plainSize <= 0 {
		return 1
	}
	return (plainSize + ChunkSize - 1) / ChunkSize
}

// EncryptedSize is the size of plainSize bytes once encrypted.
func EncryptedSize(plainSize int64) int64 {
	return HeaderSize + plainSize + Chunks(plainSize)*tagSize
}

// ContentKey is the key of a file's chunks.
func ContentKey(fileKey []byte, h Header) []byte {
	k, err := hkdfKey(fileKey, h[:], PurposeContent)
	if err != nil {
		panic(err)
	}
	return k
}

func chunkNonce(h Header, i int64, last bool) []byte {
	n := make([]byte, nonceSize)
	copy(n, h[8:8+prefixSize])
	binary.BigEndian.PutUint32(n[prefixSize:], uint32(i))
	if last {
		n[nonceSize-1] = 1
	}
	return n
}

// Encrypter encrypts one file's chunks.
type Encrypter struct {
	aead   cipher.AEAD
	header Header
	chunks int64
}

// NewEncrypter encrypts a file of plainSize bytes with fileKey, after header h.
func NewEncrypter(fileKey []byte, h Header, plainSize int64) *Encrypter {
	return &Encrypter{aead: gcm(ContentKey(fileKey, h)), header: h, chunks: Chunks(plainSize)}
}

// Chunk encrypts chunk i, which must be ChunkSize long unless it is the last one.
func (e *Encrypter) Chunk(i int64, plain []byte) []byte {
	return e.aead.Seal(nil, chunkNonce(e.header, i, i == e.chunks-1), plain, nil)
}

// Open decrypts chunk i.
func (e *Encrypter) Open(i int64, ct []byte) ([]byte, error) {
	if i < 0 || i >= e.chunks {
		return nil, errOpen
	}
	pt, err := e.aead.Open(nil, chunkNonce(e.header, i, i == e.chunks-1), ct, nil)
	if err != nil {
		return nil, errOpen
	}
	return pt, nil
}

// Encrypt encrypts a whole file: its header, then the chunks.
func Encrypt(fileKey []byte, h Header, plain []byte) []byte {
	e := NewEncrypter(fileKey, h, int64(len(plain)))
	out := append(make([]byte, 0, EncryptedSize(int64(len(plain)))), h[:]...)
	for i := int64(0); i < e.chunks; i++ {
		end := min((i+1)*ChunkSize, int64(len(plain)))
		out = append(out, e.Chunk(i, plain[i*ChunkSize:end])...)
	}
	return out
}

// Decrypt decrypts a whole file that Encrypt made from plainSize bytes.
func Decrypt(fileKey []byte, data []byte, plainSize int64) ([]byte, error) {
	if int64(len(data)) != EncryptedSize(plainSize) {
		return nil, errOpen
	}
	h, err := ParseHeader(data[:HeaderSize])
	if err != nil {
		return nil, err
	}
	e := NewEncrypter(fileKey, h, plainSize)
	out := make([]byte, 0, plainSize)
	for i, rest := int64(0), data[HeaderSize:]; i < e.chunks; i++ {
		n := min(int64(len(rest)), ChunkSize+tagSize)
		pt, err := e.Open(i, rest[:n])
		if err != nil {
			return nil, err
		}
		out, rest = append(out, pt...), rest[n:]
	}
	return out, nil
}

// MaxThumbSize is the largest sealed thumbnail: a thumbnail of up to 512 KiB, locked.
const MaxThumbSize = 512<<10 + LockOverhead

// ThumbKey is the key a file's thumbnail is locked with.
func ThumbKey(fileKey []byte) []byte {
	k, err := hkdfKey(fileKey, nil, PurposeThumb)
	if err != nil {
		panic(err)
	}
	return k
}

// SealThumb locks a file's thumbnail with a key from the file's key.
func SealThumb(fileKey, jpeg []byte) []byte { return Lock(ThumbKey(fileKey), nil, jpeg) }

// OpenThumb decrypts what SealThumb made.
func OpenThumb(fileKey, sealed []byte) ([]byte, error) { return Unlock(ThumbKey(fileKey), nil, sealed) }
