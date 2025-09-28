package transfer

import (
	"bufio"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"

	pcrypto "learnP2P/crypto"
)

const ChunkSize = 1 << 20 // 1MB

// Send streams the file with AES-GCM encryption.
func Send(conn net.Conn, filePath string) error {
	// Build manifest
	man, err := BuildManifest(filePath)
	if err != nil {
		return fmt.Errorf("build manifest: %w", err)
	}

	bw := bufio.NewWriter(conn)

	// Create session: key + base nonce
	key, err := pcrypto.GenerateKey()
	if err != nil {
		return fmt.Errorf("gen key: %w", err)
	}
	aead, err := pcrypto.NewGCM(key)
	if err != nil {
		return fmt.Errorf("gcm: %w", err)
	}
	base := make([]byte, pcrypto.NonceSize)
	if _, err := rand.Read(base); err != nil {
		return fmt.Errorf("nonce: %w", err)
	}
	// Write header: version(1) | key(32) | baseNonce(12)
	if err := bw.WriteByte(0x01); err != nil {
		return err
	}
	if _, err := bw.Write(key); err != nil {
		return err
	}
	if _, err := bw.Write(base); err != nil {
		return err
	}

	// nonce counter in last 4 bytes (big endian)
	var ctr uint32
	nonceFor := func() []byte {
		n := make([]byte, len(base))
		copy(n, base)
		i := len(n) - 4
		n[i+0] = byte(ctr >> 24)
		n[i+1] = byte(ctr >> 16)
		n[i+2] = byte(ctr >> 8)
		n[i+3] = byte(ctr)
		ctr++
		return n
	}

	// Encrypted manifest
	manBytes, _ := json.Marshal(man)
	cman := aead.Seal(nil, nonceFor(), manBytes, []byte("manifest"))
	if err := binary.Write(bw, binary.BigEndian, uint32(len(cman))); err != nil {
		return fmt.Errorf("write manifest len: %w", err)
	}
	if _, err := bw.Write(cman); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("flush manifest: %w", err)
	}

	// Send file data in 1MB chunks
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	// AAD for chunks = manifest hash bytes
	hashBytes, derr := hex.DecodeString(man.Hash)
	if derr != nil {
		return fmt.Errorf("decode hash: %w", derr)
	}

	buf := make([]byte, ChunkSize)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			// Encrypt with AAD = manifest hash
			ct := aead.Seal(nil, nonceFor(), buf[:n], hashBytes)
			if err := binary.Write(bw, binary.BigEndian, uint32(len(ct))); err != nil {
				return fmt.Errorf("write chunk len: %w", err)
			}
			if _, werr := bw.Write(ct); werr != nil {
				return fmt.Errorf("write chunk: %w", werr)
			}
		}
		if rerr == io.EOF {
			if err := bw.Flush(); err != nil {
				return fmt.Errorf("flush chunks: %w", err)
			}
			break
		}
		if rerr != nil {
			return fmt.Errorf("read file: %w", rerr)
		}
	}
	return nil
}
