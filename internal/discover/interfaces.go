package discover

import (
	"net"
	"sort"
	"strings"
)

// Interface is a usable local network interface with a friendly label
// and the addresses currently bound to it.
type Interface struct {
	Name   string         // OS name: en0, eth0, utun4 …
	Type   string         // friendly: "Wi-Fi", "Ethernet", "WireGuard", "VPN (utun)", "Loopback"
	IPv4   []net.IP       // bound IPv4 addresses (only non-link-local routable ones)
	IPv6   []net.IP       // bound IPv6 addresses
	Iface  *net.Interface // raw stdlib handle for zeroconf binding
	IsVPN  bool
	IsLoop bool
}

// Label returns a one-line summary suited to a picker row.
func (i Interface) Label() string {
	var addrs []string
	for _, ip := range i.IPv4 {
		addrs = append(addrs, ip.String())
	}
	if len(addrs) == 0 {
		for _, ip := range i.IPv6 {
			addrs = append(addrs, ip.String())
		}
	}
	addr := "(no address)"
	if len(addrs) > 0 {
		addr = strings.Join(addrs, ", ")
	}
	return i.Name + "   " + addr + "   " + i.Type
}

// Interfaces returns every up, non-loopback interface that has at least
// one routable IPv4 or IPv6 address. The list is stable-sorted: physical
// first, VPN next, alphabetical within each bucket.
func Interfaces() ([]Interface, error) {
	raw, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []Interface
	for i := range raw {
		ni := &raw[i]
		if ni.Flags&net.FlagUp == 0 {
			continue
		}
		if ni.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ni.Addrs()
		if err != nil {
			continue
		}
		var v4, v6 []net.IP
		for _, a := range addrs {
			ip := netIP(a)
			if ip == nil {
				continue
			}
			// Skip link-local entirely (IPv4 169.254/16, IPv6 fe80::/10).
			// Interfaces whose *only* addresses are link-local — AWDL,
			// idle utun handles, llw0 — show up as up but aren't useful
			// for routed discovery.
			if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			if ip.To4() != nil {
				v4 = append(v4, ip)
			} else {
				v6 = append(v6, ip)
			}
		}
		if len(v4) == 0 && len(v6) == 0 {
			continue
		}
		entry := Interface{
			Name:  ni.Name,
			IPv4:  v4,
			IPv6:  v6,
			Iface: ni,
		}
		entry.Type, entry.IsVPN = classify(ni.Name, v4)
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsVPN != out[j].IsVPN {
			return !out[i].IsVPN // non-VPN first
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// classify guesses the friendly type from the interface name. The
// heuristics are tuned for macOS and Linux.
func classify(name string, v4 []net.IP) (string, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "wg"):
		return "WireGuard", true
	case strings.HasPrefix(lower, "tun"):
		return "VPN (tun)", true
	case strings.HasPrefix(lower, "tap"):
		return "VPN (tap)", true
	case strings.HasPrefix(lower, "utun"):
		return "VPN (utun)", true
	case strings.HasPrefix(lower, "ipsec"):
		return "IPSec", true
	case strings.HasPrefix(lower, "ppp"):
		return "PPP", true
	case strings.HasPrefix(lower, "tailscale"):
		return "Tailscale", true
	case strings.HasPrefix(lower, "ts"):
		return "Tailscale", true
	case strings.HasPrefix(lower, "zt"):
		return "ZeroTier", true
	case strings.HasPrefix(lower, "en"), strings.HasPrefix(lower, "eth"):
		// macOS: en0 is usually Wi-Fi, en1+ is Thunderbolt/Ethernet. We
		// can't reliably distinguish without SCDynamicStore — fall back
		// to the address class to give a slightly better label.
		if hasPrivate10(v4) {
			return "Ethernet/Wi-Fi", false
		}
		return "Ethernet/Wi-Fi", false
	case strings.HasPrefix(lower, "wlan"), strings.HasPrefix(lower, "wifi"):
		return "Wi-Fi", false
	case strings.HasPrefix(lower, "bridge"):
		return "Bridge", false
	case strings.HasPrefix(lower, "docker"), strings.HasPrefix(lower, "br-"):
		return "Container bridge", false
	case strings.HasPrefix(lower, "veth"):
		return "Container veth", false
	}
	return name, false
}

func hasPrivate10(ips []net.IP) bool {
	for _, ip := range ips {
		if ip.To4() != nil && ip.To4()[0] == 10 {
			return true
		}
	}
	return false
}

// netIP extracts the IP from a net.Addr (IPNet or IPAddr).
func netIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	}
	return nil
}
