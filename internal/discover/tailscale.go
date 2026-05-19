package discover

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// tailscaleStatus mirrors the fields we care about from `tailscale
// status --json`. Everything else is ignored on purpose.
type tailscaleStatus struct {
	BackendState string                   `json:"BackendState"`
	Self         *tailscalePeer           `json:"Self"`
	Peer         map[string]tailscalePeer `json:"Peer"`
}

type tailscalePeer struct {
	HostName     string   `json:"HostName"`
	DNSName      string   `json:"DNSName"`
	OS           string   `json:"OS"`
	TailscaleIPs []string `json:"TailscaleIPs"`
	Online       bool     `json:"Online"`
}

// HasTailscale reports whether a usable tailscale CLI is available and
// the daemon is in Running state.
func HasTailscale(ctx context.Context) bool {
	if tailscaleBinary() == "" {
		return false
	}
	status, err := readTailscaleStatus(ctx)
	if err != nil {
		return false
	}
	return status.BackendState == "Running"
}

// ScanTailnet enumerates Tailscale peers and probes each on DSM ports
// 5001 (https) and 5000 (http). A peer that answers with a DSM-shaped
// JSON envelope (`{"success":true,...}` from SYNO.API.Info) is
// classified as a Synology device and returned.
//
// mDNS doesn't cross Tailscale's tunnels, so without this step
// tailnet-only NASes are invisible to synoctl discover.
func ScanTailnet(ctx context.Context, timeout time.Duration) ([]Device, error) {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	status, err := readTailscaleStatus(probeCtx)
	if err != nil {
		return nil, err
	}
	if status == nil || len(status.Peer) == 0 {
		return nil, nil
	}

	// Probe each peer in parallel; small worker pool to avoid hammering.
	type result struct {
		d  Device
		ok bool
	}
	out := make(chan result, len(status.Peer))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for _, p := range status.Peer {
		if !p.Online || len(p.TailscaleIPs) == 0 {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(peer tailscalePeer) {
			defer wg.Done()
			defer func() { <-sem }()
			if d, ok := probePeer(probeCtx, peer); ok {
				out <- result{d, true}
			}
		}(p)
	}
	go func() { wg.Wait(); close(out) }()

	var devices []Device
	for r := range out {
		devices = append(devices, r.d)
	}
	return devices, nil
}

// tailscaleBinary returns the first usable tailscale CLI path. macOS's
// /usr/local/bin/tailscale shim crashes with trace/BPT when exec'd from
// a non-hardened binary; the real binary inside Tailscale.app works.
// Try the most likely paths in order.
func tailscaleBinary() string {
	candidates := []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/usr/local/bin/tailscale",
		"/opt/homebrew/bin/tailscale",
		"/usr/bin/tailscale",
		"tailscale",
	}
	for _, c := range candidates {
		if p, err := exec.LookPath(c); err == nil {
			return p
		}
	}
	return ""
}

// readTailscaleStatus shells out to the CLI. We use the CLI rather than
// the local API socket because the socket path differs between macOS /
// Linux installs and the CLI handles that for us.
func readTailscaleStatus(ctx context.Context) (*tailscaleStatus, error) {
	bin := tailscaleBinary()
	if bin == "" {
		return nil, fmt.Errorf("tailscale CLI not found")
	}
	cmd := exec.CommandContext(ctx, bin, "status", "--json", "--self=false")
	cmd.Stderr = nil
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}
	var st tailscaleStatus
	if err := json.Unmarshal(out, &st); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}
	return &st, nil
}

// probePeer tries 5001/https then 5000/http. Returns a Device when the
// peer answers with a DSM-shaped envelope.
func probePeer(ctx context.Context, p tailscalePeer) (Device, bool) {
	ip := preferIPv4(p.TailscaleIPs)
	if ip == "" {
		return Device{}, false
	}
	// Try verified HTTPS first, then DSM's default HTTP port. We do not
	// disable certificate verification during discovery; users can still
	// opt into insecure TLS for the actual configured DSM profile.
	candidates := []struct {
		scheme string
		port   int
	}{{"https", 5001}, {"http", 5000}}

	for _, c := range candidates {
		if !isDSMAt(ctx, c.scheme, ip, c.port) {
			continue
		}
		host := strings.TrimSuffix(p.DNSName, ".")
		if host == "" {
			host = p.HostName
		}
		dev := Device{
			Hostname: host,
			Name:     p.HostName,
			Vendor:   "Synology", // confirmed by the DSM probe
			Model:    "",
			Port:     c.port,
			Secure:   c.scheme == "https",
			IPv4:     []net.IP{net.ParseIP(ip)},
		}
		return dev, true
	}
	return Device{}, false
}

// dsmProbeClient is a small short-timeout client used only for tailnet
// probing. HTTPS probes use the platform trust store; self-signed DSM
// certificates fail closed here and the HTTP fallback can still discover
// default DSM installs.
var dsmProbeClient = &http.Client{
	Timeout: 3 * time.Second,
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout: 2 * time.Second,
		}).DialContext,
	},
}

// isDSMAt issues a SYNO.API.Info query — DSM answers with a known JSON
// envelope; anything else (404, plain HTML, refused) is rejected.
func isDSMAt(ctx context.Context, scheme, ip string, port int) bool {
	url := fmt.Sprintf("%s://%s/webapi/query.cgi?api=SYNO.API.Info&version=1&method=query&query=SYNO.API.Auth",
		scheme, net.JoinHostPort(ip, fmt.Sprintf("%d", port)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := dsmProbeClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return false
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "json") {
		return false
	}
	var env struct {
		Success bool                       `json:"success"`
		Data    map[string]json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return false
	}
	if !env.Success {
		return false
	}
	_, ok := env.Data["SYNO.API.Auth"]
	return ok
}

func preferIPv4(ips []string) string {
	for _, ip := range ips {
		if p := net.ParseIP(ip); p != nil && p.To4() != nil {
			return ip
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return ""
}
