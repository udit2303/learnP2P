package connections

import (
	"context"
	"fmt"
	"net"

	"github.com/grandcat/zeroconf"
)

// Node represents a discovered peer/node on the local network.
type Node struct {
	Name string
	IP   string
	Port int
}

// GetLocalIPs returns all non-loopback IPv4 addresses on up interfaces.
func GetLocalIPs() ([]string, error) {
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

// StartMDNS registers this node on mDNS. Call Shutdown on the returned server when done.
func StartMDNS(name string, port int) (*zeroconf.Server, error) {
	// TXT records can carry lightweight metadata.
	txt := []string{"node_name=" + name}
	return zeroconf.Register(name, "_p2pnode._tcp", "local.", port, txt, nil)
}

// DiscoverMDNS browses for nodes and streams them on the returned channel until ctx is done.
func DiscoverMDNS(ctx context.Context) (<-chan Node, error) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return nil, err
	}
	entries := make(chan *zeroconf.ServiceEntry)
	out := make(chan Node)

	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-entries:
				if !ok || e == nil {
					return
				}
				if len(e.AddrIPv4) == 0 {
					continue
				}
				out <- Node{
					Name: e.Instance,
					IP:   e.AddrIPv4[0].String(),
					Port: e.Port,
				}
			}
		}
	}()

	go func() {
		// Close entries when ctx is done to stop the goroutine above.
		<-ctx.Done()
		close(entries)
	}()

	if err := resolver.Browse(ctx, "_p2pnode._tcp", "local.", entries); err != nil {
		return nil, fmt.Errorf("mdns browse: %w", err)
	}
	return out, nil
}
