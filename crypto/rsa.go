package crypto

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"sync"
)

// GenerateRSA4096 creates a new 4096-bit RSA private key.
func GenerateRSA4096() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 4096)
}

// MarshalPublicKeyDER encodes the public key in PKIX/DER format.
func MarshalPublicKeyDER(pub *rsa.PublicKey) ([]byte, error) {
	return x509.MarshalPKIXPublicKey(pub)
}

// ParsePublicKeyDER parses a PKIX/DER public key into *rsa.PublicKey.
func ParsePublicKeyDER(der []byte) (*rsa.PublicKey, error) {
	pk, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pub, ok := pk.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}
	return pub, nil
}

// EncryptKeyRSAOAEP encrypts data (e.g., AES key) using RSA-OAEP with SHA-256.
func EncryptKeyRSAOAEP(pub *rsa.PublicKey, data []byte) ([]byte, error) {
	h := sha256.New()
	return rsa.EncryptOAEP(h, rand.Reader, pub, data, nil)
}

// DecryptKeyRSAOAEP decrypts data using RSA-OAEP with SHA-256.
func DecryptKeyRSAOAEP(priv *rsa.PrivateKey, ct []byte) ([]byte, error) {
	h := sha256.New()
	return rsa.DecryptOAEP(h, rand.Reader, priv, ct, nil)
}

var (
	cachedPriv *rsa.PrivateKey
	once       sync.Once
	onceErr    error
)

// GetOrCreateRSA4096 returns a process-wide RSA-4096 key, generating it on first use.
func GetOrCreateRSA4096() (*rsa.PrivateKey, error) {
	once.Do(func() {
		cachedPriv, onceErr = GenerateRSA4096()
	})
	return cachedPriv, onceErr
}
