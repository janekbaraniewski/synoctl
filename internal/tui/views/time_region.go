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

// TimeRegionView shows DSM's Control Panel → Regional Options surface:
// timezone, NTP server, locale formats. It refreshes every second so
// the "live clock" line keeps moving, but actually re-fetching DSM
// settings at 1 Hz would hammer the API — we re-poll the configuration
// at 60 s and update the local clock between fetches from the system
// time.

type timeRegionMsg struct {
	T   *dsm.TimeRegion
	Err error
}

type TimeRegionView struct {
	ctx Ctx

	region    *dsm.TimeRegion
	regionErr error
	loaded    bool

	// fetched is when we last successfully retrieved settings from DSM.
	// We use it to gate actual API calls — the 1 s tick only drives the
	// clock; settings re-fetch happens at most once a minute.
	fetched time.Time
}

// NewTimeRegion constructs the time/region view.
func NewTimeRegion(c Ctx) tui.View { return &TimeRegionView{ctx: c} }

func (v *TimeRegionView) Name() string                   { return "time-region" }
func (v *TimeRegionView) Title() string                  { return "Time & Region" }
func (v *TimeRegionView) Icon() string                   { return "⌚" }
func (v *TimeRegionView) RefreshInterval() time.Duration { return 1 * time.Second }
func (v *TimeRegionView) Hint() string                   { return "r refresh" }
func (v *TimeRegionView) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

func (v *TimeRegionView) Init() tea.Cmd { return v.fetch() }

func (v *TimeRegionView) fetch() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (*dsm.TimeRegion, error) { return c.TimeRegion(ctx) },
		func(t *dsm.TimeRegion, err error) tea.Msg { return timeRegionMsg{T: t, Err: err} },
	)
}

func (v *TimeRegionView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case tui.TickMsg:
		// Only re-fetch DSM settings once a minute; the per-second tick
		// otherwise just drives the clock redraw.
		if time.Since(v.fetched) > 60*time.Second {
			return v, v.fetch()
		}
		return v, nil
	case timeRegionMsg:
		v.region, v.regionErr = m.T, m.Err
		v.loaded = true
		if m.Err == nil {
			v.fetched = time.Now()
		}
		return v, nil
	case tea.KeyMsg:
		if m.String() == "r" {
			return v, v.fetch()
		}
	}
	return v, nil
}

func (v *TimeRegionView) Render(width, height int) string {
	t := v.ctx.Theme
	if !v.loaded && v.regionErr == nil {
		return fitOrScroll(Card(t, width, " ⌚  Time & Region ", "\n  asking DSM for region settings…\n", true), height)
	}
	if v.region == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⌚  Time & Region",
			"DSM didn't return regional settings.",
			"This is unusual — every DSM box has a timezone. Try `r` to retry."), height)
	}
	r := v.region

	// Live clock — driven by the 1 Hz tick so the seconds field
	// actually moves. We format with the user's preferred 12/24 hour
	// format when DSM told us one.
	format := "15:04:05"
	if r.TimeFormat == "12" {
		format = "03:04:05 PM"
	}
	now := time.Now()
	clock := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).
		Render(now.Format(format))
	clockLine := lipgloss.NewStyle().Foreground(t.Muted).Render("  Live clock: ") + clock

	ntpStatus := "off"
	if r.NTPEnabled {
		ntpStatus = "on"
	}
	autoDST := "off"
	if r.AutoDST {
		autoDST = "on"
	}
	tz := r.TimeZone
	if r.TimeZoneDesc != "" {
		tz = r.TimeZone + " — " + r.TimeZoneDesc
	}

	parts := []string{
		hero(t, width, "⌚", "Time & Region", ntpStatus, tz),
		t.Card(false).Width(width - 2).Render(
			t.Title().Render(" Live ") + "\n" + clockLine),
		propsCard(t, width, " Region ", [][2]string{
			{"Time zone", r.TimeZone},
			{"Description", r.TimeZoneDesc},
			{"NTP enabled", ntpStatus},
			{"NTP server", r.NTPServer},
			{"Auto DST", autoDST},
			{"Time format", r.TimeFormat + "h"},
			{"Date format", r.DateFormat},
			{"Current system time", r.CurrentTime},
		}),
	}
	if v.regionErr != nil {
		parts = append(parts, noteCard(t, width, "  "+v.regionErr.Error()))
	}
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  r refresh"))
	return fitOrScroll(strings.Join(parts, "\n"), height)
}
