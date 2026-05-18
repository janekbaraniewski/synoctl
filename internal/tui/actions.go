package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Action is a single entry in the contextual action menu. Views that want
// to expose row-level actions implement the Actor interface; the shell
// pops a menu when the user presses `a`.
type Action struct {
	Key   string // single-character key, e.g. "s" — shown as a chip
	Label string // human label, e.g. "Start package"
	Token string // opaque token returned via ActionInvokedMsg
}

// Actor is the optional interface a View can implement to surface row-level
// actions through the global action menu. Views that don't implement it
// simply have an empty menu when the user presses `a`.
type Actor interface {
	Actions() []Action
}

// ActionInvokedMsg is delivered to the active view when the user picks an
// entry from the action menu. The view interprets Token (set at Actions()
// time) however it likes.
type ActionInvokedMsg struct {
	Token string
}

// actionMenu is the shell-owned modal that picks an Action for the active
// view. It is intentionally tiny — the menu is keyed-selection only (no
// arrow-key cursor) because every entry has a unique shortcut.
type actionMenu struct {
	open    bool
	theme   Theme
	width   int
	height  int
	actions []Action
	title   string
}

func newActionMenu(theme Theme) *actionMenu { return &actionMenu{theme: theme} }

func (m *actionMenu) Open(title string, actions []Action) {
	m.title = title
	m.actions = actions
	m.open = len(actions) > 0
}

func (m *actionMenu) Close() { m.open = false }

func (m *actionMenu) IsOpen() bool { return m.open }

// Update consumes key events while the menu is open. The returned tea.Cmd
// carries an ActionInvokedMsg when the user picks an entry, or nil if they
// closed the menu without choosing.
func (m *actionMenu) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !m.open {
		return false, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if key.Matches(km, key.NewBinding(key.WithKeys("esc", "a", "q"))) {
		m.open = false
		return true, nil
	}
	s := km.String()
	for _, a := range m.actions {
		if a.Key == s {
			tok := a.Token
			m.open = false
			return true, func() tea.Msg { return ActionInvokedMsg{Token: tok} }
		}
	}
	// Swallow stray keys while the menu is up so views don't receive them.
	return true, nil
}

// Render draws the menu as a centered card.
func (m *actionMenu) Render(width, height int) string {
	if !m.open {
		return ""
	}
	t := m.theme
	titleStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	keyChip := func(s string) string { return t.Chip(t.Accent2).Render(" " + s + " ") }

	var lines []string
	lines = append(lines, titleStyle.Render(m.title))
	lines = append(lines, "")
	for _, a := range m.actions {
		lines = append(lines, keyChip(a.Key)+"  "+text.Render(a.Label))
	}
	lines = append(lines, "")
	lines = append(lines, muted.Render("esc · close"))

	w := 44
	if width-12 < w {
		w = width - 12
	}
	if w < 24 {
		w = 24
	}
	card := t.Card(true).Width(w).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceForeground(t.Faint))
}
