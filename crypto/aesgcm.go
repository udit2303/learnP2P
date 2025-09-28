package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"io"
)

// KeySize is the AES-256 key length in bytes.
const KeySize = 32

// NonceSize is the GCM nonce length in bytes (12 recommended).
const NonceSize = 12

// GenerateKey returns a random 32-byte key.
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	return key, nil
}

// NewGCM returns an AEAD cipher from a 32-byte key.
func NewGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, errors.New("invalid key length")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
