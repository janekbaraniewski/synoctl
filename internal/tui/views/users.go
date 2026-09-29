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

// Users is the local-accounts view. Lists users with their UID, email,
// description, expiry state, and exposes create / edit / change-password
// / delete via the UserForm modal (depth-write stream).
type Users struct {
	ctx Ctx

	users []dsm.User
	err   error

	base    listBase
	detail  *dsm.User
	form    *UserForm
	confirm *Confirm
	flash   string
}

// NewUsers constructs the users view.
func NewUsers(c Ctx) tui.View {
	return &Users{
		ctx:     c,
		form:    NewUserCreateForm(c.Theme),
		confirm: NewConfirm(c.Theme),
	}
}

func (u *Users) Name() string                   { return "users" }
func (u *Users) Title() string                  { return "Users" }
func (u *Users) Icon() string                   { return "◐" }
func (u *Users) RefreshInterval() time.Duration { return 60 * time.Second }
func (u *Users) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "create user")),
		key.NewBinding(key.WithKeys("E"), key.WithHelp("E", "edit user")),
		key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "change password")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete user (confirm)")),
	)
}

func (u *Users) Init() tea.Cmd { return u.fetch() }

// IsTextEditing tells the shell to defer global keybindings while the
// create / edit / password form or the delete confirmation owns input.
func (u *Users) IsTextEditing() bool {
	return u.form.Open() || u.confirm.Open() || u.base.filter.IsActive()
}

func (u *Users) fetch() tea.Cmd {
	c := u.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.User, error) { return c.Users(ctx) },
		func(v []dsm.User, err error) tea.Msg { return usersMsg{U: v, Err: err} },
	)
}

func (u *Users) visible() []dsm.User {
	if u.base.FilterValue() == "" {
		return u.users
	}
	out := make([]dsm.User, 0, len(u.users))
	for _, x := range u.users {
		if MatchesAll(u.base.FilterValue(), x.Name, x.Description, x.Email) {
			out = append(out, x)
		}
	}
	return out
}

type usersActionMsg struct {
	Action string
	Name   string
	Err    error
}

func (u *Users) saveCmd(m UserSavedMsg) tea.Cmd {
	c := u.ctx.Client
	switch m.Mode {
	case "create":
		nu := m.User
		return tui.Fetch(30*time.Second,
			func(ctx context.Context) (struct{}, error) { return struct{}{}, c.CreateUser(ctx, nu) },
			func(_ struct{}, err error) tea.Msg {
				return usersActionMsg{Action: "create", Name: nu.Name, Err: err}
			},
		)
	case "update":
		name := m.User.Name
		patch := m.Patch
		return tui.Fetch(30*time.Second,
			func(ctx context.Context) (struct{}, error) { return struct{}{}, c.UpdateUser(ctx, name, patch) },
			func(_ struct{}, err error) tea.Msg {
				return usersActionMsg{Action: "update", Name: name, Err: err}
			},
		)
	case "password":
		name := m.User.Name
		pwd := m.User.Password
		return tui.Fetch(30*time.Second,
			func(ctx context.Context) (struct{}, error) { return struct{}{}, c.SetUserPassword(ctx, name, pwd) },
			func(_ struct{}, err error) tea.Msg {
				return usersActionMsg{Action: "password", Name: name, Err: err}
			},
		)
	}
	return nil
}

func (u *Users) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := u.form.Update(msg); handled {
		return u, cmd
	}
	if handled, cmd := u.confirm.Update(msg); handled {
		return u, cmd
	}
	switch m := msg.(type) {
	case UserSavedMsg:
		return u, u.saveCmd(m)
	case UserCancelledMsg:
		u.flash = "cancelled"
		return u, nil
	case usersActionMsg:
		if m.Err != nil {
			u.flash = m.Action + " " + m.Name + " failed: " + m.Err.Error()
		} else {
			u.flash = m.Action + " " + m.Name + " ok"
		}
		return u, u.fetch()
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "delete:"); ok {
			c := u.ctx.Client
			name := rest
			u.flash = "deleting " + name + "…"
			return u, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.DeleteUser(ctx, name) },
				func(_ struct{}, err error) tea.Msg {
					return usersActionMsg{Action: "delete", Name: name, Err: err}
				},
			)
		}
	case CancelledMsg:
		u.flash = "cancelled"
		return u, nil
	case tui.TickMsg:
		return u, u.fetch()
	case usersMsg:
		u.users, u.err = m.U, m.Err
		u.base.ClampCursor(len(u.visible()))
		return u, nil
	}

	if u.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				u.detail = nil
				return u, nil
			case "E":
				u.form.OpenEdit(*u.detail)
				return u, nil
			case "P":
				u.form.OpenPassword(u.detail.Name)
				return u, nil
			case "D":
				name := u.detail.Name
				u.confirm.Ask("delete:"+name, "Delete user "+name+"?",
					"This removes the local account. Their home folder may stay on disk.")
				return u, nil
			}
		}
		return u, nil
	}

	if _, handled := u.base.HandleKey(msg, len(u.visible())); handled {
		return u, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			rows := u.visible()
			if u.base.Cursor() < len(rows) {
				user := rows[u.base.Cursor()]
				u.detail = &user
			}
		case "c":
			u.form.OpenCreate()
		case "E":
			rows := u.visible()
			if u.base.Cursor() < len(rows) {
				u.form.OpenEdit(rows[u.base.Cursor()])
			}
		case "P":
			rows := u.visible()
			if u.base.Cursor() < len(rows) {
				u.form.OpenPassword(rows[u.base.Cursor()].Name)
			}
		case "D":
			rows := u.visible()
			if u.base.Cursor() < len(rows) {
				name := rows[u.base.Cursor()].Name
				u.confirm.Ask("delete:"+name, "Delete user "+name+"?",
					"This removes the local account. Their home folder may stay on disk.")
			}
		}
	}
	return u, nil
}

func (u *Users) Render(width, height int) string {
	t := u.ctx.Theme
	if u.form.Open() {
		return u.form.Render(width, height)
	}
	if u.confirm.Open() {
		return u.confirm.Render(width, height)
	}
	if u.detail != nil {
		return renderUserDetail(t, width, *u.detail)
	}
	rows := u.visible()
	parts := []string{sectionHeader(t, width, "Users", len(rows), u.err)}
	if u.users == nil && u.err == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for i, x := range rows {
		parts = append(parts, u.renderRow(x, i == u.base.Cursor()))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · c create · E edit · P password · D delete"))
	if u.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+u.flash))
	}
	if f := u.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (u *Users) Inspect(width, height int) string {
	rows := u.visible()
	if len(rows) == 0 || u.base.Cursor() >= len(rows) {
		return ""
	}
	t := u.ctx.Theme
	user := rows[u.base.Cursor()]
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	parts := []string{
		t.Title().Render(" " + user.Name + " "),
		"",
		muted.Render("UID:        ") + text.Render(fmt.Sprintf("%d", user.UID)),
		muted.Render("Email:      ") + text.Render(coalesce(user.Email, "—")),
		muted.Render("Description:") + " " + text.Render(coalesce(user.Description, "—")),
		muted.Render("Expiry:     ") + t.HealthStyle(coalesce(user.Expired, "normal")).Render(coalesce(user.Expired, "normal")),
	}
	if user.PasswordNeverExpire {
		parts = append(parts, muted.Render("Pwd policy: ")+text.Render("never expires"))
	}
	if len(user.Groups) > 0 {
		parts = append(parts, "", muted.Render("Groups:"), text.Render("  "+strings.Join(user.Groups, ", ")))
	}
	_ = width
	_ = height
	return strings.Join(parts, "\n")
}

func (u *Users) renderRow(x dsm.User, highlight bool) string {
	t := u.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	status := x.Expired
	if status == "" {
		status = "normal"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(x.Name), 20), " ",
		padLeft(muted.Render(fmt.Sprintf("uid %d", x.UID)), 8), " ",
		padRight(muted.Render(x.Description), 30), " ",
		padRight(muted.Render(x.Email), 28), " ",
		t.HealthStyle(status).Render(status),
	)
}
