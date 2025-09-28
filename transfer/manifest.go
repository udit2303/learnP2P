package transfer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// Manifest describes the file to transfer.
type Manifest struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Hash string `json:"hash"` // hex-encoded SHA-256 of the file contents
}

// BuildManifest computes the SHA-256 and size for a local file.
func BuildManifest(path string) (Manifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return Manifest{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return Manifest{}, err
	}
	sum := h.Sum(nil)
	return Manifest{
		Name: fileName(path),
		Size: n,
		Hash: hex.EncodeToString(sum),
	}, nil
}

func fileName(path string) string {
	// Minimal path base without importing filepath for simplicity
	i := len(path) - 1
	for i >= 0 {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
		i--
	}
	return path
}

// Pretty returns a human readable string for the manifest.
func (m Manifest) Pretty() string {
	return fmt.Sprintf("%s (%d bytes, sha256=%s)", m.Name, m.Size, m.Hash)
}
