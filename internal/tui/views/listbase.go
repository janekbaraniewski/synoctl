package views

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// listBase is embedded by every table-driven view. It centralises the
// keyboard handling that's identical everywhere (cursor movement, `/`
// filter) so each view only writes the truly view-specific code: which
// data it fetches and how rows are rendered.
//
// Each view manages its own structured "detail" overlay — Volumes,
// Disks, Shares, Users, Packages, Services, Network, Logs, Files all
// have purpose-built detail screens. We do NOT have a generic
// JSON-inspector fallback: every entity has a curated view.
type listBase struct {
	cursor int
	filter Filter
}

// initBase is a no-op today; kept so views have a hook for future
// shared initialisation (theme-bound widgets, etc.).
func (b *listBase) initBase(_ Ctx) {}

// BaseBindings returns the help-overlay bindings that are always
// available in a list view.
func BaseBindings() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "details")),
		key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter / close details")),
	}
}

// HandleKey processes the generic list keys. The caller passes the
// number of rows currently visible (after its own filtering). Returns
// true when the message was consumed so the caller can skip its own
// handling.
func (b *listBase) HandleKey(msg tea.Msg, rowCount int) (tea.Cmd, bool) {
	// Filter editing swallows runes when open.
	if b.filter.IsActive() {
		if b.filter.Update(msg) {
			return nil, true
		}
		return nil, false
	}

	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil, false
	}
	switch km.String() {
	case "j", "down":
		if b.cursor < rowCount-1 {
			b.cursor++
		}
		return nil, true
	case "k", "up":
		if b.cursor > 0 {
			b.cursor--
		}
		return nil, true
	case "g":
		b.cursor = 0
		return nil, true
	case "G":
		b.cursor = max(rowCount-1, 0)
		return nil, true
	case "/":
		b.filter.Open()
		return nil, true
	case "esc":
		if b.filter.Value() != "" {
			b.filter.Clear()
			b.cursor = 0
			return nil, true
		}
	}
	return nil, false
}

// IsEnter reports whether the message is the Enter key (used by
// callers to open the structured detail view with their own payload).
func (b *listBase) IsEnter(msg tea.Msg) bool {
	if b.filter.IsActive() {
		return false
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		return km.Type == tea.KeyEnter
	}
	return false
}

// Cursor returns the current cursor index.
func (b *listBase) Cursor() int { return b.cursor }

// ResetCursor jumps back to row 0.
func (b *listBase) ResetCursor() { b.cursor = 0 }

// ClampCursor keeps the cursor in range after the underlying row count
// changes (e.g. after a refresh).
func (b *listBase) ClampCursor(rowCount int) {
	if b.cursor >= rowCount {
		b.cursor = rowCount - 1
	}
	if b.cursor < 0 {
		b.cursor = 0
	}
}

// FilterValue exposes the current filter substring.
func (b *listBase) FilterValue() string { return b.filter.Value() }

// FilterMatch is sugar for MatchesAll(filter.Value(), …).
func (b *listBase) FilterMatch(cells ...string) bool {
	return MatchesAll(b.filter.Value(), cells...)
}

// FilterFooter renders the inline `/value` prompt for inclusion in a
// card footer. Returns "" when no filter is in play.
func (b *listBase) FilterFooter(theme tui.Theme) string {
	v := b.filter.Render(theme)
	if v == "" {
		return ""
	}
	return " " + v
}
