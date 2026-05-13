// Package servercrypto encrypts blobs the backend itself needs to read
// later (DDNS API tokens, other autonomous-worker secrets). The key is
// derived from the JWT secret via HKDF-SHA256 so it's stable across
// restarts but never written to disk in cleartext.
//
// This is intentionally NOT zero-knowledge: anyone with the SQLite file
// AND the JWT secret can decrypt. That's the trade-off for "DDNS keeps
// running while the browser is closed". For user-facing secrets see the
// vault package, which is zero-knowledge.
package servercrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

type Cipher struct{ aead cipher.AEAD }

// New derives an AES-256-GCM AEAD from the JWT secret. The "info" string
// scopes the derived key so different callers (e.g. ddns vs. other future
// modules) can use the same JWT secret without sharing key material —
// just pass a different info string.
func New(jwtSecret []byte, info string) (*Cipher, error) {
	if len(jwtSecret) < 16 {
		return nil, fmt.Errorf("jwt secret too short (<16 bytes)")
	}
	r := hkdf.New(sha256.New, jwtSecret, nil, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("hkdf: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns (ciphertext, nonce). The nonce is freshly randomized
// per call, so re-encrypting the same plaintext yields different output.
func (c *Cipher) Encrypt(plaintext []byte) (ciphertext, nonce []byte, err error) {
	nonce = make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	ciphertext = c.aead.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

func (c *Cipher) Decrypt(ciphertext, nonce []byte) ([]byte, error) {
	return c.aead.Open(nil, nonce, ciphertext, nil)
}
