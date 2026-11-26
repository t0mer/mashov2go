// Package crypto seals secrets at rest with AES-256-GCM (CLAUDE.md §12).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Environment variables that carry the master key. The key is required; the
// service refuses to start without it.
const (
	EnvMasterKey     = "MASHOV2GO_MASTER_KEY"
	EnvMasterKeyFile = "MASHOV2GO_MASTER_KEY_FILE"
)

// ErrNoMasterKey is returned when neither key variable is set.
var ErrNoMasterKey = errors.New("no master key: set " + EnvMasterKey + " or " + EnvMasterKeyFile)

// Sealer encrypts and decrypts with one AES-256-GCM key.
type Sealer struct {
	aead cipher.AEAD
}

// NewSealer builds a Sealer from a 32-byte key.
func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
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

// Seal encrypts plaintext. aad binds the ciphertext to its context (for example
// the row and column it is stored in), so it cannot be moved elsewhere.
// Output layout: nonce || ciphertext+tag.
func (s *Sealer) Seal(plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, aad), nil
}

// Open decrypts a value produced by Seal with the same aad.
func (s *Sealer) Open(sealed, aad []byte) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, errors.New("sealed value too short")
	}
	pt, err := s.aead.Open(nil, sealed[:n], sealed[n:], aad)
	if err != nil {
		return nil, errors.New("decryption failed: wrong key or context")
	}
	return pt, nil
}

// LoadMasterKey reads the base64-encoded 32-byte key from the environment: the
// value of EnvMasterKey, or the file named by EnvMasterKeyFile.
func LoadMasterKey(getenv func(string) string, readFile func(string) ([]byte, error)) ([]byte, error) {
	raw := strings.TrimSpace(getenv(EnvMasterKey))
	if raw == "" {
		if path := getenv(EnvMasterKeyFile); path != "" {
			b, err := readFile(path)
			if err != nil {
				return nil, fmt.Errorf("reading %s: %w", EnvMasterKeyFile, err)
			}
			raw = strings.TrimSpace(string(b))
		}
	}
	if raw == "" {
		return nil, ErrNoMasterKey
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("master key is not base64: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

// OSLoadMasterKey is LoadMasterKey wired to the process environment and files.
func OSLoadMasterKey() ([]byte, error) {
	return LoadMasterKey(os.Getenv, os.ReadFile)
}
