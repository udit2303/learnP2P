package transfer

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"

	pcrypto "learnP2P/crypto"
)

const PublicDir = "public"

// Receive reads manifest then file chunks, storing to public/<name>. It validates total size.
func Receive(conn net.Conn) (Manifest, string, error) {
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)

	// 0) Send our RSA public key first: 0x01 | uint32(len) | pubDER
	priv, err := pcrypto.GetOrCreateRSA4096()
	if err != nil {
		return Manifest{}, "", fmt.Errorf("rsa key: %w", err)
	}
	pubDER, err := pcrypto.MarshalPublicKeyDER(&priv.PublicKey)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("marshal pubkey: %w", err)
	}
	if err := bw.WriteByte(0x01); err != nil {
		return Manifest{}, "", fmt.Errorf("write pubkey tag: %w", err)
	}
	if err := binary.Write(bw, binary.BigEndian, uint32(len(pubDER))); err != nil {
		return Manifest{}, "", fmt.Errorf("write pubkey len: %w", err)
	}
	if _, err := bw.Write(pubDER); err != nil {
		return Manifest{}, "", fmt.Errorf("write pubkey der: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return Manifest{}, "", fmt.Errorf("flush pubkey: %w", err)
	}

	// 1) Read header: version(0x02), encKeyLen, encKey(RSA-OAEP), base nonce
	ver, err := br.ReadByte()
	if err != nil {
		return Manifest{}, "", fmt.Errorf("read header version: %w", err)
	}
	if ver != 0x02 {
		return Manifest{}, "", fmt.Errorf("unexpected header version: %d", ver)
	}
	var ekLen uint32
	if err := binary.Read(br, binary.BigEndian, &ekLen); err != nil {
		return Manifest{}, "", fmt.Errorf("read encKey len: %w", err)
	}
	if ekLen == 0 || ekLen > 10_000 { // RSA-4096 OAEP ciphertext size is ~512 bytes
		return Manifest{}, "", fmt.Errorf("invalid encKey len: %d", ekLen)
	}
	encKey := make([]byte, ekLen)
	if _, err := io.ReadFull(br, encKey); err != nil {
		return Manifest{}, "", fmt.Errorf("read encKey: %w", err)
	}
	base := make([]byte, pcrypto.NonceSize)
	if _, err := io.ReadFull(br, base); err != nil {
		return Manifest{}, "", fmt.Errorf("read base nonce: %w", err)
	}
	key, err := pcrypto.DecryptKeyRSAOAEP(priv, encKey)
	if err != nil {
		return Manifest{}, "", fmt.Errorf("rsa-oaep decrypt: %w", err)
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
	// Compute SHA-256 on the fly and compare to manifest at the end
	h := sha256.New()
	// AAD bytes for chunks
	hashBytes, derr := hex.DecodeString(man.Hash)
	if derr != nil {
		return Manifest{}, "", fmt.Errorf("decode hash: %w", derr)
	}
	start := time.Now()
	lastTick := time.Time{}
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

		now := time.Now()
		if lastTick.IsZero() || now.Sub(lastTick) >= 200*time.Millisecond {
			printProgress("Receiving", man.Name, written, man.Size, start)
			lastTick = now
		}
	}

	if err := out.Close(); err != nil {
		return Manifest{}, "", fmt.Errorf("close output: %w", err)
	}
	// Final progress update
	printProgress("Receiving", man.Name, written, man.Size, start)
	fmt.Print("\n")

	// Verify SHA-256 matches manifest, with simple logging
	fmt.Printf("Verifying integrity (SHA-256) for %s... ", man.Name)
	vstart := time.Now()
	calc := hex.EncodeToString(h.Sum(nil))
	if calc != man.Hash {
		fmt.Printf("FAILED (expected %s, got %s)\n", man.Hash, calc)
		// Cleanup partial file
		_ = out.Close()
		_ = os.Remove(tmpPath)
		return Manifest{}, "", fmt.Errorf("hash mismatch: got %s, expected %s", calc, man.Hash)
	}
	fmt.Printf("OK (took %s)\n", time.Since(vstart).Round(time.Millisecond))

	if err := os.Rename(tmpPath, outPath); err != nil {
		return Manifest{}, "", fmt.Errorf("finalize file: %w", err)
	}

	return man, outPath, nil
}
