package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// renderDiskDetail draws the drill-down screen for a single disk: a hero
// row, a temperature gauge with thermal banding, a SMART panel, the bay
// info, and a properties grid covering everything else DSM reports.
func renderDiskDetail(t tui.Theme, width, height int, d dsm.Disk, pools []dsm.StoragePool) string {
	_ = height
	if width < 60 {
		width = 60
	}
	cap := ParseSizeString(d.Capacity)
	used := ParseSizeString(d.Used)
	ratio := 0.0
	if cap > 0 {
		ratio = float64(used) / float64(cap)
	}

	parts := []string{
		hero(t, width, "●", strings.TrimSpace(d.Vendor+" "+d.Model), d.Status,
			strings.ToUpper(d.DiskType)+" · "+HumanBytes(cap)),
		tempCard(t, width, d.Temperature),
	}
	if cap > 0 && used > 0 {
		parts = append(parts, gaugeCard(t, width, " Capacity used ",
			fmt.Sprintf("%5.1f%%", ratio*100), ratio,
			fmt.Sprintf("%s used · %s free · %s total", HumanBytes(used), HumanBytes(cap-used), HumanBytes(cap))))
	}
	smart := d.Smart.Status
	if smart == "" {
		smart = "not reported"
	}
	parts = append(parts, chipsCard(t, width, " SMART ",
		[]string{t.HealthStyle(smart).Render(" " + smart + " ")}))

	bay := trimDev(d.ID)
	if d.Container.Str != "" {
		bay = d.Container.Str + " (" + trimDev(d.ID) + ")"
	}
	parts = append(parts, propsCard(t, width, " Properties ", [][2]string{
		{"Bay", bay},
		{"Device path", d.Path},
		{"Device node", d.Device},
		{"Type", d.DiskType},
		{"Vendor", d.Vendor},
		{"Model", d.Model},
		{"Firmware", d.Firmware},
		{"Serial", d.Serial},
		{"Capacity", HumanBytes(cap)},
		{"Used", HumanBytes(used)},
		{"Container", d.Container.Type},
		{"Bay slot", fmt.Sprintf("%d", d.Container.Order)},
	}))

	// Pools this disk participates in.
	contributes := []string{}
	for _, p := range pools {
		for _, id := range p.Disks {
			if id == d.ID {
				contributes = append(contributes, fmt.Sprintf("%s (%s, %s)", p.ID, p.RaidType, p.Status))
			}
		}
	}
	if len(contributes) > 0 {
		body := t.Title().Render(" Member of pools ") + "\n  " +
			lipgloss.NewStyle().Foreground(t.Text).Render(strings.Join(contributes, "\n  "))
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}

	parts = append(parts, noteCard(t, width,
		"  esc back · ↑/↓ scroll · disk actions are intentionally not wired"))
	return strings.Join(parts, "\n")
}

// tempCard renders a thermometer-style gauge for a disk temperature with
// colour banding: green ≤39 °C, yellow 40-49, red ≥50.
func tempCard(t tui.Theme, width, c int) string {
	innerW := width - 6
	barW := innerW - 14
	if barW < 20 {
		barW = 20
	}
	// Map 20–60 °C onto the bar; clamp.
	const lo, hi = 20, 60
	ratio := float64(c-lo) / float64(hi-lo)
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	bar := Gauge(t, barW, ratio)
	big := lipgloss.NewStyle().Foreground(tempColor(t, c)).Bold(true).
		Render(fmt.Sprintf("%d°C", c))
	sub := lipgloss.NewStyle().Foreground(t.Muted).Render("  green ≤39 · yellow 40-49 · red ≥50")
	body := t.Title().Render(" Temperature ") + "\n" + bar + "   " + big + "\n" + sub
	return t.Card(false).Width(width - 2).Render(body)
}
