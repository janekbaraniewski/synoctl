package views

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// UserSavedMsg fires when the user form is submitted. Mode tells the
// host whether to call CreateUser, UpdateUser or SetUserPassword:
// the form itself doesn't make DSM calls — keeping the network out
// of the bubbletea Update path keeps the form re-usable from any
// host view.
//
// On create: Mode == "create", Patch is empty, User holds the
// new account. On edit: Mode == "update", User.Name keys the
// existing account and Patch carries the fields the user actually
// touched (so the host can avoid round-tripping unchanged
// attributes — see the UserPatch doc-comment in dsm/user.go).
type UserSavedMsg struct {
	Mode  string // "create" | "update" | "password"
	User  dsm.NewUser
	Patch dsm.UserPatch
}

// UserCancelledMsg fires when the user backs out of the form.
type UserCancelledMsg struct{}

// UserForm is a small bubbletea sub-model for create / edit / change-
// password of DSM local users. It deliberately avoids huh: the
// listBase + Confirm pattern in this codebase already runs inside a
// bubbletea host view, and dropping in a huh.Form there would mean
// blocking the goroutine the program loop runs on. A handful of
// textinput.Model fields with tab-cycling is plenty.
//
// The form is constructed in one of three modes; the host picks the
// mode based on the action the user invoked:
//
//   - NewUserCreateForm()  : full create form (name, password, …)
//   - NewUserEditForm(u)   : edit existing user's description / email / …
//   - NewUserPasswordForm(name) : reset password only
//
// The host renders the form by checking IsOpen() and calling Render
// instead of its own body; the form handles its own key routing
// when Open() is true.
type UserForm struct {
	theme tui.Theme

	open bool
	mode string // create / update / password

	name        textinput.Model
	password    textinput.Model
	description textinput.Model
	email       textinput.Model
	expired     textinput.Model

	originalName  string
	originalDesc  string
	originalEmail string
	originalExp   string

	focus int
	flash string

	tabKey    key.Binding
	shiftTab  key.Binding
	submitKey key.Binding
	cancelKey key.Binding
}

func newUserForm(theme tui.Theme) *UserForm {
	mk := func(placeholder string, charLimit int) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = charLimit
		ti.Width = 32
		return ti
	}
	return &UserForm{
		theme:       theme,
		name:        mk("alice", 64),
		password:    mk("strong passphrase", 128),
		description: mk("Alice from accounting", 200),
		email:       mk("alice@example.com", 200),
		expired:     mk("normal", 32),
		tabKey:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		shiftTab:    key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧⇥", "prev field")),
		submitKey:   key.NewBinding(key.WithKeys("ctrl+s", "enter"), key.WithHelp("⏎", "save")),
		cancelKey:   key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// NewUserCreateForm builds an idle create form. Open() returns false
// until the host calls OpenCreate().
func NewUserCreateForm(theme tui.Theme) *UserForm {
	return newUserForm(theme)
}

// OpenCreate puts the form into create mode and focuses the first
// field. Initial values for description / email default to empty.
func (f *UserForm) OpenCreate() {
	f.open = true
	f.mode = "create"
	f.flash = ""
	f.password.EchoMode = textinput.EchoPassword
	f.password.EchoCharacter = '•'
	f.name.SetValue("")
	f.password.SetValue("")
	f.description.SetValue("")
	f.email.SetValue("")
	f.expired.SetValue("normal")
	f.focus = 0
	f.refocus()
}

// OpenEdit puts the form into edit mode against u. The password
// field is hidden — call OpenPassword separately for that flow.
func (f *UserForm) OpenEdit(u dsm.User) {
	f.open = true
	f.mode = "update"
	f.flash = ""
	f.originalName = u.Name
	f.originalDesc = u.Description
	f.originalEmail = u.Email
	f.originalExp = u.Expired
	f.name.SetValue(u.Name)
	f.description.SetValue(u.Description)
	f.email.SetValue(u.Email)
	exp := u.Expired
	if exp == "" {
		exp = "normal"
	}
	f.expired.SetValue(exp)
	f.password.SetValue("")
	f.focus = 1 // skip the read-only Name field
	f.refocus()
}

// OpenPassword puts the form into password-only mode keyed on name.
func (f *UserForm) OpenPassword(name string) {
	f.open = true
	f.mode = "password"
	f.flash = ""
	f.originalName = name
	f.name.SetValue(name)
	f.password.SetValue("")
	f.password.EchoMode = textinput.EchoPassword
	f.password.EchoCharacter = '•'
	f.focus = 1
	f.refocus()
}

// Open reports whether the form currently owns input.
func (f *UserForm) Open() bool { return f.open }

// Flash shows an inline error under the form — used by the host
// when the DSM call rejected the submission, so the form can stay
// open for a fix.
func (f *UserForm) Flash(msg string) { f.flash = msg }

// Close dismisses the form without firing a message.
func (f *UserForm) Close() {
	f.open = false
	f.name.Blur()
	f.password.Blur()
	f.description.Blur()
	f.email.Blur()
	f.expired.Blur()
}

// visibleFields returns the ordered slice of input pointers that the
// current mode renders. We use this for tab cycling and validation,
// so the rendering logic doesn't drift from focus management.
func (f *UserForm) visibleFields() []*textinput.Model {
	switch f.mode {
	case "create":
		return []*textinput.Model{&f.name, &f.password, &f.description, &f.email, &f.expired}
	case "update":
		// Name is shown but read-only; we keep it in the list at
		// position 0 so OpenEdit's `focus = 1` lines up.
		return []*textinput.Model{&f.name, &f.description, &f.email, &f.expired}
	case "password":
		return []*textinput.Model{&f.name, &f.password}
	}
	return nil
}

func (f *UserForm) refocus() {
	fields := f.visibleFields()
	for i, fi := range fields {
		if i == f.focus {
			fi.Focus()
		} else {
			fi.Blur()
		}
	}
}

// Update routes keys while the form is open. The returned tea.Cmd
// carries either UserSavedMsg or UserCancelledMsg on terminal
// transitions.
func (f *UserForm) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !f.open {
		return false, nil
	}
	km, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch {
		case key.Matches(km, f.cancelKey):
			f.Close()
			return true, func() tea.Msg { return UserCancelledMsg{} }
		case key.Matches(km, f.tabKey):
			f.advance(1)
			return true, nil
		case key.Matches(km, f.shiftTab):
			f.advance(-1)
			return true, nil
		case km.Type == tea.KeyEnter:
			// On the last field, Enter submits. Anywhere earlier,
			// Enter advances (more forgiving than requiring tab).
			fields := f.visibleFields()
			if f.focus >= len(fields)-1 {
				return f.submit()
			}
			f.advance(1)
			return true, nil
		}
	}

	// Delegate to the focused field — but skip the Name field in
	// update mode (read-only).
	fields := f.visibleFields()
	if f.focus >= 0 && f.focus < len(fields) {
		if f.mode == "update" && f.focus == 0 {
			return true, nil // swallow keys aimed at the locked Name
		}
		ti := fields[f.focus]
		var c tea.Cmd
		*ti, c = ti.Update(msg)
		return true, c
	}
	return true, nil
}

func (f *UserForm) advance(delta int) {
	fields := f.visibleFields()
	if len(fields) == 0 {
		return
	}
	f.focus += delta
	if f.focus < 0 {
		f.focus = len(fields) - 1
	}
	if f.focus >= len(fields) {
		f.focus = 0
	}
	// In update mode, skip the locked Name field automatically so
	// tab actually does what the user expects.
	if f.mode == "update" && f.focus == 0 {
		if delta < 0 {
			f.focus = len(fields) - 1
		} else {
			f.focus = 1
		}
	}
	f.refocus()
}

func (f *UserForm) submit() (handled bool, cmd tea.Cmd) {
	switch f.mode {
	case "create":
		name := strings.TrimSpace(f.name.Value())
		pwd := f.password.Value()
		if name == "" {
			f.flash = "name is required"
			return true, nil
		}
		if pwd == "" {
			f.flash = "password is required"
			return true, nil
		}
		u := dsm.NewUser{
			Name:        name,
			Password:    pwd,
			Description: strings.TrimSpace(f.description.Value()),
			Email:       strings.TrimSpace(f.email.Value()),
			Expired:     strings.TrimSpace(f.expired.Value()),
		}
		f.Close()
		return true, func() tea.Msg { return UserSavedMsg{Mode: "create", User: u} }
	case "update":
		patch := dsm.UserPatch{}
		if v := strings.TrimSpace(f.description.Value()); v != f.originalDesc {
			patch.Description = &v
		}
		if v := strings.TrimSpace(f.email.Value()); v != f.originalEmail {
			patch.Email = &v
		}
		if v := strings.TrimSpace(f.expired.Value()); v != f.originalExp && v != "" {
			patch.Expired = &v
		}
		f.Close()
		return true, func() tea.Msg {
			return UserSavedMsg{
				Mode:  "update",
				User:  dsm.NewUser{Name: f.originalName},
				Patch: patch,
			}
		}
	case "password":
		pwd := f.password.Value()
		if pwd == "" {
			f.flash = "password is required"
			return true, nil
		}
		name := f.originalName
		f.Close()
		return true, func() tea.Msg {
			return UserSavedMsg{
				Mode: "password",
				User: dsm.NewUser{Name: name, Password: pwd},
			}
		}
	}
	return true, nil
}

// Render draws the form centered. Returns "" when closed.
func (f *UserForm) Render(width, height int) string {
	if !f.open {
		return ""
	}
	t := f.theme
	w := width - 16
	if w < 60 {
		w = width - 4
	}

	title := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	hint := lipgloss.NewStyle().Foreground(t.Muted)
	row := func(label string, ti *textinput.Model, readonly bool) string {
		labelStyle := lipgloss.NewStyle().Foreground(t.Muted).Width(14)
		var fieldView string
		if readonly {
			fieldView = lipgloss.NewStyle().Foreground(t.Text).Bold(true).Render(ti.Value())
		} else {
			ti.Width = w - 20
			fieldView = ti.View()
		}
		return labelStyle.Render(label) + " " + fieldView
	}

	var titleStr, body string
	switch f.mode {
	case "create":
		titleStr = "Create user"
		body = row("Name", &f.name, false) + "\n" +
			row("Password", &f.password, false) + "\n" +
			row("Description", &f.description, false) + "\n" +
			row("Email", &f.email, false) + "\n" +
			row("Expired", &f.expired, false)
	case "update":
		titleStr = "Edit user"
		body = row("Name", &f.name, true) + "\n" +
			row("Description", &f.description, false) + "\n" +
			row("Email", &f.email, false) + "\n" +
			row("Expired", &f.expired, false)
	case "password":
		titleStr = "Reset password"
		body = row("Name", &f.name, true) + "\n" +
			row("Password", &f.password, false)
	}

	footer := t.Chip(t.Accent2).Render(" ⏎ save ") + "  " +
		t.SubtleChip().Render(" tab · next ") + "  " +
		t.SubtleChip().Render(" esc · cancel ")
	view := title.Render(titleStr) + "\n" +
		hint.Render("Tab between fields. Enter on the last field submits.") + "\n\n" +
		body + "\n\n" + footer
	if f.flash != "" {
		view += "\n\n" + lipgloss.NewStyle().Foreground(t.Error).Render("  "+f.flash)
	}
	card := t.Card(true).Width(w).Render(view)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceForeground(t.Faint))
}

// UserFormKeys are the help-overlay bindings for hosts that embed
// a UserForm. We expose them so the help overlay can stay in sync.
var UserFormKeys = []key.Binding{
	key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
	key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧⇥", "prev field")),
	key.NewBinding(key.WithKeys("enter"), key.WithHelp("⏎", "save")),
	key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}
