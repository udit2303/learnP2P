package transfer

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"net"
	"os"
	"path/filepath"

	pcrypto "learnP2P/crypto"
)

const PublicDir = "public"

// Receive reads manifest then file chunks, storing to public/<name>. It validates total size.
// TODO: For large files, consider hashing on the fly and compare to manifest.Hash (sha256).
func Receive(conn net.Conn) (Manifest, string, error) {
	br := bufio.NewReader(conn)

	// 1) Read header: version, key, base nonce
	ver, err := br.ReadByte()
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read header version: %w", err)
	}
	if ver != 0x01 {
		return Manifest{}, "", fmt.Errorf("unknown header version: %d", ver)
	}
	key := make([]byte, pcrypto.KeySize)
	if _, err := io.ReadFull(br, key); err != nil {
		return Manifest{}, "", fmt.Errorf("read key: %w", err)
	}
	base := make([]byte, pcrypto.NonceSize)
	if _, err := io.ReadFull(br, base); err != nil {
		return Manifest{}, "", fmt.Errorf("read nonce: %w", err)
	}
	aead, err := pcrypto.NewGCM(key)
	if err != nil {
		return Manifest{}, "", err
	}
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

	// 2) Read encrypted manifest
	var clen uint32
	if err := binary.Read(br, binary.BigEndian, &clen); err != nil {
		return Manifest{}, "", fmt.Errorf("read manifest len: %w", err)
	}
	cman := make([]byte, clen)
	if _, err := io.ReadFull(br, cman); err != nil {
		return Manifest{}, "", fmt.Errorf("read manifest: %w", err)
	}
	mbytes, err := aead.Open(nil, nonceFor(), cman, []byte("manifest"))
	if err != nil {
		return Manifest{}, "", fmt.Errorf("decrypt manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(mbytes, &man); err != nil {
		return Manifest{}, "", fmt.Errorf("decode manifest: %w", err)
	}

	// Ensure public dir exists
	if err := os.MkdirAll(PublicDir, 0o755); err != nil {
		return Manifest{}, "", fmt.Errorf("mkdir public: %w", err)
	}
	outPath := filepath.Join(PublicDir, man.Name)
	tmpPath := outPath + ".part"

	// Receive file data
	out, err := os.Create(tmpPath)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("create file: %w", err)
	}
	defer out.Close()

	var written int64
	// Optional lightweight checksum during transfer (CRC32)
	var h hash.Hash32 = crc32.NewIEEE()
	// AAD bytes for chunks
	hashBytes, derr := hex.DecodeString(man.Hash)
	if derr != nil {
		return Manifest{}, "", fmt.Errorf("decode hash: %w", derr)
	}
	for written < man.Size {
		// Each incoming chunk is len+ciphertext
		var clen uint32
		if err := binary.Read(br, binary.BigEndian, &clen); err != nil {
			if err == io.EOF && written == man.Size {
				break
			}
			return Manifest{}, "", fmt.Errorf("read chunk len: %w", err)
		}
		ct := make([]byte, clen)
		if _, err := io.ReadFull(br, ct); err != nil {
			return Manifest{}, "", fmt.Errorf("read chunk: %w", err)
		}
		pt, err := aead.Open(nil, nonceFor(), ct, hashBytes)
		if err != nil {
			return Manifest{}, "", fmt.Errorf("decrypt chunk: %w", err)
		}
		if _, werr := out.Write(pt); werr != nil {
			return Manifest{}, "", fmt.Errorf("write file: %w", werr)
		}
		_, _ = h.Write(pt)
		written += int64(len(pt))
	}

	if err := out.Close(); err != nil {
		return Manifest{}, "", fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return Manifest{}, "", fmt.Errorf("finalize file: %w", err)
	}

	return man, outPath, nil
}
