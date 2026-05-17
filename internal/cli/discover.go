package cli

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/janbaraniewski/synology-ctl/internal/discover"
)

func newDiscoverCmd() *cobra.Command {
	var (
		timeout time.Duration
		pick    bool
	)
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Scan the local network for Synology NAS devices via mDNS",
		Long: `Browse mDNS for Synology devices on the local network.

Scans _http._tcp, _https._tcp, _smb._tcp and _afpovertcp._tcp for records
matching Synology's vendor metadata or hostname conventions (DiskStation,
RackStation, DS#####, RS#####). Results are de-duplicated by IP.

Use --pick to choose specific network interfaces (Wi-Fi, Ethernet, VPN,
WireGuard, Tailscale, …) — useful when your NAS sits behind a VPN.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout+time.Second)
			defer cancel()

			var ifaces []*net.Interface
			if pick {
				picked, err := pickInterfaces()
				if err != nil {
					return err
				}
				ifaces = picked
			}

			fmt.Printf("Scanning for Synology devices (%.0fs)…\n\n", timeout.Seconds())
			devices, err := discover.ScanInterfaces(ctx, timeout, ifaces)
			if err != nil {
				return err
			}
			if len(devices) == 0 {
				fmt.Println("No devices found. Make sure the NAS is on the same broadcast domain")
				fmt.Println("and that DSM > Control Panel > Network > Bonjour is enabled.")
				return nil
			}

			title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#cba6f7"))
			muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#a6adc8"))

			for _, d := range devices {
				fmt.Println(title.Render(d.Name + " — " + d.Hostname))
				fmt.Println("  " + muted.Render("address: ") + d.PrimaryAddr())
				if d.Vendor != "" {
					fmt.Println("  " + muted.Render("vendor:  ") + d.Vendor)
				}
				if d.Model != "" {
					fmt.Println("  " + muted.Render("model:   ") + d.Model)
				}
				fmt.Println("  " + muted.Render("port:    ") + portStr(d.Port, d.Secure))
				if len(d.IPv4) > 1 {
					ips := make([]string, 0, len(d.IPv4))
					for _, ip := range d.IPv4 {
						ips = append(ips, ip.String())
					}
					fmt.Println("  " + muted.Render("ipv4:    ") + strings.Join(ips, ", "))
				}
				fmt.Println()
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Second, "scan duration")
	cmd.Flags().BoolVar(&pick, "pick", false, "interactively pick which network interfaces to scan")
	return cmd
}

func portStr(p int, secure bool) string {
	scheme := "http"
	if secure {
		scheme = "https"
	}
	return fmt.Sprintf("%d (%s)", p, scheme)
}
