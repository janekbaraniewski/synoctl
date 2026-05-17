package cli

import (
	"fmt"
	"net"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/discover"
)

// pickInterfaces prompts the user to pick which network interfaces to
// scan. Returns nil (= "all interfaces") when:
//   - there's only one usable interface
//   - the user picks "all" in the form
//
// This is the entry point for the multi-network discovery UX.
func pickInterfaces() ([]*net.Interface, error) {
	ifs, err := discover.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("enumerate interfaces: %w", err)
	}
	if len(ifs) <= 1 {
		return nil, nil
	}

	// Render interface options. The first option is "all" — chosen by
	// default for users who don't care about the distinction.
	const allValue = "__all__"
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color("#a6adc8")).Render

	opts := []huh.Option[string]{
		huh.NewOption("All interfaces  "+muted("(default — broadest reach)"), allValue).Selected(true),
	}
	for _, i := range ifs {
		addr := primaryAddrString(i)
		label := fmt.Sprintf("%-10s  %-22s  %s", i.Name, addr, i.Type)
		if i.IsVPN {
			label = lipgloss.NewStyle().Foreground(lipgloss.Color("#89dceb")).Render(label)
		}
		opts = append(opts, huh.NewOption(label, i.Name))
	}

	var picked []string
	form := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[string]().
			Title("Network interfaces").
			Description("Pick which interfaces to scan for Synology devices. mDNS doesn't always cross VPNs — selecting a VPN here will probe the remote side.").
			Options(opts...).
			Value(&picked).
			Validate(func(v []string) error {
				if len(v) == 0 {
					return fmt.Errorf("pick at least one interface (or All)")
				}
				return nil
			}),
	)).WithTheme(theme())
	if err := form.Run(); err != nil {
		return nil, err
	}

	// "All" wins regardless of what else is selected — that's how DSM
	// browsing already works on its own.
	for _, v := range picked {
		if v == allValue {
			return nil, nil
		}
	}

	var out []*net.Interface
	for _, i := range ifs {
		for _, name := range picked {
			if name == i.Name {
				out = append(out, i.Iface)
			}
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// primaryAddrString renders a compact address summary for the picker
// row. IPv4 first, then a single IPv6 if no v4 was bound.
func primaryAddrString(i discover.Interface) string {
	var parts []string
	for _, ip := range i.IPv4 {
		parts = append(parts, ip.String())
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	for _, ip := range i.IPv6 {
		if !ip.IsLinkLocalUnicast() {
			parts = append(parts, ip.String())
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, ", ")
	}
	return "(no address)"
}
