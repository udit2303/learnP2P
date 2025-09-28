package connections

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// dcConn adapts a WebRTC DataChannel to a stream-like net.Conn for reuse of transfer utilities.
type dcConn struct {
	dc     *webrtc.DataChannel
	msgs   chan []byte
	cur    []byte
	mu     sync.Mutex
	closed bool

	// deadlines are accepted but not enforced (best-effort no-op)
	rd, wd time.Time

	// backpressure for writes
	lowCh     chan struct{}
	threshold uint64 // BufferedAmountLowThreshold
	highWater uint64 // when BufferedAmount exceeds this, wait
}

// newDCConn wraps the given DataChannel.
func newDCConn(dc *webrtc.DataChannel) *dcConn {
	c := &dcConn{
		dc:    dc,
		msgs:  make(chan []byte, 64),
		lowCh: make(chan struct{}, 1),
	}
	// Configure backpressure thresholds
	c.threshold = 1 << 20 // 1 MiB
	c.highWater = c.threshold * 4
	dc.SetBufferedAmountLowThreshold(c.threshold)
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		// Copy buffer because msg.Data is reused by pion
		b := make([]byte, len(msg.Data))
		copy(b, msg.Data)
		c.mu.Lock()
		closed := c.closed
		c.mu.Unlock()
		if !closed {
			c.msgs <- b
		}
	})
	dc.OnBufferedAmountLow(func() {
		select {
		case c.lowCh <- struct{}{}:
		default:
		}
	})
	dc.OnClose(func() {
		c.mu.Lock()
		if !c.closed {
			c.closed = true
			close(c.msgs)
		}
		c.mu.Unlock()
		// wake waiters
		select {
		case c.lowCh <- struct{}{}:
		default:
		}
	})
	return c
}

// DataChannelConn returns a net.Conn-like wrapper over the peer's data channel.
// It waits until the data channel is available/open.
func (p *Peer) DataChannelConn() (net.Conn, error) {
	dc := p.getDataChannel()
	if dc == nil {
		// wait for readiness signal
		select {
		case <-p.DataChannelReady():
			dc = p.getDataChannel()
		case <-p.Connected(): // fallback: connection completed but dc not set
			dc = p.getDataChannel()
		case <-time.After(5 * time.Second):
			return nil, errors.New("data channel not ready")
		}
		if dc == nil {
			return nil, errors.New("data channel not ready")
		}
	}
	return newDCConn(dc), nil
}

func (c *dcConn) Read(p []byte) (int, error) {
	for len(c.cur) == 0 {
		b, ok := <-c.msgs
		if !ok {
			return 0, io.EOF
		}
		c.cur = b
	}
	n := copy(p, c.cur)
	c.cur = c.cur[n:]
	return n, nil
}

func (c *dcConn) Write(p []byte) (int, error) {
	// Send in modest chunks to avoid large SCTP messages.
	const max = 32 * 1024
	written := 0
	for len(p) > 0 {
		n := len(p)
		if n > max {
			n = max
		}
		// Backpressure: wait until buffered amount drops below high water
		for {
			c.mu.Lock()
			closed := c.closed
			c.mu.Unlock()
			if closed {
				return written, io.EOF
			}
			if c.dc.BufferedAmount() <= c.highWater {
				break
			}
			// Wait for low signal or timeout to re-check
			select {
			case <-c.lowCh:
			case <-time.After(50 * time.Millisecond):
			}
		}
		if err := c.dc.Send(p[:n]); err != nil {
			return written, err
		}
		written += n
		p = p[n:]
	}
	return written, nil
}

func (c *dcConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.msgs)
	c.mu.Unlock()
	return c.dc.Close()
}

// Minimal net.Conn plumbing
type webrtcAddr struct{}

func (webrtcAddr) Network() string { return "webrtc" }
func (webrtcAddr) String() string  { return "webrtc-datachannel" }

func (c *dcConn) LocalAddr() net.Addr                { return webrtcAddr{} }
func (c *dcConn) RemoteAddr() net.Addr               { return webrtcAddr{} }
func (c *dcConn) SetDeadline(t time.Time) error      { c.rd, c.wd = t, t; return nil }
func (c *dcConn) SetReadDeadline(t time.Time) error  { c.rd = t; return nil }
func (c *dcConn) SetWriteDeadline(t time.Time) error { c.wd = t; return nil }
