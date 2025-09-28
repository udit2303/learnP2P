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
// Protocol (single workflow, no legacy):
// 1) Receiver sends: 0x01 | uint32(pubLen) | pubDER (RSA-4096 PKIX)
// 2) Sender replies: 0x02 | uint32(encKeyLen) | encKey(RSA-OAEP of AES key) | baseNonce(12)
// 3) Sender sends: uint32(len(cman)) | cman (GCM over manifest, AAD="manifest")
// 4) Sender streams chunks: [ uint32(len(ct)) | ct ]* using AAD=sha256(manifest.data)
func Send(conn net.Conn, filePath string) error {
	// Build manifest
	man, err := BuildManifest(filePath)
	if err != nil {
		return fmt.Errorf("build manifest: %w", err)
	}

	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)

	// 1) Read receiver's RSA public key message
	msgType, err := br.ReadByte()
	if err != nil {
		return fmt.Errorf("read receiver pubkey: %w", err)
	}
	if msgType != 0x01 {
		return fmt.Errorf("unexpected receiver message type: 0x%02x", msgType)
	}
	var pkLen uint32
	if err := binary.Read(br, binary.BigEndian, &pkLen); err != nil {
		return fmt.Errorf("read pubkey len: %w", err)
	}
	if pkLen == 0 || pkLen > 1_000_000 {
		return fmt.Errorf("invalid pubkey length: %d", pkLen)
	}
	pkDER := make([]byte, pkLen)
	if _, err := io.ReadFull(br, pkDER); err != nil {
		return fmt.Errorf("read pubkey der: %w", err)
	}
	pub, err := pcrypto.ParsePublicKeyDER(pkDER)
	if err != nil {
		return fmt.Errorf("parse pubkey: %w", err)
	}

	// 2) Create session key + base nonce, encrypt key with RSA-OAEP and send header v0x02
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
	encKey, err := pcrypto.EncryptKeyRSAOAEP(pub, key)
	if err != nil {
		return fmt.Errorf("rsa-oaep encrypt: %w", err)
	}
	if err := bw.WriteByte(0x02); err != nil { // header version
		return err
	}
	if err := binary.Write(bw, binary.BigEndian, uint32(len(encKey))); err != nil {
		return fmt.Errorf("write encKey len: %w", err)
	}
	if _, err := bw.Write(encKey); err != nil {
		return fmt.Errorf("write encKey: %w", err)
	}
	if _, err := bw.Write(base); err != nil {
		return fmt.Errorf("write base nonce: %w", err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("flush header: %w", err)
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

	// 3) Encrypted manifest
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

	// 4) Send file data in 1MB chunks
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
