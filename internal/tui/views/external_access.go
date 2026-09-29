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

// ExternalAccessView is Control Panel → External Access in TUI form:
// a QuickConnect card (Synology's reverse-tunnel relay) plus a
// router-port-forwarding card driven off UPnP. Empty-state when
// neither surface is configured.

type quickConnectMsg struct {
	S   *dsm.QuickConnectStatus
	Err error
}
type portForwardingMsg struct {
	F   *dsm.RouterPortForwarding
	Err error
}

type ExternalAccessView struct {
	ctx Ctx

	qc    *dsm.QuickConnectStatus
	qcErr error

	pf    *dsm.RouterPortForwarding
	pfErr error

	loaded bool
}

// NewExternalAccess constructs the external access view.
func NewExternalAccess(c Ctx) tui.View { return &ExternalAccessView{ctx: c} }

func (v *ExternalAccessView) Name() string                   { return "external-access" }
func (v *ExternalAccessView) Title() string                  { return "External Access" }
func (v *ExternalAccessView) Icon() string                   { return "⇄" }
func (v *ExternalAccessView) RefreshInterval() time.Duration { return 5 * time.Minute }
func (v *ExternalAccessView) Hint() string                   { return "r refresh" }
func (v *ExternalAccessView) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

func (v *ExternalAccessView) Init() tea.Cmd { return tea.Batch(v.fetchQC(), v.fetchPF()) }

func (v *ExternalAccessView) fetchQC() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (*dsm.QuickConnectStatus, error) {
			return c.QuickConnectStatus(ctx)
		},
		func(s *dsm.QuickConnectStatus, err error) tea.Msg { return quickConnectMsg{S: s, Err: err} },
	)
}

func (v *ExternalAccessView) fetchPF() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (*dsm.RouterPortForwarding, error) { return c.PortForwarding(ctx) },
		func(f *dsm.RouterPortForwarding, err error) tea.Msg { return portForwardingMsg{F: f, Err: err} },
	)
}

func (v *ExternalAccessView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchQC(), v.fetchPF())
	case quickConnectMsg:
		v.qc, v.qcErr = m.S, m.Err
		v.loaded = true
	case portForwardingMsg:
		v.pf, v.pfErr = m.F, m.Err
		v.loaded = true
	case tea.KeyMsg:
		if m.String() == "r" {
			return v, tea.Batch(v.fetchQC(), v.fetchPF())
		}
	}
	return v, nil
}

func (v *ExternalAccessView) Render(width, height int) string {
	t := v.ctx.Theme
	if !v.loaded {
		return fitOrScroll(Card(t, width, " ⇄  External Access ", "\n  loading external-access settings…\n", true), height)
	}

	qcConfigured := v.qc != nil && (v.qc.Enabled || v.qc.QuickConnectID != "")
	pfConfigured := v.pf != nil && (v.pf.Enabled || len(v.pf.Mappings) > 0)

	if !qcConfigured && !pfConfigured {
		return fitOrScroll(emptyStateCard(t, width,
			"⇄  External Access",
			"Neither QuickConnect nor router port-forwarding is configured.",
			"Open DSM → Control Panel → External Access to set one of them up."), height)
	}

	parts := []string{v.renderQuickConnect(width)}
	parts = append(parts, v.renderPortForwarding(width))
	if v.qcErr != nil {
		parts = append(parts, errLine(t, v.qcErr))
	}
	if v.pfErr != nil {
		parts = append(parts, errLine(t, v.pfErr))
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  r refresh"))
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *ExternalAccessView) renderQuickConnect(width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)

	header := t.Title().Render(" QuickConnect ")
	if v.qc == nil {
		body := header + "\n" + mu.Render("  Not reported by this DSM.")
		return t.Card(false).Width(width - 2).Render(body)
	}
	state := t.Chip(t.Muted).Render(" off ")
	if v.qc.Enabled {
		state = t.Chip(t.Accent2).Render(" on ")
	}
	id := v.qc.QuickConnectID
	if id == "" {
		id = "—"
	}
	relay := t.Chip(t.Muted).Render(" relay off ")
	if v.qc.RelayEnabled {
		relay = t.Chip(t.Accent2).Render(" relay on ")
	}
	router := t.Chip(t.Muted).Render(" router incompat ")
	if v.qc.IsRouterCompat {
		router = t.Chip(t.Accent2).Render(" router compat ")
	}
	body := header + "\n" +
		"  " + state + "   " + mu.Render("ID: ") + text.Render(id) + "\n" +
		"  " + relay + "   " + router
	return t.Card(false).Width(width - 2).Render(body)
}

func (v *ExternalAccessView) renderPortForwarding(width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)

	header := t.Title().Render(" Router port-forwarding ")
	if v.pf == nil {
		body := header + "\n" + mu.Render("  Not reported by this DSM.")
		return t.Card(false).Width(width - 2).Render(body)
	}
	state := t.Chip(t.Muted).Render(" disabled ")
	if v.pf.Enabled {
		state = t.Chip(t.Accent2).Render(" enabled ")
	}
	lines := []string{"  " + state}
	if len(v.pf.Mappings) == 0 {
		lines = append(lines, "  "+mu.Render("No UPnP mappings reported."))
	} else {
		for _, m := range v.pf.Mappings {
			proto := strings.ToUpper(m.Protocol)
			if proto == "" {
				proto = "—"
			}
			svc := m.Service
			if svc == "" {
				svc = "—"
			}
			line := "  " +
				padRight(text.Render(svc), 16) + " " +
				padRight(mu.Render(proto), 6) + " " +
				padRight(text.Render(fmt.Sprintf("%d", m.ExternalPort)), 8) +
				mu.Render("→ ") +
				text.Render(fmt.Sprintf("%d", m.InternalPort))
			lines = append(lines, line)
		}
	}
	body := header + "\n" + strings.Join(lines, "\n")
	return t.Card(false).Width(width - 2).Render(body)
}
