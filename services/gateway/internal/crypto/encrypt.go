package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

const (
	// KeySize is 32 bytes for AES-256
	KeySize = 32
	// NonceSize is standard 12 bytes for GCM
	NonceSize = 12
)

var (
	ErrInvalidKeySize    = errors.New("encryption key must be exactly 32 bytes")
	ErrInvalidNonceSize  = errors.New("nonce must be exactly 12 bytes")
	ErrDecryptionFailed  = errors.New("decryption failed (ciphertext corrupted or tampered)")
	ErrEmptyPlaintext    = errors.New("plaintext cannot be empty")
)

// Encryptor encrypts and decrypts sensitive data such as upstream credentials.
type Encryptor interface {
	Encrypt(plaintext []byte) (ciphertext []byte, nonce []byte, err error)
	Decrypt(ciphertext, nonce []byte) (plaintext []byte, err error)
}

// GCMEncryptor implements Encryptor using AES-256-GCM.
type GCMEncryptor struct {
	key []byte
}

// ParseKey parses a 32-byte key from either a raw 32-byte string/slice or a base64-encoded string.
func ParseKey(rawOrBase64 string) ([]byte, error) {
	// Try base64 decode first
	if decoded, err := base64.StdEncoding.DecodeString(rawOrBase64); err == nil && len(decoded) == KeySize {
		return decoded, nil
	}
	// If already 32 raw bytes
	keyBytes := []byte(rawOrBase64)
	if len(keyBytes) == KeySize {
		return keyBytes, nil
	}
	return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKeySize, len(keyBytes))
}

// NewGCMEncryptor creates a new GCMEncryptor with the given 32-byte key.
func NewGCMEncryptor(key []byte) (*GCMEncryptor, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: got %d bytes", ErrInvalidKeySize, len(key))
	}
	// Verify that AES cipher can be created with this key
	if _, err := aes.NewCipher(key); err != nil {
		return nil, fmt.Errorf("failed to initialize AES cipher: %w", err)
	}
	return &GCMEncryptor{key: key}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM with a fresh random 12-byte nonce.
func (e *GCMEncryptor) Encrypt(plaintext []byte) ([]byte, []byte, error) {
	if len(plaintext) == 0 {
		return nil, nil, ErrEmptyPlaintext
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, nil, fmt.Errorf("cipher init failed: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("gcm init failed: %w", err)
	}

	// Always generate a fresh cryptographic random 12-byte nonce
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}

	// Seal appends ciphertext and authentication tag
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// Decrypt decrypts ciphertext using AES-256-GCM and verifies authentication tag.
func (e *GCMEncryptor) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	if len(nonce) != NonceSize {
		return nil, ErrInvalidNonceSize
	}

	block, err := aes.NewCipher(e.key)
	if err != nil {
		return nil, fmt.Errorf("cipher init failed: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm init failed: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}
