package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"learnP2P/connections"
)

func main() {
	// Flags
	webrtcFlag := flag.Bool("webrtc", false, "Use WebRTC interactive mode (deprecated; use --webrtc-send or --webrtc-recv)")
	webrtcSend := flag.Bool("webrtc-send", false, "WebRTC sender: generate OFFER and read ANSWER")
	webrtcRecv := flag.Bool("webrtc-recv", false, "WebRTC receiver: paste OFFER and output ANSWER")
	portFlag := flag.Int("port", 8000, "Port to expose for local discovery")
	nameFlag := flag.String("name", "", "Node name to expose (default: COMPUTERNAME)")
	flag.Parse()

	baseName := os.Getenv("COMPUTERNAME")
	if baseName == "" {
		baseName = "Node"
	}
	name := *nameFlag
	if name == "" {
		name = "P2PNode2-" + strings.ReplaceAll(baseName, " ", "-")
	}
	port := *portFlag

	// If WebRTC mode is requested, do not expose via mDNS
	if *webrtcFlag || *webrtcSend || *webrtcRecv {
		// If explicit role flags provided, use them; otherwise ask interactively
		role := 0
		if *webrtcSend && *webrtcRecv {
			log.Fatal("Cannot specify both --webrtc-send and --webrtc-recv")
		} else if *webrtcSend {
			role = 1
		} else if *webrtcRecv {
			role = 2
		}

		if role == 0 { // interactive fallback
			fmt.Println("WebRTC mode: no mDNS exposure. Choose a role: [1] Sender (create offer)  [2] Receiver (paste offer)")
			fmt.Print("Enter 1 or 2: ")
			line := readLine()
			if v, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
				role = v
			}
		}

		switch role {
		case 1:
			// Sender: generate offer, print base64, then accept pasted answer
			offerB64, peer, err := connections.GenerateOffer()
			if err != nil {
				log.Fatalf("Failed to generate offer: %v", err)
			}
			fmt.Println("\n--- SEND THIS OFFER TO THE RECEIVER ---")
			fmt.Println(offerB64)
			fmt.Println("--- END OFFER ---\n")

			fmt.Print("Paste receiver ANSWER and press Enter:\n> ")
			ansB64 := strings.TrimSpace(readLine())
			if ansB64 == "" {
				log.Fatal("Empty ANSWER provided")
			}
			if err := connections.AcceptAnswer(peer, ansB64); err != nil {
				log.Fatalf("Failed to accept answer: %v", err)
			}

			// Wait for connection
			select {
			case <-peer.Connected():
				log.Println("WebRTC connection established successfully (sender)")
			case <-time.After(15 * time.Second):
				log.Fatal("Timed out waiting for connection")
			}
			return

		case 2:
			// Receiver: paste offer, generate answer, print it
			fmt.Print("Paste sender OFFER and press Enter:\n> ")
			offerB64 := strings.TrimSpace(readLine())
			if offerB64 == "" {
				log.Fatal("Empty OFFER provided")
			}
			ansB64, peer, err := connections.AcceptOfferAndGenerateAnswer(offerB64)
			if err != nil {
				log.Fatalf("Failed to accept offer: %v", err)
			}
			fmt.Println("\n--- SEND THIS ANSWER BACK TO THE SENDER ---")
			fmt.Println(ansB64)
			fmt.Println("--- END ANSWER ---\n")

			// Wait for connection
			select {
			case <-peer.Connected():
				log.Println("WebRTC connection established successfully (receiver)")
			case <-time.After(15 * time.Second):
				log.Fatal("Timed out waiting for connection")
			}
			return

		default:
			log.Fatal("Invalid role; please run again and choose 1 or 2")
		}
		return
	}

	// Start mDNS service (broadcast)
	localIPs, err := connections.GetLocalIPs()
	if err != nil || len(localIPs) == 0 {
		log.Fatalf("Could not get local IPs: %v", err)
	}
	fmt.Printf("Broadcasting as '%s' on port %d with IPs: %v\n", name, port, localIPs)

	server, err := connections.StartMDNS(name, port)
	if err != nil {
		log.Fatalf("Failed to register mDNS: %v", err)
	}
	defer server.Shutdown()

	// Discover other nodes
	fmt.Println("Discovering nodes on the local network...")
	ctx := context.Background()
	nodeCh, err := connections.DiscoverMDNS(ctx)
	if err != nil {
		log.Fatalf("Failed to browse mDNS: %v", err)
	}

	// Print our own node as well
	for _, ip := range localIPs {
		fmt.Printf("(Self)     : %s\t%s\t%d\n", name, ip, port)
	}

	// Optional: create a WebRTC offer (for future P2P signaling).
	// Commented out to keep runtime simple; uncomment to test SDP generation.
	// we, err := connections.NewWebRTC()
	// if err == nil {
	//     if sdp, e := we.CreateOffer(); e == nil {
	//         fmt.Println("Local SDP offer (truncated):", sdp[:min(60, len(sdp))]+"...")
	//     }
	// }

	// Keep running and print discovered nodes as they arrive
	for n := range nodeCh {
		fmt.Printf("Discovered: %s\t%s\t%d\n", n.Name, n.IP, n.Port)
	}
}

func readLine() string {
	r := bufio.NewReader(os.Stdin)
	s, _ := r.ReadString('\n')
	return strings.TrimRight(s, "\r\n")
}
