package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/grandcat/zeroconf"
)

func getLocalIPs() ([]string, error) {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() {
				continue
			}
			ip = ip.To4()
			if ip == nil {
				continue
			}
			ips = append(ips, ip.String())
		}
	}
	return ips, nil
}

func main() {
	name := "P2PNode2-" + strings.ReplaceAll(os.Getenv("COMPUTERNAME"), " ", "-")
	port := 8000
	localIPs, err := getLocalIPs()
	if err != nil || len(localIPs) == 0 {
		log.Fatalf("Could not get local IPs: %v", err)
	}
	fmt.Printf("Broadcasting as '%s' on port %d with IPs: %v\n", name, port, localIPs)

	// Start mDNS service (broadcast)
	server, err := zeroconf.Register(
		name,                          // service instance name
		"_p2pnode._tcp",               // service type
		"local.",                      // service domain
		port,                          // service port
		[]string{"node_name=" + name}, // TXT records
		nil,                           // use default interface
	)
	if err != nil {
		log.Fatalf("Failed to register mDNS: %v", err)
	}
	defer server.Shutdown()

	// Discover other nodes
	foundNodes := make(map[string]struct{})
	type nodeInfo struct {
		Name string
		IP   string
		Port int
	}
	nodes := []nodeInfo{}

	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		log.Fatalf("Failed to initialize resolver: %v", err)
	}
	entries := make(chan *zeroconf.ServiceEntry)
	go func(results <-chan *zeroconf.ServiceEntry) {
		for entry := range results {
			if entry == nil || len(entry.AddrIPv4) == 0 {
				continue
			}
			key := entry.Instance + entry.AddrIPv4[0].String() + fmt.Sprint(entry.Port)
			if _, exists := foundNodes[key]; !exists {
				foundNodes[key] = struct{}{}
				nodes = append(nodes, nodeInfo{
					Name: entry.Instance,
					IP:   entry.AddrIPv4[0].String(),
					Port: entry.Port,
				})
				fmt.Printf("Discovered: %s\t%s\t%d\n", entry.Instance, entry.AddrIPv4[0].String(), entry.Port)
			}
		}
	}(entries)

	fmt.Println("Discovering nodes on the local network...")
	err = resolver.Browse(context.Background(), "_p2pnode._tcp", "local.", entries)
	if err != nil {
		log.Fatalf("Failed to browse mDNS: %v", err)
	}

	// Print our own node as well
	for _, ip := range localIPs {
		fmt.Printf("(Self)     : %s\t%s\t%d\n", name, ip, port)
	}

	// Keep running
	select {}
}
