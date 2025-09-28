package connections

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

const handshakeMagic = "P2P/1"

// HandshakeMagic exposes the protocol marker used in local handshakes.
func HandshakeMagic() string { return handshakeMagic }

// (Removed) StartLocalServer: keep API surface minimal; use ListenAndAcceptOnce instead.

func handleConn(ourName string, expectedPassword string, conn net.Conn) bool {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(conn)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	const prefix = "HELLO "
	// Expect "HELLO P2P/1 <peerName>"
	if !strings.HasPrefix(line, prefix) || len(line) <= len(prefix) {
		// Invalid handshake; ignore
		return false
	}
	rest := strings.TrimSpace(line[len(prefix):])
	if !strings.HasPrefix(rest, handshakeMagic+" ") || len(rest) <= len(handshakeMagic)+1 {
		return false
	}
	rest = strings.TrimSpace(rest[len(handshakeMagic)+1:])
	parts := strings.Fields(rest)
	if len(parts) < 2 {
		return false
	}
	peerName := parts[0]
	providedPassword := parts[1]
	if providedPassword != expectedPassword {
		// Deny
		_, _ = conn.Write([]byte("DENY " + handshakeMagic + "\n"))
		return false
	}
	// Respond success
	_, _ = conn.Write([]byte("WELCOME " + handshakeMagic + " " + ourName + "\n"))
	log.Printf("Local connection established with %s (%s)", peerName, conn.RemoteAddr())
	return true
}

// ListenAndAcceptOnce listens on port and returns the first connection that completes
// a valid password-protected handshake. The returned connection remains open for the caller.
func ListenAndAcceptOnce(ourName string, port int, expectedPassword string) (net.Conn, string, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, "", err
	}
	defer ln.Close()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil, "", err
		}
		// Perform handshake manually without closing conn
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		r := bufio.NewReader(conn)
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		const prefix = "HELLO "
		if !strings.HasPrefix(line, prefix) || len(line) <= len(prefix) {
			conn.Close()
			continue
		}
		rest := strings.TrimSpace(line[len(prefix):])
		if !strings.HasPrefix(rest, handshakeMagic+" ") || len(rest) <= len(handshakeMagic)+1 {
			conn.Close()
			continue
		}
		rest = strings.TrimSpace(rest[len(handshakeMagic)+1:])
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			conn.Close()
			continue
		}
		peerName := parts[0]
		providedPassword := parts[1]
		if providedPassword != expectedPassword {
			_, _ = conn.Write([]byte("DENY " + handshakeMagic + "\n"))
			conn.Close()
			continue
		}
		// Success
		_, _ = conn.Write([]byte("WELCOME " + handshakeMagic + " " + ourName + "\n"))
		_ = conn.SetDeadline(time.Time{})
		log.Printf("Local connection established with %s (%s)", peerName, conn.RemoteAddr())
		return conn, peerName, nil
	}
}

// ConnectLocal dials the given ip:port and performs the handshake.
// Returns the remote peer name on success.
// (Removed) ConnectLocal: callers should use DialAndHandshake when they need an open connection.

// DialAndHandshake establishes a TCP connection and completes the handshake, returning the open connection.
func DialAndHandshake(ip string, port int, ourName string, password string, timeout time.Duration) (net.Conn, string, error) {
	d := net.Dialer{Timeout: timeout}
	hostPort := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := d.Dial("tcp", hostPort)
	if err != nil {
		return nil, "", err
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Send HELLO with protocol magic and password
	_, err = conn.Write([]byte("HELLO " + handshakeMagic + " " + ourName + " " + password + "\n"))
	if err != nil {
		conn.Close()
		return nil, "", err
	}

	r := bufio.NewReader(conn)
	resp, err := r.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, "", err
	}
	resp = strings.TrimSpace(resp)
	const prefix = "WELCOME "
	if !strings.HasPrefix(resp, prefix) || len(resp) <= len(prefix) {
		conn.Close()
		return nil, "", fmt.Errorf("invalid handshake response")
	}
	rest := strings.TrimSpace(resp[len(prefix):])
	if !strings.HasPrefix(rest, handshakeMagic+" ") || len(rest) <= len(handshakeMagic)+1 {
		conn.Close()
		return nil, "", fmt.Errorf("invalid handshake magic")
	}
	peer := strings.TrimSpace(rest[len(handshakeMagic)+1:])
	_ = conn.SetDeadline(time.Time{})
	return conn, peer, nil
}
