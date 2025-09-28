package connections

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/pion/webrtc/v4"
)

// WebRTC wraps Pion constructs needed to create an offer/answer and a data channel.
type WebRTC struct {
	API      *webrtc.API
	PeerConn *webrtc.PeerConnection
}

// Peer is a lightweight handle for a WebRTC peer and a connection signal.
type Peer struct {
	pc        *webrtc.PeerConnection
	connected chan struct{}
}

// NewWebRTC creates a minimal WebRTC peer connection with a single ordered, reliable data channel.
func NewWebRTC() (*WebRTC, error) {
	m := webrtc.MediaEngine{}
	// No media for now; data-channel only.
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("register codecs: %w", err)
	}

	s := webrtc.SettingEngine{}
	api := webrtc.NewAPI(webrtc.WithMediaEngine(&m), webrtc.WithSettingEngine(s))

	cfg := webrtc.Configuration{
		ICETransportPolicy: webrtc.ICETransportPolicyAll,
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	pc, err := api.NewPeerConnection(cfg)
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}

	return &WebRTC{API: api, PeerConn: pc}, nil
}

// CreateOffer returns a local SDP offer string.
func (w *WebRTC) CreateOffer() (string, error) {
	offer, err := w.PeerConn.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("create offer: %w", err)
	}
	if err = w.PeerConn.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("set local: %w", err)
	}
	return offer.SDP, nil
}

// SetRemoteAnswer sets the remote SDP answer.
func (w *WebRTC) SetRemoteAnswer(sdp string) error {
	answer := webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}
	if err := w.PeerConn.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("set remote: %w", err)
	}
	return nil
}

// Close closes the underlying PeerConnection.
func (w *WebRTC) Close() error { return w.PeerConn.Close() }

// GenerateOffer creates an offerer peer, returns base64-encoded SDP offer and a peer handle.
func GenerateOffer() (string, *Peer, error) {
	w, err := NewWebRTC()
	if err != nil {
		return "", nil, err
	}

	// Create data channel on offerer side so negotiation includes it
	dc, err := w.PeerConn.CreateDataChannel("p2p", nil)
	if err != nil {
		w.Close()
		return "", nil, fmt.Errorf("create data channel: %w", err)
	}

	connected := make(chan struct{})
	dc.OnOpen(func() {
		select {
		case <-connected:
		default:
			close(connected)
		}
	})
	w.PeerConn.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state == webrtc.ICEConnectionStateConnected || state == webrtc.ICEConnectionStateCompleted {
			select {
			case <-connected:
			default:
				close(connected)
			}
		}
	})

	offer, err := w.PeerConn.CreateOffer(nil)
	if err != nil {
		w.Close()
		return "", nil, fmt.Errorf("create offer: %w", err)
	}
	if err = w.PeerConn.SetLocalDescription(offer); err != nil {
		w.Close()
		return "", nil, fmt.Errorf("set local: %w", err)
	}
	<-webrtc.GatheringCompletePromise(w.PeerConn)

	enc, err := encodeSDP(*w.PeerConn.LocalDescription())
	if err != nil {
		w.Close()
		return "", nil, err
	}
	return enc, &Peer{pc: w.PeerConn, connected: connected}, nil
}

// AcceptAnswer applies a base64-encoded SDP answer to the given offerer peer.
func AcceptAnswer(p *Peer, b64Ans string) error {
	var sd webrtc.SessionDescription
	if err := decodeSDP(b64Ans, &sd); err != nil {
		return err
	}
	return p.pc.SetRemoteDescription(sd)
}

// AcceptOfferAndGenerateAnswer creates an answerer peer, applies the remote offer and returns a base64 answer.
func AcceptOfferAndGenerateAnswer(b64Offer string) (string, *Peer, error) {
	w, err := NewWebRTC()
	if err != nil {
		return "", nil, err
	}

	connected := make(chan struct{})
	w.PeerConn.OnDataChannel(func(dc *webrtc.DataChannel) {
		dc.OnOpen(func() {
			select {
			case <-connected:
			default:
				close(connected)
			}
		})
	})
	w.PeerConn.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state == webrtc.ICEConnectionStateConnected || state == webrtc.ICEConnectionStateCompleted {
			select {
			case <-connected:
			default:
				close(connected)
			}
		}
	})

	var remote webrtc.SessionDescription
	if err := decodeSDP(b64Offer, &remote); err != nil {
		w.Close()
		return "", nil, err
	}
	if err := w.PeerConn.SetRemoteDescription(remote); err != nil {
		w.Close()
		return "", nil, fmt.Errorf("set remote: %w", err)
	}
	ans, err := w.PeerConn.CreateAnswer(nil)
	if err != nil {
		w.Close()
		return "", nil, fmt.Errorf("create answer: %w", err)
	}
	if err := w.PeerConn.SetLocalDescription(ans); err != nil {
		w.Close()
		return "", nil, fmt.Errorf("set local: %w", err)
	}
	<-webrtc.GatheringCompletePromise(w.PeerConn)

	enc, err := encodeSDP(*w.PeerConn.LocalDescription())
	if err != nil {
		w.Close()
		return "", nil, err
	}
	return enc, &Peer{pc: w.PeerConn, connected: connected}, nil
}

// Connected returns a channel that closes when the peer is connected.
func (p *Peer) Connected() <-chan struct{} { return p.connected }

func encodeSDP(sd webrtc.SessionDescription) (string, error) {
	b, err := json.Marshal(sd)
	if err != nil {
		return "", fmt.Errorf("marshal sdp: %w", err)
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func decodeSDP(b64 string, out *webrtc.SessionDescription) error {
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("base64 decode: %w", err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("json unmarshal: %w", err)
	}
	return nil
}
