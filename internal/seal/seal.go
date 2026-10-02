// Package seal encrypts small secrets at rest with AES-256-GCM under a
// server key (ZELDA_SECRET_KEY).
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

// KeySize is the required key length in bytes.
const KeySize = 32

const version byte = 1

var (
	// ErrKey means the configured key is not 32 bytes of base64 or hex.
	ErrKey = errors.New("secret key must be 32 bytes, given as base64 or 64 hex characters")
	// ErrOpen means a sealed value is corrupt, was sealed under another key,
	// or belongs to a different record.
	ErrOpen = errors.New("cannot decrypt secret")
)

// ParseKey decodes a 32-byte key written as standard or URL-safe base64
// (with or without padding) or as hex.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if len(s) == 2*KeySize {
		if b, err := hex.DecodeString(s); err == nil {
			return b, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil && len(b) == KeySize {
			return b, nil
		}
	}
	return nil, ErrKey
}

// Sealer encrypts and decrypts with one key. It is safe for concurrent use.
type Sealer struct{ aead cipher.AEAD }

// New builds a Sealer from a 32-byte key.
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

// Seal encrypts plaintext. aad binds the result to its record (for example
// a block ID) so a sealed value copied to another record will not open.
// The output is a version byte, a random nonce and the ciphertext.
func (s *Sealer) Seal(plaintext, aad []byte) []byte {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	out := make([]byte, 0, 1+len(nonce)+len(plaintext)+s.aead.Overhead())
	out = append(append(out, version), nonce...)
	return s.aead.Seal(out, nonce, plaintext, aad)
}

// Open decrypts a value from Seal with the same aad.
func (s *Sealer) Open(box, aad []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(box) < 1+n+s.aead.Overhead() || box[0] != version {
		return nil, ErrOpen
	}
	pt, err := s.aead.Open(nil, box[1:1+n], box[1+n:], aad)
	if err != nil {
		return nil, ErrOpen
	}
	return pt, nil
}
