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

// SurveillanceView lists configured Surveillance Station cameras.
// When the package isn't installed (Cameras returns an empty slice
// via Supports() short-circuit) we render a tasteful empty-state.

type camerasMsg struct {
	C   []dsm.Camera
	Err error
}
type surveillanceInfoMsg struct {
	I   *dsm.SurveillanceInfo
	Err error
}

type SurveillanceView struct {
	ctx Ctx

	cams []dsm.Camera
	info *dsm.SurveillanceInfo

	camsErr, infoErr error

	cursor int
	filter Filter
	loaded bool

	detail *dsm.Camera
}

func NewSurveillance(c Ctx) tui.View { return &SurveillanceView{ctx: c} }

func (v *SurveillanceView) Name() string                   { return "cameras" }
func (v *SurveillanceView) Title() string                  { return "Cameras" }
func (v *SurveillanceView) Icon() string                   { return "◉" }
func (v *SurveillanceView) RefreshInterval() time.Duration { return 30 * time.Second }
func (v *SurveillanceView) Bindings() []key.Binding        { return BaseBindings() }
func (v *SurveillanceView) IsTextEditing() bool            { return v.filter.IsActive() }

func (v *SurveillanceView) Init() tea.Cmd {
	return tea.Batch(v.fetchCams(), v.fetchInfo())
}

func (v *SurveillanceView) fetchCams() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.Camera, error) { return c.Cameras(ctx) },
		func(x []dsm.Camera, err error) tea.Msg { return camerasMsg{C: x, Err: err} },
	)
}
func (v *SurveillanceView) fetchInfo() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.SurveillanceInfo, error) { return c.SurveillanceInfo(ctx) },
		func(i *dsm.SurveillanceInfo, err error) tea.Msg { return surveillanceInfoMsg{I: i, Err: err} },
	)
}

func (v *SurveillanceView) filtered() []dsm.Camera {
	if v.filter.Value() == "" {
		return v.cams
	}
	out := make([]dsm.Camera, 0, len(v.cams))
	for _, c := range v.cams {
		if MatchesAll(v.filter.Value(), c.Name, c.Model, c.Vendor, c.IP, c.Resolution, c.Group) {
			out = append(out, c)
		}
	}
	return out
}

func (v *SurveillanceView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if v.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detail = nil
		}
		return v, nil
	}
	if v.filter.IsActive() {
		before := v.filter.Value()
		if v.filter.Update(msg) {
			if v.filter.Value() != before {
				v.cursor = 0
			}
			return v, nil
		}
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchCams(), v.fetchInfo())
	case camerasMsg:
		v.cams, v.camsErr = m.C, m.Err
		v.loaded = true
		v.clampCursor()
	case surveillanceInfoMsg:
		v.info, v.infoErr = m.I, m.Err
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := v.filtered()
			if v.cursor < len(rows)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g":
			v.cursor = 0
		case "G":
			v.cursor = max(len(v.filtered())-1, 0)
		case "/":
			v.filter.Open()
			v.cursor = 0
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, tea.Batch(v.fetchCams(), v.fetchInfo())
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				c := rows[v.cursor]
				v.detail = &c
			}
		}
	}
	return v, nil
}

func (v *SurveillanceView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *SurveillanceView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detail != nil {
		return renderCameraDetail(t, width, *v.detail)
	}

	// Empty state: Surveillance Station not installed OR no cameras
	// configured. We can't distinguish the two without an API probe,
	// so we phrase the empty-state to cover both.
	if v.loaded && len(v.cams) == 0 && v.camsErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"◉  Surveillance Station",
			"Surveillance Station is not installed, or no cameras have been configured.",
			"Install Surveillance Station from Package Center and add a camera to see live status here."), height)
	}

	cams := v.filtered()
	var parts []string
	if v.info != nil {
		parts = append(parts, v.renderInfoStrip(width))
	}
	parts = append(parts, sectionHeader(t, width, "Cameras", len(cams), v.camsErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(cams) == 0 {
		parts = append(parts, "  "+muted(t, "(none matching)"))
	}
	for i, c := range cams {
		parts = append(parts, v.renderRow(c, i == v.cursor))
	}

	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · esc clear · r refresh"))
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *SurveillanceView) renderInfoStrip(width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	pair := func(k, val string) string {
		if val == "" {
			val = "—"
		}
		return mu.Render(k+":") + " " + text.Render(val)
	}
	body := t.Title().Render(" Surveillance Station ") + "\n" +
		strings.Join([]string{
			pair("Version", v.info.Version),
			pair("Cameras", fmt.Sprintf("%d / %d", v.info.CameraNumber, v.info.MaxCamera)),
			pair("Hostname", v.info.Hostname),
		}, "   ")
	return t.Card(false).Width(width - 2).Render(body)
}

func camStatusLabel(c dsm.Camera) string {
	// DSM-side status codes: 1=enabled, 7=disconnected, 11=unknown.
	switch c.Status {
	case 1:
		if c.Enabled.Bool() {
			return "connected"
		}
		return "disabled"
	case 7:
		return "disconnected"
	default:
		if c.Enabled.Bool() {
			return "starting"
		}
		return "disabled"
	}
}

func (v *SurveillanceView) renderRow(c dsm.Camera, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	status := camStatusLabel(c)
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padLeft(mu.Render(fmt.Sprintf("%d", c.ID)), 4), " ",
		padRight(text.Render(c.Name), 22), " ",
		padRight(mu.Render(clipTo(strings.TrimSpace(c.Vendor+" "+c.Model), 28)), 28), " ",
		padRight(mu.Render(c.IP), 16), " ",
		t.HealthStyle(status).Render(status),
	)
}

func renderCameraDetail(t tui.Theme, width int, c dsm.Camera) string {
	if width < 60 {
		width = 60
	}
	status := camStatusLabel(c)
	parts := []string{
		hero(t, width, "◉", c.Name, status, c.Model),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", fmt.Sprintf("%d", c.ID)},
			{"Name", c.Name},
			{"Vendor", c.Vendor},
			{"Model", c.Model},
			{"IP", c.IP},
			{"Port", fmt.Sprintf("%d", c.Port)},
			{"Resolution", c.Resolution},
			{"FPS", fmt.Sprintf("%d", c.FPS)},
			{"Group", c.Group},
			{"Storage path", c.DSPath},
			{"Volume use", c.VolumeSpace},
			{"Last connected", c.LastConnected},
		}),
	}
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	parts = append(parts, chipsCard(t, width, " Flags ", []string{
		chip("enabled", c.Enabled.Bool()),
		chip("recording", c.Recording.Bool()),
		chip("PTZ capable", c.HasPTZ.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · camera control isn't wired up yet"))
	return strings.Join(parts, "\n")
}
