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

// StartLocalServer starts a TCP server listening on the given port.
// It performs a simple handshake with password: expects "HELLO P2P/1 <peerName> <password>" and replies "WELCOME P2P/1 <ourName>" if password matches.
// Returns a shutdown function to stop listening. Only the first successful handshake is accepted; the listener closes after that.
func StartLocalServer(ourName string, port int, expectedPassword string) (func() error, error) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return nil, err
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// If listener closed, exit loop
				return
			}
			if ok := handleConn(ourName, expectedPassword, conn); ok {
				// Close listener after first successful handshake
				_ = ln.Close()
				return
			}
		}
	}()
	return ln.Close, nil
}

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

// ConnectLocal dials the given ip:port and performs the handshake.
// Returns the remote peer name on success.
func ConnectLocal(ip string, port int, ourName string, password string, timeout time.Duration) (string, error) {
	d := net.Dialer{Timeout: timeout}
	hostPort := net.JoinHostPort(ip, strconv.Itoa(port))
	conn, err := d.Dial("tcp", hostPort)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Send HELLO with protocol magic and password
	_, err = conn.Write([]byte("HELLO " + handshakeMagic + " " + ourName + " " + password + "\n"))
	if err != nil {
		return "", err
	}

	r := bufio.NewReader(conn)
	resp, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	resp = strings.TrimSpace(resp)
	const prefix = "WELCOME "
	if !strings.HasPrefix(resp, prefix) || len(resp) <= len(prefix) {
		return "", fmt.Errorf("invalid handshake response")
	}
	rest := strings.TrimSpace(resp[len(prefix):])
	if !strings.HasPrefix(rest, handshakeMagic+" ") || len(rest) <= len(handshakeMagic)+1 {
		return "", fmt.Errorf("invalid handshake magic")
	}
	return strings.TrimSpace(rest[len(handshakeMagic)+1:]), nil
}
