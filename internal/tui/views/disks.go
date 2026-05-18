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

// Disks is the dedicated view for physical drives — one row per disk with
// bay, vendor/model, capacity, temperature and SMART status. Drill-down
// opens the structured disk detail (thermal banding + pool membership).
type Disks struct {
	ctx Ctx

	storage *dsm.Storage
	err     error

	base   listBase
	detail *dsm.Disk
}

// NewDisks constructs the disks view.
func NewDisks(c Ctx) tui.View { return &Disks{ctx: c} }

func (d *Disks) Name() string                   { return "disks" }
func (d *Disks) Title() string                  { return "Disks" }
func (d *Disks) Icon() string                   { return "●" }
func (d *Disks) RefreshInterval() time.Duration { return 30 * time.Second }
func (d *Disks) Bindings() []key.Binding        { return BaseBindings() }

func (d *Disks) Init() tea.Cmd { return d.fetch() }

func (d *Disks) fetch() tea.Cmd {
	c := d.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.Storage, error) { return c.Storage(ctx) },
		func(s *dsm.Storage, err error) tea.Msg { return storageMsg{S: s, Err: err} },
	)
}

func (d *Disks) visible() []dsm.Disk {
	if d.storage == nil {
		return nil
	}
	if d.base.FilterValue() == "" {
		return d.storage.Disks
	}
	out := make([]dsm.Disk, 0, len(d.storage.Disks))
	for _, x := range d.storage.Disks {
		if MatchesAll(d.base.FilterValue(), x.ID, x.Model, x.Vendor, x.Status, x.DiskType, x.Serial) {
			out = append(out, x)
		}
	}
	return out
}

func (d *Disks) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if d.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			d.detail = nil
		}
		return d, nil
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return d, d.fetch()
	case storageMsg:
		d.storage, d.err = m.S, m.Err
		d.base.ClampCursor(len(d.visible()))
		return d, nil
	}
	if _, handled := d.base.HandleKey(msg, len(d.visible())); handled {
		return d, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyEnter {
		if rows := d.visible(); d.base.Cursor() < len(rows) {
			disk := rows[d.base.Cursor()]
			d.detail = &disk
		}
	}
	return d, nil
}

func (d *Disks) Render(width, height int) string {
	t := d.ctx.Theme
	if d.detail != nil {
		pools := []dsm.StoragePool{}
		if d.storage != nil {
			pools = d.storage.StoragePools
		}
		return renderDiskDetail(t, width, height, *d.detail, pools)
	}
	rows := d.visible()
	parts := []string{sectionHeader(t, width, "Disks", len(rows), d.err)}
	if d.storage == nil && d.err == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for i, dk := range rows {
		parts = append(parts, d.renderRow(width, dk, i == d.base.Cursor()))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter"))
	if f := d.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (d *Disks) Inspect(width, height int) string {
	rows := d.visible()
	if len(rows) == 0 || d.base.Cursor() >= len(rows) {
		return ""
	}
	t := d.ctx.Theme
	dk := rows[d.base.Cursor()]
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	tempStyle := lipgloss.NewStyle().Foreground(tempColor(t, dk.Temperature)).Bold(true)
	smart := dk.Smart.Status
	if smart == "" {
		smart = "—"
	}
	parts := []string{
		t.Title().Render(" " + trimDev(dk.ID) + " "),
		"",
		text.Render(strings.TrimSpace(dk.Vendor + " " + dk.Model)),
		muted.Render(dk.DiskType + " · " + dk.Firmware),
		"",
		muted.Render("Capacity: ") + text.Render(HumanBytes(ParseSizeString(dk.Capacity))),
		muted.Render("Temp:     ") + tempStyle.Render(fmt.Sprintf("%d °C", dk.Temperature)),
		muted.Render("Status:   ") + t.HealthStyle(dk.Status).Render(dk.Status),
		muted.Render("SMART:    ") + text.Render(smart),
	}
	if dk.Serial != "" {
		parts = append(parts, muted.Render("Serial:   ")+text.Render(dk.Serial))
	}
	if dk.Container.Str != "" {
		parts = append(parts, "", muted.Render("Pool: ")+text.Render(dk.Container.Str))
	}
	_ = height
	return strings.Join(parts, "\n")
}

func (d *Disks) renderRow(width int, dk dsm.Disk, highlight bool) string {
	t := d.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	capacity := HumanBytes(ParseSizeString(dk.Capacity))
	bay := trimDev(dk.ID)
	temp := lipgloss.NewStyle().Foreground(tempColor(t, dk.Temperature)).Bold(true).Render(
		fmt.Sprintf("%d°C", dk.Temperature))
	smart := dk.Smart.Status
	if smart == "" {
		smart = "—"
	}
	_ = width
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(bay), 6), " ",
		padRight(muted.Render(strings.TrimSpace(dk.Vendor+" "+dk.Model)), 28), " ",
		padRight(muted.Render(dk.DiskType), 10), " ",
		padLeft(text.Render(capacity), 10), "  ",
		temp, "  ",
		padRight(muted.Render("SMART "+smart), 14), " ",
		t.HealthStyle(dk.Status).Render(dk.Status),
	)
}
