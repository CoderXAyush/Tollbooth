package crypto

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func TestGCMEncryptor(t *testing.T) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate random key: %v", err)
	}

	enc, err := NewGCMEncryptor(key)
	if err != nil {
		t.Fatalf("failed to create encryptor: %v", err)
	}

	t.Run("Round-trip success", func(t *testing.T) {
		secret := []byte("super-secret-upstream-token-12345")
		ciphertext, nonce, err := enc.Encrypt(secret)
		if err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}
		if len(nonce) != NonceSize {
			t.Fatalf("expected nonce size %d, got %d", NonceSize, len(nonce))
		}
		if bytes.Equal(ciphertext, secret) {
			t.Fatal("ciphertext must not match plaintext")
		}

		decrypted, err := enc.Decrypt(ciphertext, nonce)
		if err != nil {
			t.Fatalf("decrypt failed: %v", err)
		}
		if !bytes.Equal(decrypted, secret) {
			t.Fatalf("decrypted value mismatch: got %q, want %q", decrypted, secret)
		}
	})

	t.Run("Fresh random nonce per encryption", func(t *testing.T) {
		secret := []byte("constant-plaintext")
		c1, n1, err := enc.Encrypt(secret)
		if err != nil {
			t.Fatal(err)
		}
		c2, n2, err := enc.Encrypt(secret)
		if err != nil {
			t.Fatal(err)
		}

		if bytes.Equal(n1, n2) {
			t.Fatal("two successive encryptions produced identical nonces!")
		}
		if bytes.Equal(c1, c2) {
			t.Fatal("two successive encryptions produced identical ciphertexts!")
		}
	})

	t.Run("Tamper detection: modified ciphertext", func(t *testing.T) {
		secret := []byte("secret-token")
		ciphertext, nonce, err := enc.Encrypt(secret)
		if err != nil {
			t.Fatal(err)
		}

		// Tamper with one byte of ciphertext
		tamperedCiphertext := make([]byte, len(ciphertext))
		copy(tamperedCiphertext, ciphertext)
		tamperedCiphertext[0] ^= 0xFF

		_, err = enc.Decrypt(tamperedCiphertext, nonce)
		if err == nil {
			t.Fatal("expected decryption error on tampered ciphertext, got nil")
		}
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Fatalf("expected ErrDecryptionFailed, got: %v", err)
		}
	})

	t.Run("Tamper detection: modified nonce", func(t *testing.T) {
		secret := []byte("secret-token")
		ciphertext, nonce, err := enc.Encrypt(secret)
		if err != nil {
			t.Fatal(err)
		}

		tamperedNonce := make([]byte, len(nonce))
		copy(tamperedNonce, nonce)
		tamperedNonce[0] ^= 0x01

		_, err = enc.Decrypt(ciphertext, tamperedNonce)
		if err == nil {
			t.Fatal("expected decryption error on tampered nonce, got nil")
		}
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Fatalf("expected ErrDecryptionFailed, got: %v", err)
		}
	})

	t.Run("Invalid nonce size", func(t *testing.T) {
		secret := []byte("secret-token")
		ciphertext, _, err := enc.Encrypt(secret)
		if err != nil {
			t.Fatal(err)
		}

		badNonce := make([]byte, 8)
		_, err = enc.Decrypt(ciphertext, badNonce)
		if !errors.Is(err, ErrInvalidNonceSize) {
			t.Fatalf("expected ErrInvalidNonceSize, got: %v", err)
		}
	})

	t.Run("Empty plaintext rejected", func(t *testing.T) {
		_, _, err := enc.Encrypt([]byte{})
		if !errors.Is(err, ErrEmptyPlaintext) {
			t.Fatalf("expected ErrEmptyPlaintext, got: %v", err)
		}
	})

	t.Run("Invalid key length", func(t *testing.T) {
		shortKey := make([]byte, 16)
		_, err := NewGCMEncryptor(shortKey)
		if err == nil {
			t.Fatal("expected error on 16-byte key, got nil")
		}
	})
}

func TestParseKey(t *testing.T) {
	raw32 := make([]byte, KeySize)
	for i := range raw32 {
		raw32[i] = byte(i)
	}

	b64Key := base64.StdEncoding.EncodeToString(raw32)
	parsed, err := ParseKey(b64Key)
	if err != nil {
		t.Fatalf("failed to parse base64 key: %v", err)
	}
	if !bytes.Equal(parsed, raw32) {
		t.Fatalf("parsed key mismatch: got %v, want %v", parsed, raw32)
	}

	// Raw 32-byte string
	rawStr := string(raw32)
	parsedRaw, err := ParseKey(rawStr)
	if err != nil {
		t.Fatalf("failed to parse raw key: %v", err)
	}
	if !bytes.Equal(parsedRaw, raw32) {
		t.Fatalf("parsed raw key mismatch")
	}

	// Invalid length
	_, err = ParseKey("too-short")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}
