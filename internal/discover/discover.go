// Package discover scans the local network for Synology NAS devices via mDNS.
package discover

import (
	"context"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

// Device represents a Synology NAS discovered on the network.
type Device struct {
	Hostname string   // e.g. "deep-thought.local"
	Name     string   // mDNS instance name
	Model    string   // parsed from TXT records when available
	Vendor   string   // typically "Synology"
	IPv4     []net.IP // v4 addresses
	IPv6     []net.IP // v6 addresses
	Port     int      // DSM web port (5000/5001 usually)
	Secure   bool     // true when discovered on https
}

// PrimaryAddr returns the address to connect to: routable IPv4 first,
// then the mDNS hostname, then routable IPv6, then anything else.
func (d Device) PrimaryAddr() string {
	for _, ip := range d.IPv4 {
		if !ip.IsLinkLocalUnicast() {
			return ip.String()
		}
	}
	if d.Hostname != "" {
		return strings.TrimSuffix(d.Hostname, ".")
	}
	for _, ip := range d.IPv6 {
		if !ip.IsLinkLocalUnicast() {
			return ip.String()
		}
	}
	if len(d.IPv4) > 0 {
		return d.IPv4[0].String()
	}
	if len(d.IPv6) > 0 {
		return d.IPv6[0].String()
	}
	return ""
}

// Services we probe — _http/_https are the primary advertisements,
// _smb/_afp are fallbacks for boxes that filter the web service.
var probeServices = []struct {
	name   string
	secure bool
}{
	{"_https._tcp", true},
	{"_http._tcp", false},
	{"_smb._tcp", false},
	{"_afpovertcp._tcp", false},
}

type keyed struct {
	key string
	dev Device
}

// Scan browses every interface on the host. Equivalent to
// ScanInterfaces(ctx, timeout, nil).
func Scan(ctx context.Context, timeout time.Duration) ([]Device, error) {
	return ScanInterfaces(ctx, timeout, nil)
}

// ScanInterfaces browses for Synology devices, binding zeroconf to the
// interfaces in `ifaces`. Pass nil/empty for the default (all
// interfaces). Useful when a host has multiple networks (VPNs, Wi-Fi,
// LAN) and the user wants to scan only specific ones.
func ScanInterfaces(ctx context.Context, timeout time.Duration, ifaces []*net.Interface) ([]Device, error) {
	scanCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	collect := make(chan keyed, 64)

	var wg sync.WaitGroup
	for _, svc := range probeServices {
		wg.Add(1)
		go func(name string, secure bool) {
			defer wg.Done()
			browseOne(scanCtx, name, secure, ifaces, collect)
		}(svc.name, svc.secure)
	}
	go func() { wg.Wait(); close(collect) }()

	byKey := map[string]*Device{}
	for k := range collect {
		existing, ok := byKey[k.key]
		if !ok {
			d := k.dev
			byKey[k.key] = &d
			continue
		}
		mergeDevice(existing, k.dev)
	}

	out := make([]Device, 0, len(byKey))
	for _, d := range byKey {
		if isSynology(*d) {
			out = append(out, *d)
		}
	}
	return out, nil
}

func browseOne(ctx context.Context, service string, secure bool, ifaces []*net.Interface, sink chan<- keyed) {
	var opts []zeroconf.ClientOption
	if len(ifaces) > 0 {
		// zeroconf takes a flat []net.Interface (not pointers).
		flat := make([]net.Interface, 0, len(ifaces))
		for _, p := range ifaces {
			if p != nil {
				flat = append(flat, *p)
			}
		}
		opts = append(opts, zeroconf.SelectIfaces(flat))
	}
	resolver, err := zeroconf.NewResolver(opts...)
	if err != nil {
		return
	}
	entries := make(chan *zeroconf.ServiceEntry, 32)
	go func() {
		_ = resolver.Browse(ctx, service, "local.", entries)
	}()
	for e := range entries {
		if e == nil {
			continue
		}
		dev := Device{
			Hostname: strings.TrimSuffix(e.HostName, "."),
			Name:     e.Instance,
			IPv4:     e.AddrIPv4,
			IPv6:     e.AddrIPv6,
			Port:     e.Port,
			Secure:   secure,
		}
		dev.Vendor, dev.Model = parseTXT(e.Text)

		// For non-web services we don't have the DSM port, so default
		// to the usual ones; the merge step replaces these if a web
		// record arrives.
		if service != "_http._tcp" && service != "_https._tcp" {
			if secure {
				dev.Port = 5001
			} else {
				dev.Port = 5000
			}
		}

		key := primaryKey(dev)
		if key == "" {
			continue
		}
		sink <- keyed{key: key, dev: dev}
	}
}

func primaryKey(d Device) string {
	if len(d.IPv4) > 0 {
		return d.IPv4[0].String()
	}
	if d.Hostname != "" {
		return strings.ToLower(d.Hostname)
	}
	if len(d.IPv6) > 0 {
		return d.IPv6[0].String()
	}
	return ""
}

func mergeDevice(dst *Device, src Device) {
	if dst.Vendor == "" {
		dst.Vendor = src.Vendor
	}
	if dst.Model == "" {
		dst.Model = src.Model
	}
	if dst.Name == "" {
		dst.Name = src.Name
	}
	// Prefer the secure web record for the port hint.
	if src.Secure && (src.Port == 5001 || src.Port == 443) {
		dst.Port = src.Port
		dst.Secure = true
	} else if !dst.Secure && src.Port == 5000 {
		dst.Port = src.Port
	}
	dst.IPv4 = mergeIPs(dst.IPv4, src.IPv4)
	dst.IPv6 = mergeIPs(dst.IPv6, src.IPv6)
}

func mergeIPs(a, b []net.IP) []net.IP {
	seen := map[string]struct{}{}
	out := []net.IP{}
	for _, ip := range append(a, b...) {
		k := ip.String()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, ip)
	}
	return out
}

// parseTXT pulls vendor + model hints out of an mDNS TXT record. Synology
// puts `vendor=Synology` and `model=...` on most service advertisements,
// but the keys vary by service type, so we accept several aliases.
func parseTXT(txt []string) (vendor, model string) {
	for _, kv := range txt {
		key, val, ok := splitKV(kv)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "vendor", "manufacturer":
			vendor = val
		case "model", "modelname", "md":
			model = val
		}
	}
	return vendor, model
}

func splitKV(s string) (k, v string, ok bool) {
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// isSynology decides whether a discovered record looks like a Synology unit.
// We match on the vendor TXT first, then fall back to hostname heuristics
// since some service types do not include vendor metadata.
func isSynology(d Device) bool {
	if strings.EqualFold(d.Vendor, "Synology") {
		return true
	}
	host := strings.ToLower(d.Hostname)
	switch {
	case strings.Contains(host, "synology"):
		return true
	case strings.Contains(host, "diskstation"):
		return true
	case strings.Contains(host, "rackstation"):
		return true
	case strings.HasPrefix(host, "ds") && hasDigits(host):
		return true
	case strings.HasPrefix(host, "rs") && hasDigits(host):
		return true
	}
	return false
}

func hasDigits(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
