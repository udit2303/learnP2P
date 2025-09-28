package transfer

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"net"
	"os"
	"path/filepath"
)

const PublicDir = "public"

// Receive reads manifest then file chunks, storing to public/<name>. It validates total size.
// TODO: For large files, consider hashing on the fly and compare to manifest.Hash (sha256).
func Receive(conn net.Conn) (Manifest, string, error) {
	br := bufio.NewReader(conn)

	// Read manifest length + bytes
	var mlen uint32
	if err := binary.Read(br, binary.BigEndian, &mlen); err != nil {
		return Manifest{}, "", fmt.Errorf("read manifest len: %w", err)
	}
	mbytes := make([]byte, mlen)
	if _, err := io.ReadFull(br, mbytes); err != nil {
		return Manifest{}, "", fmt.Errorf("read manifest: %w", err)
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
	buf := make([]byte, ChunkSize)
	for written < man.Size {
		need := man.Size - written
		if need < int64(len(buf)) {
			buf = buf[:need]
		}
		n, rerr := br.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return Manifest{}, "", fmt.Errorf("write file: %w", werr)
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
		}
		if rerr != nil {
			if rerr == io.EOF && written == man.Size {
				break
			}
			return Manifest{}, "", fmt.Errorf("receive chunk: %w", rerr)
		}
	}

	if err := out.Close(); err != nil {
		return Manifest{}, "", fmt.Errorf("close output: %w", err)
	}
	if err := os.Rename(tmpPath, outPath); err != nil {
		return Manifest{}, "", fmt.Errorf("finalize file: %w", err)
	}

	return man, outPath, nil
}
