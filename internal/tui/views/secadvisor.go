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

// SecurityAdvisorView surfaces Security Advisor checklist + per-severity
// summary card. Severity badges are coloured against the canonical
// semantic slots (Error / Warn / Info) rather than hex.

type secReportMsg struct {
	R   *dsm.SecAdvisorReport
	Err error
}
type secItemsMsg struct {
	I   []dsm.SecAdvisorItem
	Err error
}

type SecurityAdvisorView struct {
	ctx Ctx

	report    *dsm.SecAdvisorReport
	items     []dsm.SecAdvisorItem
	reportErr error
	itemsErr  error

	cursor int
	filter Filter
	loaded bool

	detail *dsm.SecAdvisorItem
}

func NewSecurityAdvisor(c Ctx) tui.View { return &SecurityAdvisorView{ctx: c} }

func (v *SecurityAdvisorView) Name() string                   { return "security" }
func (v *SecurityAdvisorView) Title() string                  { return "Security" }
func (v *SecurityAdvisorView) Icon() string                   { return "⚐" }
func (v *SecurityAdvisorView) RefreshInterval() time.Duration { return 5 * time.Minute }
func (v *SecurityAdvisorView) Bindings() []key.Binding        { return BaseBindings() }
func (v *SecurityAdvisorView) IsTextEditing() bool            { return v.filter.IsActive() }

func (v *SecurityAdvisorView) Init() tea.Cmd {
	return tea.Batch(v.fetchReport(), v.fetchItems())
}

func (v *SecurityAdvisorView) fetchReport() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (*dsm.SecAdvisorReport, error) { return c.SecurityAdvisorReport(ctx) },
		func(r *dsm.SecAdvisorReport, err error) tea.Msg { return secReportMsg{R: r, Err: err} },
	)
}
func (v *SecurityAdvisorView) fetchItems() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.SecAdvisorItem, error) { return c.SecurityAdvisorItems(ctx) },
		func(i []dsm.SecAdvisorItem, err error) tea.Msg { return secItemsMsg{I: i, Err: err} },
	)
}

func (v *SecurityAdvisorView) filtered() []dsm.SecAdvisorItem {
	if v.filter.Value() == "" {
		return v.items
	}
	out := make([]dsm.SecAdvisorItem, 0, len(v.items))
	for _, it := range v.items {
		if MatchesAll(v.filter.Value(), it.ID, it.Title, it.Description, it.Category, it.Severity, it.Status, it.Result) {
			out = append(out, it)
		}
	}
	return out
}

func (v *SecurityAdvisorView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
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
		return v, tea.Batch(v.fetchReport(), v.fetchItems())
	case secReportMsg:
		v.report, v.reportErr = m.R, m.Err
		v.loaded = true
	case secItemsMsg:
		v.items, v.itemsErr = m.I, m.Err
		v.loaded = true
		v.clampCursor()
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
			return v, tea.Batch(v.fetchReport(), v.fetchItems())
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				it := rows[v.cursor]
				v.detail = &it
			}
		}
	}
	return v, nil
}

func (v *SecurityAdvisorView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *SecurityAdvisorView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detail != nil {
		return renderSecItemDetail(t, width, *v.detail)
	}

	if v.loaded && v.report == nil && len(v.items) == 0 && v.reportErr == nil && v.itemsErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⚐  Security Advisor",
			"Security Advisor is not advertised on this DSM build.",
			"Run a scan from DSM's Security Advisor app to populate the checklist."), height)
	}

	items := v.filtered()
	var parts []string
	parts = append(parts, v.renderSummary(width))
	parts = append(parts, "")
	parts = append(parts, sectionHeader(t, width, "Checklist", len(items), v.itemsErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(items) == 0 {
		parts = append(parts, "  "+muted(t, "(no items)"))
	}
	for i, it := range items {
		parts = append(parts, v.renderRow(it, i == v.cursor))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · esc clear · r refresh"))
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func severityStyle(t tui.Theme, sev string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(sev)) {
	case "critical", "crit":
		return lipgloss.NewStyle().Foreground(t.Error).Bold(true)
	case "risk", "danger", "high":
		return lipgloss.NewStyle().Foreground(t.Error).Bold(true)
	case "warning", "warn", "medium":
		return lipgloss.NewStyle().Foreground(t.Warn).Bold(true)
	case "info", "low":
		return lipgloss.NewStyle().Foreground(t.Info)
	case "safe", "ok":
		return lipgloss.NewStyle().Foreground(t.Success).Bold(true)
	default:
		return lipgloss.NewStyle().Foreground(t.Muted)
	}
}

func severityKey(it dsm.SecAdvisorItem) string {
	if it.Severity != "" {
		return it.Severity
	}
	return it.Status
}

func (v *SecurityAdvisorView) renderSummary(width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	if v.report == nil && len(v.items) == 0 {
		return t.Card(false).Width(width - 2).Render(
			t.Title().Render(" Security Advisor ") + "\n  " + muted(t, "loading…"))
	}
	// Prefer counts from the report. Fall back to recomputing from items.
	crit, risk, warn, info, safe := 0, 0, 0, 0, 0
	if v.report != nil {
		crit = v.report.CritCount
		risk = v.report.RiskCount
		warn = v.report.WarnCount
		info = v.report.InfoCount
		safe = v.report.SafeCount
	}
	if crit+risk+warn+info+safe == 0 {
		for _, it := range v.items {
			switch strings.ToLower(severityKey(it)) {
			case "critical", "crit":
				crit++
			case "risk", "danger", "high":
				risk++
			case "warning", "warn", "medium":
				warn++
			case "info", "low":
				info++
			case "safe", "ok":
				safe++
			}
		}
	}
	pill := func(label string, n int, sev string) string {
		st := severityStyle(t, sev)
		return st.Render(fmt.Sprintf(" %s %d ", label, n))
	}
	row1 := strings.Join([]string{
		pill("CRIT", crit, "critical"),
		pill("RISK", risk, "risk"),
		pill("WARN", warn, "warning"),
		pill("INFO", info, "info"),
		pill("SAFE", safe, "safe"),
	}, "  ")
	lastScan := "—"
	baseline := "—"
	if v.report != nil {
		baseline = v.report.Baseline
		if v.report.LastScanned > 0 {
			lastScan = time.Unix(v.report.LastScanned, 0).Format("2006-01-02 15:04")
		}
	}
	row2 := strings.Join([]string{
		mu.Render("Last scan:") + " " + lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(lastScan),
		mu.Render("Baseline:") + " " + lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(baseline),
	}, "   ")
	body := t.Title().Render(" Security Advisor ") + "\n" + row1 + "\n" + row2
	return t.Card(false).Width(width - 2).Render(body)
}

func (v *SecurityAdvisorView) renderRow(it dsm.SecAdvisorItem, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	sev := severityKey(it)
	if sev == "" {
		sev = "info"
	}
	badge := severityStyle(t, sev).Render(padRight(strings.ToUpper(sev), 8))
	scanned := "—"
	if it.LastScanned > 0 {
		scanned = time.Unix(it.LastScanned, 0).Format("2006-01-02 15:04")
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		badge, " ",
		padRight(text.Render(clipTo(it.Title, 40)), 40), " ",
		padRight(mu.Render(it.Category), 18), " ",
		mu.Render(scanned),
	)
}

func renderSecItemDetail(t tui.Theme, width int, it dsm.SecAdvisorItem) string {
	if width < 60 {
		width = 60
	}
	sev := severityKey(it)
	if sev == "" {
		sev = "info"
	}
	scanned := "—"
	if it.LastScanned > 0 {
		scanned = time.Unix(it.LastScanned, 0).Format("2006-01-02 15:04")
	}
	parts := []string{
		hero(t, width, "⚐", coalesce(it.Title, it.ID), sev, it.Category),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", it.ID},
			{"Title", it.Title},
			{"Category", it.Category},
			{"Severity", sev},
			{"Status", it.Status},
			{"Result", it.Result},
			{"Last scanned", scanned},
		}),
	}
	if it.Description != "" {
		body := t.Title().Render(" Description ") + "\n" +
			wrap(lipgloss.NewStyle().Foreground(t.Text), it.Description, width-6)
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	if it.Suggestion != "" {
		body := t.Title().Render(" Suggestion ") + "\n" +
			wrap(lipgloss.NewStyle().Foreground(t.Text), it.Suggestion, width-6)
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back · running scans isn't wired up yet"))
	return strings.Join(parts, "\n")
}
