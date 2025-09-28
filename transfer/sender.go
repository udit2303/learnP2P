package transfer

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
)

const ChunkSize = 1 << 20 // 1MB

// Send streams the file: first the manifest (length-prefixed JSON), then chunks.
func Send(conn net.Conn, filePath string) error {
	// Build manifest
	man, err := BuildManifest(filePath)
	if err != nil {
		return fmt.Errorf("build manifest: %w", err)
	}

	// Send manifest length + bytes
	bw := bufio.NewWriter(conn)
	manBytes, _ := json.Marshal(man)
	if err := binary.Write(bw, binary.BigEndian, uint32(len(manBytes))); err != nil {
		return fmt.Errorf("write manifest len: %w", err)
	}
	if _, err := bw.Write(manBytes); err != nil {
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

	buf := make([]byte, ChunkSize)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			if _, werr := bw.Write(buf[:n]); werr != nil {
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
