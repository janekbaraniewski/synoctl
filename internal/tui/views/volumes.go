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

// Volumes is the dedicated view for logical volumes — one row per volume
// with a usage gauge, status chip, and capacity numbers. Drill-down opens
// the structured volume detail with pool + contributing disks.
type Volumes struct {
	ctx Ctx

	storage *dsm.Storage
	err     error

	base   listBase
	detail *dsm.Volume
}

// NewVolumes constructs the volumes view.
func NewVolumes(c Ctx) tui.View { return &Volumes{ctx: c} }

func (v *Volumes) Name() string                   { return "volumes" }
func (v *Volumes) Title() string                  { return "Volumes" }
func (v *Volumes) Icon() string                   { return "▮" }
func (v *Volumes) RefreshInterval() time.Duration { return 15 * time.Second }
func (v *Volumes) Bindings() []key.Binding        { return BaseBindings() }

func (v *Volumes) Init() tea.Cmd { return v.fetch() }

func (v *Volumes) fetch() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.Storage, error) { return c.Storage(ctx) },
		func(s *dsm.Storage, err error) tea.Msg { return storageMsg{S: s, Err: err} },
	)
}

func (v *Volumes) visible() []dsm.Volume {
	if v.storage == nil {
		return nil
	}
	if v.base.FilterValue() == "" {
		return v.storage.Volumes
	}
	out := make([]dsm.Volume, 0, len(v.storage.Volumes))
	for _, x := range v.storage.Volumes {
		if MatchesAll(v.base.FilterValue(), x.ID, x.VolPath, x.FSType, x.RaidType, x.Status, x.Desc) {
			out = append(out, x)
		}
	}
	return out
}

func (v *Volumes) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if v.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detail = nil
		}
		return v, nil
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return v, v.fetch()
	case storageMsg:
		v.storage, v.err = m.S, m.Err
		v.base.ClampCursor(len(v.visible()))
		return v, nil
	}

	if _, handled := v.base.HandleKey(msg, len(v.visible())); handled {
		return v, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyEnter {
		if rows := v.visible(); v.base.Cursor() < len(rows) {
			vol := rows[v.base.Cursor()]
			v.detail = &vol
		}
	}
	return v, nil
}

func (v *Volumes) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detail != nil {
		pools, disks := []dsm.StoragePool{}, []dsm.Disk{}
		if v.storage != nil {
			pools = v.storage.StoragePools
			disks = v.storage.Disks
		}
		return renderVolumeDetail(t, width, height, *v.detail, pools, disks, nil)
	}

	rows := v.visible()
	parts := []string{sectionHeader(t, width, "Volumes", len(rows), v.err)}
	if v.storage == nil && v.err == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for i, vol := range rows {
		parts = append(parts, v.renderRow(width, vol, i == v.base.Cursor()))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter"))
	if f := v.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

// Inspect renders the cursor'd volume in the right-pane inspector — a
// compact summary so the user gets feedback as they scroll. Full drill-
// down is still on enter.
func (v *Volumes) Inspect(width, height int) string {
	rows := v.visible()
	if len(rows) == 0 || v.base.Cursor() >= len(rows) {
		return ""
	}
	t := v.ctx.Theme
	vol := rows[v.base.Cursor()]
	total := ParseSizeString(vol.Size.Total)
	used := ParseSizeString(vol.Size.Used)
	ratio := 0.0
	if total > 0 {
		ratio = float64(used) / float64(total)
	}
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	parts := []string{
		t.Title().Render(" " + coalesce(vol.VolPath, vol.ID) + " "),
		"",
		Gauge(t, width-2, ratio),
		fmt.Sprintf("%s %s used of %s",
			lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(fmt.Sprintf("%5.1f%%", ratio*100)),
			HumanBytes(used), HumanBytes(total)),
		"",
		muted.Render("FS:      ") + text.Render(strings.ToUpper(vol.FSType)),
		muted.Render("RAID:    ") + text.Render(coalesce(vol.RaidType, vol.DeviceType)),
		muted.Render("Status:  ") + t.HealthStyle(vol.Status).Render(vol.Status),
	}
	if vol.SummaryStatus != "" && vol.SummaryStatus != vol.Status {
		parts = append(parts, muted.Render("Summary: ")+text.Render(vol.SummaryStatus))
	}
	_ = height
	return strings.Join(parts, "\n")
}

func (v *Volumes) renderRow(width int, vol dsm.Volume, highlight bool) string {
	t := v.ctx.Theme
	name := vol.VolPath
	if name == "" {
		name = vol.ID
	}
	total := ParseSizeString(vol.Size.Total)
	used := ParseSizeString(vol.Size.Used)
	ratio := 0.0
	if total > 0 {
		ratio = float64(used) / float64(total)
	}
	barW := max(width-72, 16)
	bar := Gauge(t, barW, ratio)
	status := t.HealthStyle(vol.Status).Render(vol.Status)
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(name), 12), " ",
		padRight(muted.Render(strings.ToUpper(vol.FSType)+"·"+vol.RaidType), 14), " ",
		bar, " ",
		padLeft(text.Render(fmt.Sprintf("%5.1f%%", ratio*100)), 7), "  ",
		padLeft(muted.Render(fmt.Sprintf("%s / %s", HumanBytes(used), HumanBytes(total))), 22), "  ",
		status,
	)
}
