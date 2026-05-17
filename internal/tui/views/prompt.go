package views

import (
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Prompt is a single-line text-input modal — the read/write counterpart
// to Confirm. Views Ask() with a token + title + placeholder + initial
// value; the modal emits SubmittedMsg{Token, Value} on enter or
// CancelledMsg{Token} on esc.
type Prompt struct {
	theme  tui.Theme
	open   bool
	title  string
	hint   string
	token  string
	input  textinput.Model
	width  int
}

// SubmittedMsg fires when the user accepts the prompt.
type SubmittedMsg struct {
	Token string
	Value string
}

// NewPrompt builds an idle prompt bound to the given theme.
func NewPrompt(theme tui.Theme) *Prompt {
	ti := textinput.New()
	ti.CharLimit = 256
	return &Prompt{theme: theme, input: ti}
}

// Ask opens the modal.
func (p *Prompt) Ask(token, title, hint, initial string) {
	p.open = true
	p.token = token
	p.title = title
	p.hint = hint
	p.input.SetValue(initial)
	p.input.Focus()
}

// Open reports whether the modal currently owns input.
func (p *Prompt) Open() bool { return p.open }

// Update routes keys while open. Returns true when the message was
// consumed.
func (p *Prompt) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !p.open {
		return false, nil
	}
	km, ok := msg.(tea.KeyMsg)
	if ok {
		switch km.Type {
		case tea.KeyEnter:
			val := p.input.Value()
			tok := p.token
			p.open = false
			p.input.Blur()
			return true, func() tea.Msg { return SubmittedMsg{Token: tok, Value: val} }
		case tea.KeyEsc:
			tok := p.token
			p.open = false
			p.input.Blur()
			return true, func() tea.Msg { return CancelledMsg{Token: tok} }
		}
	}
	var c tea.Cmd
	p.input, c = p.input.Update(msg)
	return true, c
}

// Render draws the modal centered.
func (p *Prompt) Render(width, height int) string {
	if !p.open {
		return ""
	}
	t := p.theme
	w := width - 16
	if w < 50 {
		w = width - 4
	}
	p.input.Width = w - 6
	title := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(p.title)
	hint := lipgloss.NewStyle().Foreground(t.Muted).Render(p.hint)
	body := title + "\n\n" + p.input.View() + "\n\n" +
		t.Chip(t.Accent2).Render(" ⏎ accept ") + "  " +
		t.SubtleChip().Render(" esc · cancel ")
	if p.hint != "" {
		body = title + "\n" + hint + "\n\n" + p.input.View() + "\n\n" +
			t.Chip(t.Accent2).Render(" ⏎ accept ") + "  " +
			t.SubtleChip().Render(" esc · cancel ")
	}
	card := t.Card(true).Width(w).Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceForeground(t.Faint))
}

// PromptKeys are the help-overlay bindings for callers that host a Prompt.
var PromptKeys = []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "submit")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}
