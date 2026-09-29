package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Network is the interfaces view. One row per NIC with type, IPv4/mask,
// gateway, link speed, status.
type Network struct {
	ctx Ctx

	ifs []dsm.NetworkInterface
	err error

	base   listBase
	detail *dsm.NetworkInterface
}

// NewNetwork constructs the network view.
func NewNetwork(c Ctx) tui.View { return &Network{ctx: c} }

func (n *Network) Name() string                   { return "network" }
func (n *Network) Title() string                  { return "Network" }
func (n *Network) Icon() string                   { return "⇄" }
func (n *Network) RefreshInterval() time.Duration { return 30 * time.Second }
func (n *Network) Bindings() []key.Binding        { return BaseBindings() }

// IsTextEditing defers global keys while the inline filter owns input.
func (n *Network) IsTextEditing() bool { return n.base.filter.IsActive() }

func (n *Network) Init() tea.Cmd { return n.fetch() }

func (n *Network) fetch() tea.Cmd {
	c := n.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.NetworkInterface, error) { return c.NetworkInterfaces(ctx) },
		func(v []dsm.NetworkInterface, err error) tea.Msg { return netMsg{I: v, Err: err} },
	)
}

func (n *Network) visible() []dsm.NetworkInterface {
	if n.base.FilterValue() == "" {
		return n.ifs
	}
	out := make([]dsm.NetworkInterface, 0, len(n.ifs))
	for _, x := range n.ifs {
		if MatchesAll(n.base.FilterValue(), x.IFName, x.Type, x.IP, x.Gateway, x.MAC, x.Status) {
			out = append(out, x)
		}
	}
	return out
}

func (n *Network) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if n.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			n.detail = nil
		}
		return n, nil
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return n, n.fetch()
	case netMsg:
		n.ifs, n.err = m.I, m.Err
		n.base.ClampCursor(len(n.visible()))
		return n, nil
	}
	if _, handled := n.base.HandleKey(msg, len(n.visible())); handled {
		return n, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyEnter {
		rows := n.visible()
		if n.base.Cursor() < len(rows) {
			iface := rows[n.base.Cursor()]
			n.detail = &iface
		}
	}
	return n, nil
}

func (n *Network) Render(width, height int) string {
	t := n.ctx.Theme
	if n.detail != nil {
		return renderNetworkDetail(t, width, *n.detail)
	}
	rows := n.visible()
	parts := []string{sectionHeader(t, width, "Network interfaces", len(rows), n.err)}
	if n.ifs == nil && n.err == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for i, x := range rows {
		parts = append(parts, n.renderRow(x, i == n.base.Cursor()))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter"))
	if f := n.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (n *Network) Inspect(width, height int) string {
	rows := n.visible()
	if len(rows) == 0 || n.base.Cursor() >= len(rows) {
		return ""
	}
	t := n.ctx.Theme
	iface := rows[n.base.Cursor()]
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	ip := iface.IP
	if ip != "" && iface.Mask != "" {
		ip += "/" + iface.Mask
	}
	speed := "—"
	if iface.Speed > 0 {
		speed = fmt.Sprintf("%d Mbit/s", iface.Speed)
	}
	parts := []string{
		t.Title().Render(" " + iface.IFName + " "),
		"",
		muted.Render(iface.Type),
		"",
		muted.Render("IP:      ") + text.Render(coalesce(ip, "—")),
		muted.Render("Gateway: ") + text.Render(coalesce(iface.Gateway, "—")),
		muted.Render("MAC:     ") + text.Render(coalesce(iface.MAC, "—")),
		muted.Render("Speed:   ") + text.Render(speed),
		muted.Render("Status:  ") + t.HealthStyle(iface.Status).Render(iface.Status),
	}
	if iface.UseDHCP {
		parts = append(parts, "", t.HealthStyle("enabled").Render(" DHCP "))
	}
	_ = width
	_ = height
	return strings.Join(parts, "\n")
}

func (n *Network) renderRow(iface dsm.NetworkInterface, highlight bool) string {
	t := n.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	ip := iface.IP
	if ip != "" && iface.Mask != "" {
		ip += "/" + iface.Mask
	}
	speed := "—"
	if iface.Speed > 0 {
		speed = fmt.Sprintf("%d Mbit/s", iface.Speed)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(iface.IFName), 12), " ",
		padRight(muted.Render(iface.Type), 10), " ",
		padRight(text.Render(ip), 22), " ",
		padRight(muted.Render(iface.Gateway), 18), " ",
		padLeft(muted.Render(speed), 14), " ",
		t.HealthStyle(iface.Status).Render(iface.Status),
	)
}
