// Package seal encrypts small secrets at rest with AES-256-GCM under a server key (ZELDA_SECRET_KEY).
package seal

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
)

// KeySize is the key length in bytes.
const KeySize = 32

// version prefixes every box so the format can change later.
const version byte = 1

var (
	// ErrKey means the key is not 32 bytes in a supported encoding.
	ErrKey = errors.New("ZELDA_SECRET_KEY must be 32 random bytes as base64 or hex (for example: openssl rand -base64 32)")
	// ErrOpen means the box is malformed, was sealed under another key or
	// AAD, or was modified.
	ErrOpen = errors.New("sealed value cannot be opened")
)

// ParseKey decodes a 32-byte key given as standard or URL-safe base64
// (padding optional) or hex.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := hex.DecodeString(s); err == nil && len(b) == KeySize {
		return b, nil
	}
	trimmed := strings.TrimRight(s, "=")
	for _, enc := range []*base64.Encoding{base64.RawStdEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(trimmed); err == nil && len(b) == KeySize {
			return b, nil
		}
	}
	return nil, ErrKey
}

// Sealer encrypts and decrypts with one key. It is safe for concurrent use.
type Sealer struct{ aead cipher.AEAD }

// New builds a Sealer from a KeySize-byte key.
func New(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, ErrKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext. The result is a version byte, a random nonce and
// the ciphertext. aad binds the box to its record (for example a user ID):
// Open fails if a different aad is given.
func (s *Sealer) Seal(plaintext, aad []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+s.aead.Overhead())
	out = append(out, version)
	out = append(out, nonce...)
	return s.aead.Seal(out, nonce, plaintext, aad)
}

// Open decrypts a box made by Seal with the same aad.
func (s *Sealer) Open(box, aad []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(box) < 1+n+s.aead.Overhead() || box[0] != version {
		return nil, ErrOpen
	}
	plain, err := s.aead.Open(nil, box[1:1+n], box[1+n:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return plain, nil
}
