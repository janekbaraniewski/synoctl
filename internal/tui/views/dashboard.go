package views

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Dashboard is the live overview: CPU / Memory / Network / Disk I/O
// cards, volume bars, disk strip, top processes, and recent activity.
type Dashboard struct {
	ctx Ctx

	util     *dsm.Utilization
	storage  *dsm.Storage
	procs    []dsm.Process
	logs     []dsm.LogEntry
	utilErr  error
	storeErr error
	procErr  error
	logErr   error

	lastTick time.Time

	cpuHist  []float64
	memHist  []float64
	rxHist   []float64
	txHist   []float64
	diskHist []float64
}

const dashHist = 120

func NewDashboard(c Ctx) tui.View { return &Dashboard{ctx: c} }

func (d *Dashboard) Name() string                   { return "dashboard" }
func (d *Dashboard) Title() string                  { return "Dashboard" }
func (d *Dashboard) Icon() string                   { return "◆" }
func (d *Dashboard) RefreshInterval() time.Duration { return 2 * time.Second }
func (d *Dashboard) Bindings() []key.Binding {
	return []key.Binding{key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh"))}
}

// (utilMsg / storageMsg / procsMsg / recentLogsMsg now live in messages.go)

func (d *Dashboard) Init() tea.Cmd {
	return tea.Batch(d.fetchUtil(), d.fetchStorage(), d.fetchProcs(), d.fetchRecentLogs())
}

func (d *Dashboard) fetchUtil() tea.Cmd {
	c := d.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(5*time.Second,
		func(ctx context.Context) (*dsm.Utilization, error) { return c.Utilization(ctx) },
		func(u *dsm.Utilization, err error) tea.Msg { return utilMsg{U: u, Err: err} },
	)
}

func (d *Dashboard) fetchStorage() tea.Cmd {
	c := d.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.Storage, error) { return c.Storage(ctx) },
		func(s *dsm.Storage, err error) tea.Msg { return storageMsg{S: s, Err: err} },
	)
}

func (d *Dashboard) fetchProcs() tea.Cmd {
	c := d.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(5*time.Second,
		func(ctx context.Context) ([]dsm.Process, error) { return c.Processes(ctx) },
		func(p []dsm.Process, err error) tea.Msg { return procsMsg{P: p, Err: err} },
	)
}

func (d *Dashboard) fetchRecentLogs() tea.Cmd {
	c := d.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(5*time.Second,
		func(ctx context.Context) ([]dsm.LogEntry, error) {
			items, _, err := c.Logs(ctx, dsm.LogQuery{Source: "system", Limit: 30})
			return items, err
		},
		func(l []dsm.LogEntry, err error) tea.Msg { return recentLogsMsg{L: l, Err: err} },
	)
}

func (d *Dashboard) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case tui.TickMsg:
		d.lastTick = m.At
		return d, tea.Batch(d.fetchUtil(), d.fetchStorage(), d.fetchProcs(), d.fetchRecentLogs())
	case utilMsg:
		d.util, d.utilErr = m.U, m.Err
		d.sampleHistory()
		return d, nil
	case storageMsg:
		d.storage, d.storeErr = m.S, m.Err
		return d, nil
	case procsMsg:
		d.procs, d.procErr = m.P, m.Err
		return d, nil
	case recentLogsMsg:
		d.logs, d.logErr = m.L, m.Err
		return d, nil
	case tea.KeyMsg:
		if m.String() == "r" {
			return d, tea.Batch(d.fetchUtil(), d.fetchStorage(), d.fetchProcs(), d.fetchRecentLogs())
		}
	}
	return d, nil
}

func (d *Dashboard) sampleHistory() {
	if d.util == nil {
		return
	}
	cpu := float64(d.util.CPU.UserLoad + d.util.CPU.SystemLoad + d.util.CPU.OtherLoad)
	d.cpuHist = pushHistory(d.cpuHist, cpu)
	d.memHist = pushHistory(d.memHist, float64(d.util.Memory.RealUsage))
	var rx, tx int64
	for _, n := range d.util.Network {
		if n.Device == "total" {
			rx, tx = n.Rx, n.Tx
			break
		}
	}
	d.rxHist = pushHistory(d.rxHist, float64(rx))
	d.txHist = pushHistory(d.txHist, float64(tx))
	d.diskHist = pushHistory(d.diskHist, float64(d.util.Disk.Total.Util))
}

func pushHistory(buf []float64, v float64) []float64 {
	buf = append(buf, v)
	if len(buf) > dashHist {
		buf = buf[len(buf)-dashHist:]
	}
	return buf
}

func (d *Dashboard) Render(width, height int) string {
	t := d.ctx.Theme
	if d.util == nil && d.utilErr == nil {
		return Card(t, width, " ◆  Loading dashboard… ", "\n  fetching first sample…\n", true)
	}

	// Fixed-height sections; bottom row gets the remainder.
	const metricsH = 7
	volumesH := d.volumesHeight()
	disksH := 4
	bottomH := height - metricsH - volumesH - disksH
	if bottomH < 6 {
		bottomH = 6
	}

	rows := []string{
		d.renderMetricsRow(width),
		d.renderVolumes(width, volumesH),
		d.renderDisks(width, disksH),
		d.renderBottomRow(width, bottomH),
	}
	_ = t
	return strings.Join(rows, "\n")
}

func (d *Dashboard) volumesHeight() int {
	rows := 1
	if d.storage != nil && len(d.storage.Volumes) > 0 {
		rows = len(d.storage.Volumes)
	}
	return rows + 3 // border + title + spacing
}

// ────────────────────────── metrics row ──────────────────────────

func (d *Dashboard) renderMetricsRow(width int) string {
	colW := (width - 3) / 4
	if colW < 18 {
		colW = (width - 1) / 2
	}
	var cpu, mem int
	var memUsedKB, memTotalKB int
	var rx, tx int64
	var diskUtil int
	if d.util != nil {
		cpu = d.util.CPU.UserLoad + d.util.CPU.SystemLoad + d.util.CPU.OtherLoad
		mem = d.util.Memory.RealUsage
		memTotalKB = d.util.Memory.TotalReal
		memUsedKB = memTotalKB - d.util.Memory.AvailReal
		for _, n := range d.util.Network {
			if n.Device == "total" {
				rx, tx = n.Rx, n.Tx
			}
		}
		diskUtil = d.util.Disk.Total.Util
	}
	return lipgloss.JoinHorizontal(lipgloss.Top,
		d.metricCard(colW, "CPU", cpu, d.cpuHist, ""),
		" ",
		d.metricCard(colW, "Memory", mem, d.memHist,
			fmt.Sprintf("%s · %s", HumanBytes(uint64(memUsedKB)*1024), HumanBytes(uint64(memTotalKB)*1024))),
		" ",
		d.networkMetricCard(colW, rx, tx),
		" ",
		d.metricCard(colW, "Disk I/O", diskUtil, d.diskHist, ""),
	)
}

// metricCard renders a card with: title row, big percentage, gauge bar
// below, sparkline below, optional subtitle. Every line is independent
// (no inline %s padding that ANSI breaks).
func (d *Dashboard) metricCard(width int, label string, pct int, hist []float64, sub string) string {
	t := d.ctx.Theme
	innerW := width - 4
	if innerW < 8 {
		innerW = 8
	}
	title := t.Title().Render(label)
	big := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(fmt.Sprintf("%d%%", pct))
	bar := Gauge(t, innerW, float64(pct)/100)
	spark := Sparkline(t, innerW, hist)
	parts := []string{title, big, bar, spark}
	if sub != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(sub))
	}
	return t.Card(false).Width(width - 2).Render(strings.Join(parts, "\n"))
}

func (d *Dashboard) networkMetricCard(width int, rx, tx int64) string {
	t := d.ctx.Theme
	innerW := width - 4
	if innerW < 8 {
		innerW = 8
	}
	title := t.Title().Render("Network")
	rxLabel := lipgloss.NewStyle().Foreground(t.Success).Bold(true).Render("↓ " + HumanRate(rx))
	txLabel := lipgloss.NewStyle().Foreground(t.Info).Bold(true).Render("↑ " + HumanRate(tx))
	rxSpark := Sparkline(t, innerW, d.rxHist)
	txSpark := Sparkline(t, innerW, d.txHist)
	body := title + "\n" + rxLabel + "\n" + rxSpark + "\n" + txLabel + "\n" + txSpark
	return t.Card(false).Width(width - 2).Render(body)
}

// ────────────────────────── volumes row ──────────────────────────

func (d *Dashboard) renderVolumes(width, height int) string {
	t := d.ctx.Theme
	title := t.Title().Render(" Volumes ")
	if d.storage == nil {
		body := title + "\n  " + muted(t, "loading…")
		if d.storeErr != nil {
			body = title + "\n" + errLine(t, d.storeErr)
		}
		return t.Card(false).Width(width - 2).Render(body)
	}
	if len(d.storage.Volumes) == 0 {
		return t.Card(false).Width(width - 2).Render(title + "\n  " + muted(t, "no volumes reported"))
	}
	// Reserve room for: 2 chars left margin · name (12) · bar · 1 space ·
	// pct (6) · 3 chars · used/total (≈24) · 3 chars · status chip (12).
	barWidth := width - (2 + 12 + 1 + 6 + 3 + 24 + 3 + 12) - 4 // card chrome
	if barWidth < 16 {
		barWidth = 16
	}
	var lines []string
	for _, v := range d.storage.Volumes {
		total := ParseSizeString(v.Size.Total)
		used := ParseSizeString(v.Size.Used)
		ratio := 0.0
		if total > 0 {
			ratio = float64(used) / float64(total)
		}
		name := v.VolPath
		if name == "" {
			name = v.ID
		}
		text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
		muted := lipgloss.NewStyle().Foreground(t.Muted)
		status := t.HealthStyle(v.Status).Render(v.Status)
		line := lipgloss.JoinHorizontal(lipgloss.Center,
			padRight(text.Render(name), 12), " ",
			Gauge(t, barWidth, ratio), " ",
			padLeft(text.Render(fmt.Sprintf("%5.1f%%", ratio*100)), 6),
			"   ",
			padLeft(muted.Render(fmt.Sprintf("%s / %s", HumanBytes(used), HumanBytes(total))), 22),
			"   ",
			status,
		)
		lines = append(lines, "  "+line)
	}
	body := title + "\n" + strings.Join(lines, "\n")
	_ = height
	return t.Card(false).Width(width - 2).Render(body)
}

// ────────────────────────── disks row ──────────────────────────

func (d *Dashboard) renderDisks(width, height int) string {
	t := d.ctx.Theme
	title := t.Title().Render(" Disks ")
	if d.storage == nil {
		return t.Card(false).Width(width - 2).Render(title + "\n  " + muted(t, "loading…"))
	}
	if len(d.storage.Disks) == 0 {
		return t.Card(false).Width(width - 2).Render(title + "\n  " + muted(t, "no disks reported"))
	}
	var chips []string
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	for _, dk := range d.storage.Disks {
		bay := trimDev(dk.ID)
		health := t.HealthStyle(dk.Status).Render(dk.Status)
		temp := lipgloss.NewStyle().Foreground(tempColor(t, dk.Temperature)).Bold(true).Render(
			fmt.Sprintf("%d°C", dk.Temperature))
		chip := text.Render(bay) + " " +
			muted.Render(strings.TrimSpace(dk.Vendor+" "+dk.Model)) + "  " +
			temp + "  " + health
		chips = append(chips, chip)
	}
	body := title + "\n  " + strings.Join(chips, "      ")
	_ = height
	return t.Card(false).Width(width - 2).Render(body)
}

// ────────────────────────── bottom row ──────────────────────────

func (d *Dashboard) renderBottomRow(width, height int) string {
	colW := (width - 1) / 2
	left := d.renderTopProcesses(colW, height)
	right := d.renderRecentAlerts(width-colW-1, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)
}

func (d *Dashboard) renderTopProcesses(width, height int) string {
	t := d.ctx.Theme
	title := t.Title().Render(" Top processes (by CPU) ")
	if d.procErr != nil {
		return t.Card(false).Width(width - 2).Height(height).Render(title + "\n" + errLine(t, d.procErr))
	}
	if len(d.procs) == 0 {
		return t.Card(false).Width(width - 2).Height(height).Render(title + "\n  " + muted(t, "loading…"))
	}
	sorted := make([]dsm.Process, len(d.procs))
	copy(sorted, d.procs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].CPU > sorted[j].CPU })
	maxRows := height - 4
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > len(sorted) {
		maxRows = len(sorted)
	}
	maxCPU := 1
	for _, p := range sorted[:maxRows] {
		if p.CPU > maxCPU {
			maxCPU = p.CPU
		}
	}
	// Layout (all lipgloss-aware so ANSI doesn't break alignment):
	//   PID(7)  COMMAND(flex)  BAR(10)  MEM(right, 10)
	innerW := width - 4
	pidW, barW, memW := 7, 10, 12
	cmdW := innerW - pidW - barW - memW - 6 // four single-space separators
	if cmdW < 16 {
		cmdW = 16
	}
	mutedSt := lipgloss.NewStyle().Foreground(t.Muted)
	textSt := lipgloss.NewStyle().Foreground(t.Text)
	accentSt := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	header := lipgloss.JoinHorizontal(lipgloss.Top,
		padRight(mutedSt.Render("PID"), pidW), "  ",
		padRight(mutedSt.Render("COMMAND"), cmdW), "  ",
		padRight(mutedSt.Render("CPU"), barW), "  ",
		padLeft(mutedSt.Render("MEM"), memW),
	)
	rows := []string{header}
	for _, p := range sorted[:maxRows] {
		bar := Gauge(t, barW, float64(p.CPU)/float64(maxCPU))
		line := lipgloss.JoinHorizontal(lipgloss.Top,
			padRight(textSt.Render(fmt.Sprintf("%d", p.PID)), pidW), "  ",
			padRight(textSt.Render(clipTo(p.Command, cmdW)), cmdW), "  ",
			bar, "  ",
			padLeft(accentSt.Render(HumanBytes(uint64(p.Mem)*1024)), memW),
		)
		rows = append(rows, line)
	}
	body := title + "\n" + strings.Join(rows, "\n")
	return t.Card(false).Width(width - 2).Height(height).Render(body)
}

func (d *Dashboard) renderRecentAlerts(width, height int) string {
	t := d.ctx.Theme
	title := t.Title().Render(" Recent activity ")
	if d.logErr != nil {
		return t.Card(false).Width(width - 2).Height(height).Render(title + "\n" + errLine(t, d.logErr))
	}
	if len(d.logs) == 0 {
		return t.Card(false).Width(width - 2).Height(height).Render(title + "\n  " + muted(t, "loading…"))
	}
	innerW := width - 4
	timeW := 18
	iconW := 2
	eventW := innerW - timeW - iconW - 4
	if eventW < 10 {
		eventW = 10
	}
	mutedSt := lipgloss.NewStyle().Foreground(t.Muted)
	textSt := lipgloss.NewStyle().Foreground(t.Text)

	maxRows := height - 3
	if maxRows < 1 {
		maxRows = 1
	}
	if maxRows > len(d.logs) {
		maxRows = len(d.logs)
	}
	var rows []string
	for _, e := range d.logs[:maxRows] {
		level := strings.ToLower(e.Level)
		var icon string
		switch level {
		case "err", "error":
			icon = lipgloss.NewStyle().Foreground(t.Error).Bold(true).Render("✗")
		case "warn", "warning":
			icon = lipgloss.NewStyle().Foreground(t.Warn).Bold(true).Render("⚠")
		default:
			icon = lipgloss.NewStyle().Foreground(t.Info).Render("•")
		}
		event := e.Event
		if e.Descr != "" {
			event = e.Descr
		}
		line := lipgloss.JoinHorizontal(lipgloss.Top,
			padRight(icon, iconW), " ",
			padRight(mutedSt.Render(clipTo(e.Time, timeW)), timeW), "  ",
			textSt.Render(clipTo(event, eventW)),
		)
		rows = append(rows, line)
	}
	body := title + "\n" + strings.Join(rows, "\n")
	return t.Card(false).Width(width - 2).Height(height).Render(body)
}

// ────────────────────────── helpers ──────────────────────────

func muted(t tui.Theme, s string) string {
	return lipgloss.NewStyle().Foreground(t.Muted).Render(s)
}

func padLeft(s string, n int) string {
	w := lipgloss.Width(s)
	if w >= n {
		return clipTo(s, n)
	}
	return strings.Repeat(" ", n-w) + s
}

func tempColor(t tui.Theme, c int) lipgloss.AdaptiveColor {
	switch {
	case c >= 50:
		return t.Error
	case c >= 40:
		return t.Warn
	default:
		return t.Success
	}
}

func trimDev(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
