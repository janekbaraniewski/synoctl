package views

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// DSMUpdateView surfaces DSM's Control Panel → Update & Restore status.
// Read-only on purpose for this pass — applying an update from the TUI
// would need a real "are you sure, the box will reboot" workflow that
// we haven't designed yet. Pressing `r` re-runs the upstream check.

type dsmUpdateMsg struct {
	S   *dsm.DSMUpdateStatus
	Err error
}

type DSMUpdateView struct {
	ctx Ctx

	status    *dsm.DSMUpdateStatus
	statusErr error
	loaded    bool

	flash string
}

// NewDSMUpdate constructs the DSM update view.
func NewDSMUpdate(c Ctx) tui.View { return &DSMUpdateView{ctx: c} }

func (v *DSMUpdateView) Name() string  { return "dsm-update" }
func (v *DSMUpdateView) Title() string { return "DSM Update" }
func (v *DSMUpdateView) Icon() string  { return "⇡" }

// 10 minutes — checking the upstream catalog repeatedly is rude, and
// nothing about this surface needs to be live.
func (v *DSMUpdateView) RefreshInterval() time.Duration { return 10 * time.Minute }

func (v *DSMUpdateView) Hint() string { return "r re-check upstream" }
func (v *DSMUpdateView) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "re-check upstream")),
	}
}

func (v *DSMUpdateView) Init() tea.Cmd { return v.fetch() }

func (v *DSMUpdateView) fetch() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	// 30s — DSM hits the upstream catalog on this call and a residential
	// uplink with a packet-loss spike can stretch that out.
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (*dsm.DSMUpdateStatus, error) { return c.DSMUpdate(ctx) },
		func(s *dsm.DSMUpdateStatus, err error) tea.Msg { return dsmUpdateMsg{S: s, Err: err} },
	)
}

func (v *DSMUpdateView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, v.fetch()
	case dsmUpdateMsg:
		v.status, v.statusErr = m.S, m.Err
		v.loaded = true
		if m.Err == nil {
			v.flash = "checked just now"
		}
		return v, nil
	case tea.KeyMsg:
		if m.String() == "r" {
			v.flash = "checking upstream…"
			return v, v.fetch()
		}
	}
	return v, nil
}

func (v *DSMUpdateView) Render(width, height int) string {
	t := v.ctx.Theme
	if !v.loaded && v.statusErr == nil {
		return fitOrScroll(Card(t, width, " ⇡  DSM Update ", "\n  asking DSM for the latest catalog entry…\n", true), height)
	}
	if v.status == nil {
		// API completely unavailable — render a graceful empty state.
		return fitOrScroll(emptyStateCard(t, width,
			"⇡  DSM Update",
			"DSM didn't return any update information.",
			"This is normal on boxes that disable upstream checks at the package layer. Try `r` to retry."), height)
	}

	s := v.status
	chip := updateChip(t, s.UpdateAvailable)
	lastCheck := "—"
	if s.LastCheck > 0 {
		lastCheck = time.Unix(s.LastCheck, 0).Format("2006-01-02 15:04")
	}
	channel := s.UpdateChannel
	if channel == "" {
		channel = "stable"
	}
	available := s.AvailableVersion
	if available == "" {
		available = "—"
	}
	autoUpdate := "off"
	if s.AutoUpdateEnabled {
		autoUpdate = "on"
	}

	parts := []string{
		hero(t, width, "⇡", "DSM Update", chipText(s.UpdateAvailable), s.CurrentVersion),
		propsCard(t, width, " Update ", [][2]string{
			{"Installed version", s.CurrentVersion},
			{"Latest available", available},
			{"Channel", channel},
			{"Auto-update", autoUpdate},
			{"Last check", lastCheck},
			{"Release notes", s.ReleaseNotesURL},
		}),
		chipsCard(t, width, " Status ", []string{chip}),
	}
	if v.statusErr != nil {
		parts = append(parts, noteCard(t, width, "  "+v.statusErr.Error()))
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  r re-check upstream"))
	if v.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+v.flash))
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

// chipText returns the status word displayed beside the update title.
func chipText(updateAvailable bool) string {
	if updateAvailable {
		return "update available"
	}
	return "up to date"
}

// updateChip renders the colour-coded "update available / up to date"
// chip used on the status card. We use t.Warn for "you should patch"
// rather than t.Error — a missed update is rarely critical, just
// untidy.
func updateChip(t tui.Theme, updateAvailable bool) string {
	if updateAvailable {
		return t.Chip(t.Warn).Render(" update available ")
	}
	return t.Chip(t.Accent2).Render(" up to date ")
}
