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

// Volumes renders DSM's storage hardware surface: logical volumes and
// physical disks in one list. Both are returned by the same DSM storage
// endpoint, so keeping them together avoids two mostly-empty pages while
// preserving the existing drill-down detail views.
type Volumes struct {
	ctx Ctx

	storage *dsm.Storage
	err     error

	base         listBase
	detailVolume *dsm.Volume
	detailDisk   *dsm.Disk
}

type storageRowKind int

const (
	storageRowVolume storageRowKind = iota
	storageRowDisk
)

type storageRow struct {
	kind  storageRowKind
	index int
}

// NewVolumes constructs the combined storage hardware view.
func NewVolumes(c Ctx) tui.View { return &Volumes{ctx: c} }

func (v *Volumes) Name() string                   { return "storage" }
func (v *Volumes) Title() string                  { return "Storage Health" }
func (v *Volumes) Icon() string                   { return "▮" }
func (v *Volumes) RefreshInterval() time.Duration { return 15 * time.Second }
func (v *Volumes) Bindings() []key.Binding        { return BaseBindings() }
func (v *Volumes) Hint() string                   { return "↑/↓ move · ⏎ details · / filter · r refresh" }

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

func (v *Volumes) visibleVolumes() []dsm.Volume {
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

func (v *Volumes) visibleDisks() []dsm.Disk {
	if v.storage == nil {
		return nil
	}
	if v.base.FilterValue() == "" {
		return v.storage.Disks
	}
	out := make([]dsm.Disk, 0, len(v.storage.Disks))
	for _, x := range v.storage.Disks {
		if MatchesAll(v.base.FilterValue(), x.ID, x.Model, x.Vendor, x.Status, x.DiskType, x.Serial) {
			out = append(out, x)
		}
	}
	return out
}

func (v *Volumes) rows() []storageRow {
	volumes := v.visibleVolumes()
	disks := v.visibleDisks()
	out := make([]storageRow, 0, len(volumes)+len(disks))
	for i := range volumes {
		out = append(out, storageRow{kind: storageRowVolume, index: i})
	}
	for i := range disks {
		out = append(out, storageRow{kind: storageRowDisk, index: i})
	}
	return out
}

func (v *Volumes) current() (storageRow, bool) {
	rows := v.rows()
	if v.base.Cursor() < 0 || v.base.Cursor() >= len(rows) {
		return storageRow{}, false
	}
	return rows[v.base.Cursor()], true
}

func (v *Volumes) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if v.detailVolume != nil || v.detailDisk != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detailVolume, v.detailDisk = nil, nil
		}
		return v, nil
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return v, v.fetch()
	case storageMsg:
		v.storage, v.err = m.S, m.Err
		v.base.ClampCursor(len(v.rows()))
		return v, nil
	}

	if _, handled := v.base.HandleKey(msg, len(v.rows())); handled {
		return v, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && km.Type == tea.KeyEnter {
		if row, ok := v.current(); ok {
			switch row.kind {
			case storageRowVolume:
				vol := v.visibleVolumes()[row.index]
				v.detailVolume = &vol
			case storageRowDisk:
				disk := v.visibleDisks()[row.index]
				v.detailDisk = &disk
			}
		}
	}
	return v, nil
}

func (v *Volumes) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detailVolume != nil {
		pools, disks := []dsm.StoragePool{}, []dsm.Disk{}
		if v.storage != nil {
			pools = v.storage.StoragePools
			disks = v.storage.Disks
		}
		return renderVolumeDetail(t, width, height, *v.detailVolume, pools, disks, nil)
	}
	if v.detailDisk != nil {
		pools := []dsm.StoragePool{}
		if v.storage != nil {
			pools = v.storage.StoragePools
		}
		return renderDiskDetail(t, width, height, *v.detailDisk, pools)
	}

	volumes := v.visibleVolumes()
	disks := v.visibleDisks()
	cursor := v.base.Cursor()
	idx := 0

	parts := []string{sectionHeader(t, width, "Volumes", len(volumes), v.err)}
	if v.storage == nil && v.err == nil {
		parts = append(parts, "  "+muted(t, "loading..."))
	} else if len(volumes) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for _, vol := range volumes {
		parts = append(parts, v.renderVolumeRow(width, vol, idx == cursor))
		idx++
	}

	parts = append(parts, "", sectionHeader(t, width, "Disks", len(disks), nil))
	if v.storage == nil && v.err == nil {
		parts = append(parts, "  "+muted(t, "loading..."))
	} else if len(disks) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for _, disk := range disks {
		parts = append(parts, v.renderDiskRow(width, disk, idx == cursor))
		idx++
	}

	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter"))
	if f := v.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

// Inspect renders the cursor'd storage entity in the right-pane inspector.
func (v *Volumes) Inspect(width, height int) string {
	row, ok := v.current()
	if !ok {
		return ""
	}
	switch row.kind {
	case storageRowVolume:
		return v.inspectVolume(width, height, v.visibleVolumes()[row.index])
	case storageRowDisk:
		return v.inspectDisk(width, height, v.visibleDisks()[row.index])
	default:
		return ""
	}
}

func (v *Volumes) inspectVolume(width, height int, vol dsm.Volume) string {
	t := v.ctx.Theme
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

func (v *Volumes) inspectDisk(width, height int, disk dsm.Disk) string {
	t := v.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	tempStyle := lipgloss.NewStyle().Foreground(tempColor(t, disk.Temperature)).Bold(true)
	smart := disk.Smart.Status
	if smart == "" {
		smart = "-"
	}
	parts := []string{
		t.Title().Render(" " + trimDev(disk.ID) + " "),
		"",
		text.Render(strings.TrimSpace(disk.Vendor + " " + disk.Model)),
		muted.Render(disk.DiskType + " · " + disk.Firmware),
		"",
		muted.Render("Capacity: ") + text.Render(HumanBytes(ParseSizeString(disk.Capacity))),
		muted.Render("Temp:     ") + tempStyle.Render(fmt.Sprintf("%d °C", disk.Temperature)),
		muted.Render("Status:   ") + t.HealthStyle(disk.Status).Render(disk.Status),
		muted.Render("SMART:    ") + text.Render(smart),
	}
	if disk.Serial != "" {
		parts = append(parts, muted.Render("Serial:   ")+text.Render(disk.Serial))
	}
	if disk.Container.Str != "" {
		parts = append(parts, "", muted.Render("Pool: ")+text.Render(disk.Container.Str))
	}
	_ = width
	_ = height
	return strings.Join(parts, "\n")
}

func (v *Volumes) renderVolumeRow(width int, vol dsm.Volume, highlight bool) string {
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

func (v *Volumes) renderDiskRow(width int, disk dsm.Disk, highlight bool) string {
	t := v.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	capacity := HumanBytes(ParseSizeString(disk.Capacity))
	bay := trimDev(disk.ID)
	temp := lipgloss.NewStyle().Foreground(tempColor(t, disk.Temperature)).Bold(true).Render(
		fmt.Sprintf("%d°C", disk.Temperature))
	smart := disk.Smart.Status
	if smart == "" {
		smart = "-"
	}
	_ = width
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(bay), 6), " ",
		padRight(muted.Render(strings.TrimSpace(disk.Vendor+" "+disk.Model)), 28), " ",
		padRight(muted.Render(disk.DiskType), 10), " ",
		padLeft(text.Render(capacity), 10), "  ",
		temp, "  ",
		padRight(muted.Render("SMART "+smart), 14), " ",
		t.HealthStyle(disk.Status).Render(disk.Status),
	)
}
