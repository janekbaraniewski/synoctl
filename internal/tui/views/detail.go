package views

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Filter is a tiny on-line text input used by list views for `/`.
//
// Each list view embeds one via listBase. The filter is just a
// substring matcher — case-insensitive — applied to whatever cell
// strings the view passes to FilterMatch.
type Filter struct {
	active bool
	value  string
}

// IsActive reports whether the user is currently typing in the filter.
func (f *Filter) IsActive() bool { return f.active }

// Value returns the current filter text.
func (f *Filter) Value() string { return f.value }

// Open starts an editing session.
func (f *Filter) Open() { f.active = true }

// Close commits the filter (keeps the value but stops typing).
func (f *Filter) Close() { f.active = false }

// Clear empties the filter.
func (f *Filter) Clear() { f.active = false; f.value = "" }

// Update applies a key event to the filter editor; returns true if the
// event was consumed.
func (f *Filter) Update(msg tea.Msg) bool {
	if !f.active {
		return false
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	switch km.Type {
	case tea.KeyEnter:
		f.active = false
		return true
	case tea.KeyEsc:
		f.active = false
		f.value = ""
		return true
	case tea.KeyBackspace:
		if len(f.value) > 0 {
			f.value = f.value[:len(f.value)-1]
		}
		return true
	case tea.KeyRunes, tea.KeySpace:
		f.value += string(km.Runes)
		return true
	}
	return false
}

// Render returns the inline prompt to draw in the card footer; empty
// when no filter is in play.
func (f Filter) Render(theme tui.Theme) string {
	if !f.active && f.value == "" {
		return ""
	}
	prompt := lipgloss.NewStyle().Foreground(theme.Accent).Bold(true).Render(" / ")
	val := f.value
	if f.active {
		val += "▎"
	}
	return prompt + lipgloss.NewStyle().Foreground(theme.Text).Render(val)
}

// MatchesAll returns true when any haystack cell contains the needle
// (case-insensitive substring). When needle is empty it always returns
// true — making it safe to call unconditionally inside a view's
// `visible()` filter.
func MatchesAll(needle string, cells ...string) bool {
	if needle == "" {
		return true
	}
	n := strings.ToLower(needle)
	for _, c := range cells {
		if strings.Contains(strings.ToLower(c), n) {
			return true
		}
	}
	return false
}
