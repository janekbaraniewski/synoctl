package views

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// renderVolumeDetail builds the rich drill-down for a single volume. The
// layout is a stack of panels — hero, capacity, inodes, properties,
// capabilities, suggestions, contributing disks, raw payload — sized to
// the available canvas. Each panel is its own rounded card so users get
// a clear visual hierarchy and can scan to the bit they care about.
func renderVolumeDetail(t tui.Theme, width, height int, vol dsm.Volume, pools []dsm.StoragePool, disks []dsm.Disk, raw json.RawMessage) string {
	if width < 60 {
		width = 60
	}

	total := ParseSizeString(vol.Size.Total)
	used := ParseSizeString(vol.Size.Used)
	var free uint64
	if total > used {
		free = total - used
	}
	ratio := 0.0
	if total > 0 {
		ratio = float64(used) / float64(total)
	}

	parts := []string{
		volumeHero(t, width, vol),
		volumeCapacityCard(t, width, vol, ratio, used, free, total),
		volumeInodesCard(t, width, vol),
		volumePropsCard(t, width, vol),
		volumeCapabilitiesCard(t, width, raw),
		volumeSuggestionsCard(t, width, raw),
		volumeDisksCard(t, width, vol, pools, disks),
	}
	body := strings.Join(parts, "\n")
	footer := lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  esc to go back · F browses files on this volume")
	body = body + "\n" + footer
	return body
}

func volumeHero(t tui.Theme, width int, vol dsm.Volume) string {
	name := vol.VolPath
	if name == "" {
		name = vol.ID
	}
	title := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("▮  " + name)
	status := t.HealthStyle(vol.Status).Render(" " + vol.Status + " ")
	subtitle := lipgloss.NewStyle().Foreground(t.Muted).Render(
		strings.ToUpper(vol.FSType) + " · " + vol.RaidType + " · " + vol.Desc)
	header := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", status, "  ", subtitle)
	return t.Card(true).Width(width - 2).Render(header)
}

func volumeCapacityCard(t tui.Theme, width int, vol dsm.Volume, ratio float64, used, free, total uint64) string {
	innerW := width - 6
	barW := innerW - 18
	if barW < 16 {
		barW = innerW
	}
	bar := Gauge(t, barW, ratio)
	big := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(fmt.Sprintf("%5.1f%%", ratio*100))
	stats := lipgloss.JoinHorizontal(lipgloss.Bottom,
		lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(HumanBytes(used)),
		lipgloss.NewStyle().Foreground(t.Muted).Render(" used  ·  "),
		lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(HumanBytes(free)),
		lipgloss.NewStyle().Foreground(t.Muted).Render(" free  ·  "),
		lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(HumanBytes(total)),
		lipgloss.NewStyle().Foreground(t.Muted).Render(" total"),
	)
	title := t.Title().Render(" Capacity ")
	body := title + "\n" + bar + "   " + big + "\n" + stats
	return t.Card(false).Width(width - 2).Render(body)
}

func volumeInodesCard(t tui.Theme, width int, vol dsm.Volume) string {
	totalI := ParseSizeString(vol.Size.TotalInode)
	freeI := ParseSizeString(vol.Size.FreeInode)
	var usedI uint64
	if totalI > freeI {
		usedI = totalI - freeI
	}
	ratio := 0.0
	if totalI > 0 {
		ratio = float64(usedI) / float64(totalI)
	}
	innerW := width - 6
	barW := innerW - 18
	if barW < 16 {
		barW = innerW
	}
	bar := Gauge(t, barW, ratio)
	big := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true).Render(fmt.Sprintf("%5.1f%%", ratio*100))
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	stats := lipgloss.JoinHorizontal(lipgloss.Bottom,
		text.Render(humanCount(usedI)),
		muted.Render(" used  ·  "),
		text.Render(humanCount(freeI)),
		muted.Render(" free  ·  "),
		text.Render(humanCount(totalI)),
		muted.Render(" total"),
	)
	title := t.Title().Render(" Inodes ")
	body := title + "\n" + bar + "   " + big + "\n" + stats
	return t.Card(false).Width(width - 2).Render(body)
}

func volumePropsCard(t tui.Theme, width int, vol dsm.Volume) string {
	writable := "no"
	if vol.IsWritable {
		writable = "yes"
	}
	props := [][2]string{
		{"ID", vol.ID},
		{"Path", vol.VolPath},
		{"Filesystem", strings.ToUpper(vol.FSType)},
		{"RAID type", vol.RaidType},
		{"Device type", vol.DeviceType},
		{"Description", vol.Desc},
		{"Container", vol.Container},
		{"Pool path", vol.PoolPath},
		{"Space path", vol.SpacePath},
		{"Writable", writable},
		{"Status", vol.Status},
		{"Summary status", vol.SummaryStatus},
	}
	rows := renderTwoColumnProps(t, width-6, props)
	title := t.Title().Render(" Properties ")
	return t.Card(false).Width(width - 2).Render(title + "\n" + rows)
}

func renderTwoColumnProps(t tui.Theme, inner int, kv [][2]string) string {
	// Two columns; each takes half the width.
	colW := inner / 2
	keyStyle := lipgloss.NewStyle().Foreground(t.Muted)
	valStyle := lipgloss.NewStyle().Foreground(t.Text).Bold(true)

	cell := func(k, v string) string {
		if v == "" {
			v = "—"
		}
		// Reserve ~16 chars for the key column.
		const keyW = 16
		key := keyStyle.Render(padRight(k, keyW))
		val := valStyle.Render(clipTo(v, colW-keyW-1))
		return key + " " + val
	}
	var b strings.Builder
	for i := 0; i < len(kv); i += 2 {
		left := cell(kv[i][0], kv[i][1])
		right := ""
		if i+1 < len(kv) {
			right = cell(kv[i+1][0], kv[i+1][1])
		}
		row := lipgloss.JoinHorizontal(lipgloss.Top,
			padRight(left, colW),
			right,
		)
		b.WriteString(row)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func volumeCapabilitiesCard(t tui.Theme, width int, raw json.RawMessage) string {
	type canDo struct {
		Delete       bool `json:"delete"`
		DiskReplace  bool `json:"disk_replace"`
		ExpandByDisk int  `json:"expand_by_disk"`
		ConvertShr   int  `json:"convert_shr_to_pool"`
	}
	var probe struct {
		CanDo canDo `json:"can_do"`
	}
	_ = json.Unmarshal(raw, &probe)

	chip := func(label string, ok bool) string {
		s := t.HealthStyle("error")
		if ok {
			s = t.HealthStyle("ok")
		}
		return s.Render(" " + label + " ")
	}
	chips := []string{
		chip("delete", probe.CanDo.Delete),
		chip("replace disk", probe.CanDo.DiskReplace),
		chip("expand", probe.CanDo.ExpandByDisk > 0),
		chip("convert SHR", probe.CanDo.ConvertShr > 0),
	}
	title := t.Title().Render(" Capabilities ")
	body := title + "\n  " + strings.Join(chips, "   ") + "\n" +
		lipgloss.NewStyle().Foreground(t.Faint).Render(
			"  (synoctl can read these flags; the destructive actions aren't wired up yet)")
	return t.Card(false).Width(width - 2).Render(body)
}

func volumeSuggestionsCard(t tui.Theme, width int, raw json.RawMessage) string {
	type sug struct {
		Section string   `json:"section"`
		Str     string   `json:"str"`
		Type    string   `json:"type"`
		Arg     []string `json:"arg"`
	}
	var probe struct {
		Suggestions []sug `json:"suggestions"`
	}
	_ = json.Unmarshal(raw, &probe)
	if len(probe.Suggestions) == 0 {
		return t.Card(false).Width(width - 2).Render(
			t.Title().Render(" Health & suggestions ") + "\n" +
				lipgloss.NewStyle().Foreground(t.Success).Bold(true).Render("  ✓ No issues reported"))
	}
	var lines []string
	for _, s := range probe.Suggestions {
		icon := lipgloss.NewStyle().Foreground(t.Warn).Bold(true).Render("  ⚠ ")
		if s.Type == "error" {
			icon = lipgloss.NewStyle().Foreground(t.Error).Bold(true).Render("  ✗ ")
		}
		lines = append(lines, icon+
			lipgloss.NewStyle().Foreground(t.Text).Render(humanizeSuggestion(s.Str, s.Arg, s.Section)))
	}
	body := t.Title().Render(" Health & suggestions ") + "\n" + strings.Join(lines, "\n")
	return t.Card(false).Width(width - 2).Render(body)
}

func volumeDisksCard(t tui.Theme, width int, vol dsm.Volume, pools []dsm.StoragePool, disks []dsm.Disk) string {
	// Match disks to this volume's pool by num_id where we can; otherwise
	// list every internal disk so the user at least sees them.
	var contributingIDs []string
	for _, p := range pools {
		if p.NumID == vol.NumID {
			contributingIDs = append(contributingIDs, p.Disks...)
		}
	}
	want := map[string]bool{}
	for _, id := range contributingIDs {
		want[id] = true
	}

	var lines []string
	for _, d := range disks {
		if len(want) > 0 && !want[d.ID] {
			continue
		}
		cap := HumanBytes(ParseSizeString(d.Capacity))
		health := t.HealthStyle(d.Status).Render(" " + d.Status + " ")
		smart := d.Smart.Status
		if smart == "" {
			smart = "—"
		}
		muted := lipgloss.NewStyle().Foreground(t.Muted)
		text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
		bay := trimDev(d.ID)
		if d.Container.Str != "" {
			bay = d.Container.Str
		}
		lines = append(lines, "  "+
			text.Render(padRight(bay, 10))+
			muted.Render(padRight(strings.TrimSpace(d.Vendor+" "+d.Model), 28))+
			text.Render(padRight(cap, 10))+
			muted.Render(padRight(fmt.Sprintf("%d°C", d.Temperature), 8))+
			muted.Render(padRight("SMART "+smart, 16))+
			health)
	}
	if len(lines) == 0 {
		lines = []string{lipgloss.NewStyle().Foreground(t.Muted).Render("  (no contributing disks reported)")}
	}
	title := t.Title().Render(" Contributing disks ")
	return t.Card(false).Width(width - 2).Render(title + "\n" + strings.Join(lines, "\n"))
}

// humanizeSuggestion maps a few of the most common DSM suggestion strings
// to readable English. Everything else is shown as-is so we don't hide
// the underlying signal.
func humanizeSuggestion(key string, args []string, section string) string {
	switch key {
	case "volume_usage_suggestion":
		return "Volume usage is high — consider freeing space or expanding."
	case "fs_almost_full":
		return "Filesystem almost full."
	case "volume_attention":
		return "Volume requires attention."
	default:
		s := key
		if len(args) > 0 {
			s += " (" + strings.Join(args, ", ") + ")"
		}
		if section != "" {
			s = section + ": " + s
		}
		return s
	}
}

// padRight pads s with spaces on the right to width n, clipping if it's
// already longer.
func padRight(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return clipTo(s, n)
	}
	return s + strings.Repeat(" ", n-w)
}

// humanCount renders large integer counts with `, ` thousand separators
// — we use this for inode counts which can otherwise be 60+M.
func humanCount(n uint64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
