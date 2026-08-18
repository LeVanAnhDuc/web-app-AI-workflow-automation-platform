// Package crypto seals credential payloads before they reach the database.
// Phase 1 stores no credentials, but the seam exists now so Phase 3 does not
// have to migrate plaintext secrets out of a table.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// ErrBadKey means the key was not 32 bytes.
var ErrBadKey = errors.New("crypto: key must be 32 bytes")

// ErrCorrupt means the ciphertext failed authentication — a wrong key, or a
// tampered row. The two are deliberately indistinguishable to the caller.
var ErrCorrupt = errors.New("crypto: ciphertext failed authentication")

// Sealer encrypts and decrypts credential blobs with AES-256-GCM. The nonce is
// stored in front of the ciphertext, so one blob is self-contained.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer builds a Sealer from a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, ErrBadKey
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("crypto: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("crypto: new GCM: %w", err)
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts plaintext, returning nonce || ciphertext.
func (s *Sealer) Seal(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("crypto: read nonce: %w", err)
	}
	// Appending onto the nonce slice keeps the layout in one allocation.
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

// Open decrypts a blob produced by Seal.
func (s *Sealer) Open(blob []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(blob) < n {
		return nil, ErrCorrupt
	}
	plain, err := s.aead.Open(nil, blob[:n], blob[n:], nil)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plain, nil
}

// SealJSON marshals a value and seals it, which is how a credential's fields
// are stored: one JSON object per credential, opaque to the database.
func (s *Sealer) SealJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("crypto: marshal payload: %w", err)
	}
	return s.Seal(raw)
}

// OpenJSON opens a blob and unmarshals it into dst.
func (s *Sealer) OpenJSON(blob []byte, dst any) error {
	raw, err := s.Open(blob)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("crypto: unmarshal payload: %w", err)
	}
	return nil
}
