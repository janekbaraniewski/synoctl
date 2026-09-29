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

// PowerView surfaces Control Panel → Hardware & Power: a WoL card with
// the configured target MAC, and a list of recurring power-on /
// power-off / restart entries. Read-only — scheduling/Wol mutation
// belongs in a dedicated write-pass with explicit confirmation.

type powerScheduleMsg struct {
	S   []dsm.PowerSchedule
	Err error
}
type wolConfMsg struct {
	W   *dsm.WakeOnLANConf
	Err error
}

type PowerView struct {
	ctx Ctx

	schedule    []dsm.PowerSchedule
	scheduleErr error

	wol    *dsm.WakeOnLANConf
	wolErr error

	loaded bool
}

// NewPower constructs the power & schedule view.
func NewPower(c Ctx) tui.View { return &PowerView{ctx: c} }

func (v *PowerView) Name() string                   { return "power" }
func (v *PowerView) Title() string                  { return "Power & Schedule" }
func (v *PowerView) Icon() string                   { return "⏻" }
func (v *PowerView) RefreshInterval() time.Duration { return 5 * time.Minute }
func (v *PowerView) Hint() string                   { return "r refresh" }
func (v *PowerView) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

func (v *PowerView) Init() tea.Cmd { return tea.Batch(v.fetchSchedule(), v.fetchWOL()) }

func (v *PowerView) fetchSchedule() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.PowerSchedule, error) { return c.PowerSchedule(ctx) },
		func(s []dsm.PowerSchedule, err error) tea.Msg { return powerScheduleMsg{S: s, Err: err} },
	)
}

func (v *PowerView) fetchWOL() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (*dsm.WakeOnLANConf, error) { return c.WakeOnLANConf(ctx) },
		func(w *dsm.WakeOnLANConf, err error) tea.Msg { return wolConfMsg{W: w, Err: err} },
	)
}

func (v *PowerView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchSchedule(), v.fetchWOL())
	case powerScheduleMsg:
		v.schedule, v.scheduleErr = m.S, m.Err
		v.loaded = true
	case wolConfMsg:
		v.wol, v.wolErr = m.W, m.Err
		v.loaded = true
	case tea.KeyMsg:
		if m.String() == "r" {
			return v, tea.Batch(v.fetchSchedule(), v.fetchWOL())
		}
	}
	return v, nil
}

func (v *PowerView) Render(width, height int) string {
	t := v.ctx.Theme
	if !v.loaded {
		return fitOrScroll(Card(t, width, " ⏻  Power & Schedule ", "\n  loading power configuration…\n", true), height)
	}

	parts := []string{v.renderWOL(width)}
	parts = append(parts, v.renderSchedule(width))
	if v.scheduleErr != nil {
		parts = append(parts, errLine(t, v.scheduleErr))
	}
	if v.wolErr != nil {
		parts = append(parts, errLine(t, v.wolErr))
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  r refresh"))
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *PowerView) renderWOL(width int) string {
	t := v.ctx.Theme
	enabledChip := t.Chip(t.Muted).Render(" disabled ")
	if v.wol != nil && v.wol.Enabled {
		enabledChip = t.Chip(t.Accent2).Render(" enabled ")
	}
	mac := "—"
	if v.wol != nil && v.wol.MACAddress != "" {
		mac = v.wol.MACAddress
	}
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	body := t.Title().Render(" Wake-on-LAN ") + "\n" +
		"  " + enabledChip + "   " + mu.Render("MAC: ") + text.Render(mac)
	return t.Card(false).Width(width - 2).Render(body)
}

func (v *PowerView) renderSchedule(width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)

	header := t.Title().Render(" Power schedule ")
	if len(v.schedule) == 0 {
		body := header + "\n" + mu.Render("  No scheduled power events configured.")
		return t.Card(false).Width(width - 2).Render(body)
	}

	// Render as a compact aligned table. Columns: day (8) · time (8) · action (10) · enabled chip.
	var rows []string
	for _, s := range v.schedule {
		dayCell := padRight(text.Render(prettyDay(s.Day)), 10)
		timeCell := padRight(text.Render(fmt.Sprintf("%02d:%02d", s.Hour, s.Minute)), 8)
		actionCell := padRight(actionStyle(t, s.Action).Render(prettyAction(s.Action)), 14)
		stateCell := stateChip(t, s.Enabled)
		rows = append(rows, "  "+dayCell+" "+timeCell+" "+actionCell+" "+stateCell)
	}
	body := header + "\n" + strings.Join(rows, "\n")
	return t.Card(false).Width(width - 2).Render(body)
}

// actionStyle picks a foreground colour for the action label. We use
// the warn colour for shutdowns so the row visually signals "the box
// will go away at this time" even at a glance.
func actionStyle(t tui.Theme, action string) lipgloss.Style {
	switch action {
	case "power-off":
		return lipgloss.NewStyle().Foreground(t.Warn).Bold(true)
	case "restart":
		return lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	}
}

func stateChip(t tui.Theme, on bool) string {
	if on {
		return t.Chip(t.Accent2).Render(" on ")
	}
	return t.Chip(t.Muted).Render(" off ")
}

// prettyDay maps DSM's lowercase 3-letter day keys to capitalised
// labels. Anything else (e.g. "daily" / "weekdays") is title-cased
// rather than reshaped, so we pass through DSM-specific extras the
// firmware adds.
func prettyDay(d string) string {
	switch strings.ToLower(d) {
	case "sun":
		return "Sun"
	case "mon":
		return "Mon"
	case "tue":
		return "Tue"
	case "wed":
		return "Wed"
	case "thu":
		return "Thu"
	case "fri":
		return "Fri"
	case "sat":
		return "Sat"
	case "daily":
		return "Daily"
	case "weekdays":
		return "Weekdays"
	case "weekends":
		return "Weekends"
	}
	if len(d) == 0 {
		return "—"
	}
	return strings.ToUpper(d[:1]) + d[1:]
}

func prettyAction(a string) string {
	switch a {
	case "power-on":
		return "Power on"
	case "power-off":
		return "Power off"
	case "restart":
		return "Restart"
	}
	if a == "" {
		return "—"
	}
	return a
}
