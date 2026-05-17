package views

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Confirm is a yes/no modal. The token lets one view host several
// distinct confirmations.
type Confirm struct {
	theme   tui.Theme
	open    bool
	title   string
	message string
	token   string
	yesKey  key.Binding
	noKey   key.Binding
}

// ConfirmedMsg is delivered when the user accepts the prompt.
type ConfirmedMsg struct{ Token string }

// CancelledMsg is delivered when the user backs out of a confirmation.
type CancelledMsg struct{ Token string }

// NewConfirm constructs an idle modal.
func NewConfirm(theme tui.Theme) *Confirm {
	return &Confirm{
		theme:  theme,
		yesKey: key.NewBinding(key.WithKeys("y", "enter"), key.WithHelp("y / ⏎", "confirm")),
		noKey:  key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n / esc", "cancel")),
	}
}

// Ask opens the modal with a title and message tied to a token.
func (c *Confirm) Ask(token, title, message string) {
	c.open = true
	c.title = title
	c.message = message
	c.token = token
}

// Open reports whether the modal currently owns input.
func (c *Confirm) Open() bool { return c.open }

// Update routes keys while open. The returned tea.Cmd carries either a
// ConfirmedMsg or CancelledMsg.
func (c *Confirm) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !c.open {
		return false, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return false, nil
	}
	if key.Matches(km, c.yesKey) {
		tok := c.token
		c.open = false
		return true, func() tea.Msg { return ConfirmedMsg{Token: tok} }
	}
	if key.Matches(km, c.noKey) {
		tok := c.token
		c.open = false
		return true, func() tea.Msg { return CancelledMsg{Token: tok} }
	}
	return true, nil // swallow stray keys while modal is up
}

// Render draws the modal centered. Returns "" when closed.
func (c *Confirm) Render(width, height int) string {
	if !c.open {
		return ""
	}
	w := width - 16
	if w < 40 {
		w = width - 4
	}
	t := c.theme
	titleStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	msgStyle := lipgloss.NewStyle().Foreground(t.Text)
	yes := t.Chip(t.Error).Render(" y · confirm ")
	no := t.Chip(t.Accent2).Render(" n · cancel ")
	hint := lipgloss.NewStyle().Foreground(t.Muted).Render("   esc to cancel")
	body := titleStyle.Render(c.title) + "\n\n" +
		msgStyle.Render(c.message) + "\n\n" +
		yes + "   " + no + hint
	card := t.Card(true).Width(w).Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceForeground(t.Faint))
}
