// Package secrets provides random values, hashing and AES-256-GCM sealing for server-side token storage.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
)

// RandomToken returns n random bytes encoded as unpadded base64url.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func RandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return b
}

// Hash is the SHA-256 digest used to store opaque tokens (sessions, state, flow bindings).
func Hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

// HMAC returns base64url(HMAC-SHA256(key, msg)).
func HMAC(key []byte, msg string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(msg))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Equal compares two strings in constant time.
func Equal(a, b string) bool { return hmac.Equal([]byte(a), []byte(b)) }

// Sealer encrypts small secrets (refresh tokens, PKCE verifiers) at rest.
type Sealer struct{ aead cipher.AEAD }

func NewSealer(key []byte) (*Sealer, error) {
	if len(key) < 32 {
		return nil, errors.New("encryption key must be at least 32 bytes")
	}
	k := sha256.Sum256(key) // normalise arbitrary-length key material to 32 bytes
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

func (s *Sealer) Seal(plain []byte, aad string) []byte {
	nonce := RandomBytes(s.aead.NonceSize())
	return append(nonce, s.aead.Seal(nil, nonce, plain, []byte(aad))...)
}

func (s *Sealer) Open(sealed []byte, aad string) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return nil, errors.New("sealed value too short")
	}
	return s.aead.Open(nil, sealed[:n], sealed[n:], []byte(aad))
}
