package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// OTPModal collects a fresh 6-digit OTP code for an API call that
// demands inline 2-step verification (notably Share.Snapshot
// create/delete on the firmware we calibrated against). It mirrors
// the Confirm/Prompt token-routing shape: the host view tells the
// modal "I need an OTP to do X under token T", the modal returns
// either OTPProvidedMsg{T, code} or OTPCancelledMsg{T}, and the host
// re-issues the original action threading the captured code through
// dsm.CallWithOTP.
//
// The token is deliberately opaque — callers stuff whatever
// identifying string they need (e.g. "snapshot.create:photos") so a
// single host view can have several distinct OTP-protected actions
// in flight without keeping side state.
type OTPModal struct {
	theme tui.Theme

	open    bool
	prompt  string
	token   string
	input   textinput.Model
	width   int
	submit  key.Binding
	cancel  key.Binding
	flash   string
}

// OTPProvidedMsg is delivered when the user submits a code.
//
// The Code is whatever the user typed — the modal validates that the
// field is 6 digits before firing, so callers can trust the shape,
// but should still surface DSM's own "code rejected" error (404)
// rather than re-validating client-side.
type OTPProvidedMsg struct {
	Token string
	Code  string
}

// OTPCancelledMsg is delivered when the user backs out of the modal
// (esc or ctrl-c). Hosts should treat this as "abort the action",
// not "retry without a code".
type OTPCancelledMsg struct {
	Token string
}

// NewOTPModal constructs an idle modal bound to the given theme. The
// input is configured for the 6-digit numeric codes DSM expects;
// alphanumeric codes from hardware tokens haven't appeared on any
// firmware we've tested, so we lean on the simpler digit-only
// validator.
func NewOTPModal(theme tui.Theme) *OTPModal {
	ti := textinput.New()
	ti.Placeholder = "123456"
	ti.CharLimit = 6
	ti.Width = 12
	// DSM 2FA codes are always digits; reject letters at input time
	// so the user gets immediate feedback rather than a "code invalid"
	// after a round trip.
	ti.Validate = func(s string) error {
		for _, r := range s {
			if r < '0' || r > '9' {
				return errOnlyDigits
			}
		}
		return nil
	}
	return &OTPModal{
		theme:  theme,
		input:  ti,
		submit: key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "submit")),
		cancel: key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel")),
	}
}

// Ask opens the modal with prompt tied to token. prompt should be a
// short, action-shaped sentence (e.g. "OTP needed to create snapshot
// of Volume 1") — the modal renders it verbatim above the input.
func (m *OTPModal) Ask(token, prompt string) {
	m.open = true
	m.token = token
	m.prompt = prompt
	m.flash = ""
	m.input.SetValue("")
	m.input.Focus()
}

// Open reports whether the modal currently owns input.
func (m *OTPModal) Open() bool { return m.open }

// Close dismisses the modal without firing a message. Used by hosts
// that want to swap the modal out programmatically (e.g. when an
// outer action gets cancelled by something other than the modal
// itself).
func (m *OTPModal) Close() {
	m.open = false
	m.input.Blur()
}

// Flash shows an inline error message under the input — used by the
// host when DSM rejects the previously-submitted code, so the modal
// can stay open for a retry.
func (m *OTPModal) Flash(msg string) {
	m.flash = msg
}

// Update routes input while the modal is open. The first return value
// reports whether the message was consumed (so the host can skip its
// own handling). The tea.Cmd carries OTPProvidedMsg / OTPCancelledMsg
// when the user submits or backs out.
func (m *OTPModal) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !m.open {
		return false, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(km, m.submit):
			code := strings.TrimSpace(m.input.Value())
			if len(code) != 6 {
				m.flash = "code must be 6 digits"
				return true, nil
			}
			tok := m.token
			m.open = false
			m.input.Blur()
			return true, func() tea.Msg { return OTPProvidedMsg{Token: tok, Code: code} }
		case key.Matches(km, m.cancel):
			tok := m.token
			m.open = false
			m.input.Blur()
			return true, func() tea.Msg { return OTPCancelledMsg{Token: tok} }
		}
	}
	var c tea.Cmd
	m.input, c = m.input.Update(msg)
	return true, c
}

// Render draws the modal centered. Returns "" when closed.
func (m *OTPModal) Render(width, height int) string {
	if !m.open {
		return ""
	}
	t := m.theme
	w := width - 16
	if w < 50 {
		w = width - 4
	}
	m.input.Width = w - 12
	title := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("2-step verification")
	prompt := lipgloss.NewStyle().Foreground(t.Text).Render(m.prompt)
	hint := lipgloss.NewStyle().Foreground(t.Muted).Render("Open your authenticator app and enter the current 6-digit code.")
	body := title + "\n" + prompt + "\n" + hint + "\n\n" + m.input.View() + "\n\n" +
		t.Chip(t.Accent2).Render(" ⏎ submit ") + "  " +
		t.SubtleChip().Render(" esc · cancel ")
	if m.flash != "" {
		body += "\n\n" + lipgloss.NewStyle().Foreground(t.Error).Render("  "+m.flash)
	}
	card := t.Card(true).Width(w).Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceForeground(t.Faint))
}

// OTPKeys are the help-overlay bindings for hosts that embed an OTPModal.
var OTPKeys = []key.Binding{
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "submit OTP")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel OTP")),
}

// errOnlyDigits is the sentinel returned by the input validator. We
// intentionally keep the message terse — the textinput model renders
// it inline, where extra prose just blows up the modal.
var errOnlyDigits = otpValidationError("digits only")

type otpValidationError string

func (e otpValidationError) Error() string { return string(e) }
