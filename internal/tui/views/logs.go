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

const logsPageSize = 100

// Logs is the paginated system / connection log viewer. `t` toggles the
// log source; `n`/`p` flip pages.
type Logs struct {
	ctx Ctx

	source string // "system" | "connection"
	page   int    // 0-indexed
	total  int

	items []dsm.LogEntry
	err   error

	base   listBase
	detail *dsm.LogEntry
}

// NewLogs constructs the log viewer.
func NewLogs(c Ctx) tui.View { return &Logs{ctx: c, source: "system"} }

func (l *Logs) Name() string                   { return "logs" }
func (l *Logs) Title() string                  { return "Logs" }
func (l *Logs) Icon() string                   { return "≡" }
func (l *Logs) RefreshInterval() time.Duration { return 60 * time.Second }
func (l *Logs) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next page")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "prev page")),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "toggle source")),
	)
}

// IsTextEditing defers global keys while the inline filter owns input.
func (l *Logs) IsTextEditing() bool { return l.base.filter.IsActive() }

func (l *Logs) Init() tea.Cmd { return l.fetch() }

func (l *Logs) fetch() tea.Cmd {
	c := l.ctx.Client
	if c == nil {
		return nil
	}
	source := l.source
	page := l.page
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (logsFetched, error) {
			items, total, err := c.Logs(ctx, dsm.LogQuery{
				Source: source,
				Offset: page * logsPageSize,
				Limit:  logsPageSize,
			})
			return logsFetched{Items: items, Total: total}, err
		},
		func(v logsFetched, err error) tea.Msg { return logsMsgInternal{F: v, Err: err} },
	)
}

type logsFetched struct {
	Items []dsm.LogEntry
	Total int
}
type logsMsgInternal struct {
	F   logsFetched
	Err error
}

func (l *Logs) visible() []dsm.LogEntry {
	if l.base.FilterValue() == "" {
		return l.items
	}
	out := make([]dsm.LogEntry, 0, len(l.items))
	for _, x := range l.items {
		if MatchesAll(l.base.FilterValue(), x.Time, x.Level, x.User, x.IP, x.Event, x.Descr) {
			out = append(out, x)
		}
	}
	return out
}

func (l *Logs) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if l.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			l.detail = nil
		}
		return l, nil
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return l, l.fetch()
	case logsMsgInternal:
		l.items, l.err, l.total = m.F.Items, m.Err, m.F.Total
		l.base.ClampCursor(len(l.visible()))
		return l, nil
	}
	if _, handled := l.base.HandleKey(msg, len(l.visible())); handled {
		return l, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			rows := l.visible()
			if l.base.Cursor() < len(rows) {
				e := rows[l.base.Cursor()]
				l.detail = &e
			}
		case "n":
			if (l.page+1)*logsPageSize < l.total {
				l.page++
				l.base.ResetCursor()
				return l, l.fetch()
			}
		case "p":
			if l.page > 0 {
				l.page--
				l.base.ResetCursor()
				return l, l.fetch()
			}
		case "t":
			if l.source == "system" {
				l.source = "connection"
			} else {
				l.source = "system"
			}
			l.page = 0
			l.items = nil
			l.base.ResetCursor()
			return l, l.fetch()
		case "r":
			return l, l.fetch()
		}
	}
	return l, nil
}

func (l *Logs) Render(width, height int) string {
	t := l.ctx.Theme
	if l.detail != nil {
		return renderLogDetail(t, width, *l.detail)
	}
	rows := l.visible()
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	pageInfo := ""
	if l.total > 0 {
		from := l.page*logsPageSize + 1
		to := l.page*logsPageSize + len(l.items)
		pageInfo = fmt.Sprintf(" · page %d (%d–%d of %d)", l.page+1, from, to, l.total)
	}
	parts := []string{
		sectionHeader(t, width, l.source+" log"+pageInfo, len(rows), l.err),
	}
	if l.items == nil && l.err == nil {
		parts = append(parts, "  "+muted.Render("loading…"))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted.Render("(none)"))
	}
	for i, e := range rows {
		parts = append(parts, l.renderRow(e, i == l.base.Cursor()))
	}
	parts = append(parts, "")
	parts = append(parts, muted.Render(
		"  ↑/↓ move · ⏎ details · / filter · n next · p prev · t toggle source"))
	if f := l.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (l *Logs) Inspect(width, height int) string {
	rows := l.visible()
	if len(rows) == 0 || l.base.Cursor() >= len(rows) {
		return ""
	}
	t := l.ctx.Theme
	e := rows[l.base.Cursor()]
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	level := strings.ToLower(e.Level)
	var levelChip string
	switch level {
	case "err", "error":
		levelChip = t.Chip(t.Error).Render(" error ")
	case "warn", "warning":
		levelChip = t.Chip(t.Warn).Render(" warn ")
	default:
		levelChip = t.Chip(t.Accent2).Render(" info ")
	}
	parts := []string{
		levelChip + "  " + muted.Render(e.Time),
		"",
		text.Render(coalesce(e.Event, "—")),
	}
	if e.Descr != "" {
		parts = append(parts, "", muted.Render(e.Descr))
	}
	if e.User != "" || e.IP != "" {
		parts = append(parts, "", muted.Render("User: ")+text.Render(coalesce(e.User, "—"))+"  "+muted.Render("IP: ")+text.Render(coalesce(e.IP, "—")))
	}
	_ = width
	_ = height
	return strings.Join(parts, "\n")
}

func (l *Logs) renderRow(e dsm.LogEntry, highlight bool) string {
	t := l.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	var icon string
	switch strings.ToLower(e.Level) {
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
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		icon, " ",
		padRight(muted.Render(e.Time), 20), "  ",
		text.Render(clipTo(event, 80)),
	)
}
