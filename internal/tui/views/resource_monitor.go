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

// ResourceMonitor renders historical CPU / memory / network / disk
// utilisation as in-terminal sparklines, with a header window switcher
// (hour / day / week / month / year). It's the "what did the last hour
// look like?" companion to the live Dashboard.
type ResourceMonitor struct {
	ctx Ctx

	window  string // one of: hour, day, week, month, year
	samples []dsm.Utilization
	err     error

	loading  bool
	lastTick time.Time
}

// monWindow describes a selectable history window.
type monWindow struct {
	key   string // hotkey ("1", "2", …)
	id    string // DSM type param ("hour", "day", …)
	label string // header label
}

var monWindows = []monWindow{
	{"1", "hour", "Hour"},
	{"2", "day", "Day"},
	{"3", "week", "Week"},
	{"4", "month", "Month"},
	{"5", "year", "Year"},
}

// NewResourceMonitor constructs the view. Default window is "hour".
func NewResourceMonitor(c Ctx) tui.View {
	return &ResourceMonitor{ctx: c, window: "hour"}
}

func (m *ResourceMonitor) Name() string                   { return "monitor" }
func (m *ResourceMonitor) Title() string                  { return "Resource Monitor" }
func (m *ResourceMonitor) Icon() string                   { return "▤" }
func (m *ResourceMonitor) RefreshInterval() time.Duration { return 30 * time.Second }

func (m *ResourceMonitor) Bindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "next window")),
		key.NewBinding(key.WithKeys("1", "2", "3", "4", "5"), key.WithHelp("1-5", "jump window")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	}
}

func (m *ResourceMonitor) Hint() string {
	return "t window · 1/2/3/4/5 jump · r refresh"
}

// utilHistoryMsg is the result of a UtilizationHistory fetch.
type utilHistoryMsg struct {
	Window  string
	Samples []dsm.Utilization
	Err     error
}

func (m *ResourceMonitor) Init() tea.Cmd { return m.fetch() }

func (m *ResourceMonitor) fetch() tea.Cmd {
	c := m.ctx.Client
	if c == nil {
		return nil
	}
	win := m.window
	m.loading = true
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.Utilization, error) {
			return c.UtilizationHistory(ctx, win)
		},
		func(s []dsm.Utilization, err error) tea.Msg {
			return utilHistoryMsg{Window: win, Samples: s, Err: err}
		},
	)
}

func (m *ResourceMonitor) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch t := msg.(type) {
	case tui.TickMsg:
		m.lastTick = t.At
		return m, m.fetch()
	case utilHistoryMsg:
		// Drop stale responses from a previous window selection.
		if t.Window != m.window {
			return m, nil
		}
		m.samples, m.err = t.Samples, t.Err
		m.loading = false
		return m, nil
	case tea.KeyMsg:
		switch t.String() {
		case "r":
			return m, m.fetch()
		case "t":
			m.window = nextWindow(m.window)
			m.samples = nil
			return m, m.fetch()
		case "1", "2", "3", "4", "5":
			if w := windowForKey(t.String()); w != "" && w != m.window {
				m.window = w
				m.samples = nil
				return m, m.fetch()
			}
		}
	}
	return m, nil
}

func nextWindow(cur string) string {
	for i, w := range monWindows {
		if w.id == cur {
			return monWindows[(i+1)%len(monWindows)].id
		}
	}
	return monWindows[0].id
}

func windowForKey(k string) string {
	for _, w := range monWindows {
		if w.key == k {
			return w.id
		}
	}
	return ""
}

func (m *ResourceMonitor) Render(width, height int) string {
	t := m.ctx.Theme
	header := m.renderHeader(width)

	if m.loading && len(m.samples) == 0 && m.err == nil {
		body := "  " + muted(t, "loading historical samples…")
		return joinRows(header, t.Card(false).Width(width-2).Render(body))
	}
	if m.err != nil {
		body := errLine(t, m.err)
		return joinRows(header, t.Card(false).Width(width-2).Render(body))
	}
	if len(m.samples) == 0 {
		body := "  " + muted(t, "no samples returned for this window")
		return joinRows(header, t.Card(false).Width(width-2).Render(body))
	}

	// Extract the four series from the sample list.
	cpu, mem, rx, tx, disk := m.extractSeries()

	// Layout: 4 sparkline cards stacked. We size each to roughly
	// equal height inside the remaining body region.
	headerH := lipgloss.Height(header)
	bodyH := height - headerH
	if bodyH < 8 {
		bodyH = 8
	}
	// Each card has: title row + spacer + sparkline + spacer = 4 lines
	// inside, plus 2 lines border. We pad to fill bodyH.
	const cards = 4
	cardH := bodyH / cards
	if cardH < 4 {
		cardH = 4
	}

	sparkW := width - 6 // inside the card after border + padding
	if sparkW < 16 {
		sparkW = 16
	}

	rxF := int64sToFloat(rx)
	txF := int64sToFloat(tx)
	cpuF := intsToFloat(cpu)
	memF := intsToFloat(mem)
	diskF := intsToFloat(disk)

	// Combined network series: rx + tx total bytes/sec.
	net := make([]float64, len(rxF))
	for i := range rxF {
		net[i] = rxF[i] + txF[i]
	}

	cpuCard := m.metricCard(width, cardH, "CPU",
		formatPctStats(cpuF), cpuF, sparkW)
	memCard := m.metricCard(width, cardH, "Memory",
		formatPctStats(memF), memF, sparkW)
	netCard := m.networkCard(width, cardH, rxF, txF, net, sparkW)
	diskCard := m.metricCard(width, cardH, "Disk I/O",
		formatPctStats(diskF), diskF, sparkW)

	return joinRows(header, cpuCard, memCard, netCard, diskCard)
}

// renderHeader draws the window switcher row inside a card.
func (m *ResourceMonitor) renderHeader(width int) string {
	t := m.ctx.Theme
	title := t.Title().Render(" Resource Monitor ")
	chips := make([]string, 0, len(monWindows))
	activeSt := lipgloss.NewStyle().
		Foreground(t.Bg).
		Background(t.Accent).
		Bold(true).
		Padding(0, 1)
	inactiveSt := lipgloss.NewStyle().
		Foreground(t.Muted).
		Padding(0, 1)
	for _, w := range monWindows {
		label := fmt.Sprintf("%s %s", w.key, w.label)
		if w.id == m.window {
			chips = append(chips, activeSt.Render(label))
		} else {
			chips = append(chips, inactiveSt.Render(label))
		}
	}
	right := strings.Join(chips, " ")
	hint := muted(t, "  t cycle · r refresh")

	leftW := lipgloss.Width(title)
	rightW := lipgloss.Width(right) + lipgloss.Width(hint)
	gapW := width - leftW - rightW - 4
	if gapW < 1 {
		gapW = 1
	}
	row := title + strings.Repeat(" ", gapW) + right + hint
	return t.Card(false).Width(width - 2).Render(row)
}

// metricCard renders one sparkline card. stats is the pre-formatted
// "min / avg / max" string for the row.
func (m *ResourceMonitor) metricCard(width, height int, label, stats string, data []float64, sparkW int) string {
	t := m.ctx.Theme
	title := t.Title().Render(label)
	statsRow := lipgloss.NewStyle().Foreground(t.Muted).Render(stats)
	header := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", statsRow)
	spark := Sparkline(t, sparkW, data)
	axis := axisLabels(t, sparkW, m.window)

	body := header + "\n" + spark + "\n" + axis
	card := t.Card(false).Width(width - 2)
	if height > 0 {
		card = card.Height(height - 1)
	}
	return card.Render(body)
}

// networkCard renders the two-line network sparkline (rx + tx) with a
// shared summary line.
func (m *ResourceMonitor) networkCard(width, height int, rx, tx, total []float64, sparkW int) string {
	t := m.ctx.Theme
	title := t.Title().Render("Network")
	rxLabel := lipgloss.NewStyle().Foreground(t.Success).Bold(true).Render("↓ rx")
	txLabel := lipgloss.NewStyle().Foreground(t.Info).Bold(true).Render("↑ tx")
	// Reserve 5 columns for the label prefix ("↓ rx ").
	innerW := sparkW - 5
	if innerW < 8 {
		innerW = 8
	}
	rxSpark := Sparkline(t, innerW, rx)
	txSpark := Sparkline(t, innerW, tx)

	stats := lipgloss.NewStyle().Foreground(t.Muted).Render(formatRateStats(total))
	axis := axisLabels(t, innerW, m.window)

	header := lipgloss.JoinHorizontal(lipgloss.Top, title, "  ", stats)
	body := header + "\n" +
		rxLabel + " " + rxSpark + "\n" +
		txLabel + " " + txSpark + "\n" +
		strings.Repeat(" ", 5) + axis

	card := t.Card(false).Width(width - 2)
	if height > 0 {
		card = card.Height(height - 1)
	}
	return card.Render(body)
}

// extractSeries walks the sample list and pulls the per-metric
// timeseries used by the charts.
func (m *ResourceMonitor) extractSeries() (cpu, mem []int, rx, tx []int64, disk []int) {
	cpu = make([]int, 0, len(m.samples))
	mem = make([]int, 0, len(m.samples))
	rx = make([]int64, 0, len(m.samples))
	tx = make([]int64, 0, len(m.samples))
	disk = make([]int, 0, len(m.samples))
	for _, s := range m.samples {
		cpu = append(cpu, s.CPU.UserLoad+s.CPU.SystemLoad+s.CPU.OtherLoad)
		mem = append(mem, s.Memory.RealUsage)
		var r, x int64
		for _, n := range s.Network {
			if n.Device == "total" {
				r, x = n.Rx, n.Tx
				break
			}
		}
		rx = append(rx, r)
		tx = append(tx, x)
		disk = append(disk, s.Disk.Total.Util)
	}
	return cpu, mem, rx, tx, disk
}

// formatPctStats renders the "min% / avg% / max%" line used on percent-
// based cards.
func formatPctStats(data []float64) string {
	if len(data) == 0 {
		return ""
	}
	mn, mx := data[0], data[0]
	var sum float64
	for _, v := range data {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		sum += v
	}
	avg := sum / float64(len(data))
	return fmt.Sprintf("min %.0f%% · avg %.0f%% · max %.0f%%", mn, avg, mx)
}

// formatRateStats renders min/avg/max for a bytes/second series.
func formatRateStats(data []float64) string {
	if len(data) == 0 {
		return ""
	}
	mn, mx := data[0], data[0]
	var sum float64
	for _, v := range data {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		sum += v
	}
	avg := sum / float64(len(data))
	return fmt.Sprintf("min %s · avg %s · max %s",
		HumanRate(int64(mn)), HumanRate(int64(avg)), HumanRate(int64(mx)))
}

// axisLabels renders a faint "start … end" timeline beneath a sparkline.
func axisLabels(t tui.Theme, width int, window string) string {
	if width < 8 {
		return ""
	}
	left, right := windowAxisLabels(window)
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	line := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().Foreground(t.Muted).Faint(true).Render(line)
}

// windowAxisLabels returns the (oldest, newest) labels for a window.
func windowAxisLabels(window string) (string, string) {
	switch window {
	case "hour":
		return "-1h", "now"
	case "day":
		return "-24h", "now"
	case "week":
		return "-7d", "now"
	case "month":
		return "-30d", "now"
	case "year":
		return "-1y", "now"
	}
	return "", "now"
}

// joinRows concatenates rendered rows with newlines, dropping empties.
func joinRows(rows ...string) string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r == "" {
			continue
		}
		out = append(out, r)
	}
	return strings.Join(out, "\n")
}

func intsToFloat(s []int) []float64 {
	out := make([]float64, len(s))
	for i, v := range s {
		out[i] = float64(v)
	}
	return out
}

func int64sToFloat(s []int64) []float64 {
	out := make([]float64, len(s))
	for i, v := range s {
		out[i] = float64(v)
	}
	return out
}
