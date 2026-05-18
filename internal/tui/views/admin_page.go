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

// AdminPage shows system info, users, network interfaces, and recent
// log entries on one screen. B / S trigger reboot / shutdown (confirmed).
type AdminPage struct {
	ctx Ctx

	info  *dsm.SystemInfo
	util  *dsm.Utilization
	users []dsm.User
	ifs   []dsm.NetworkInterface
	logs  []dsm.LogEntry

	infoErr, utilErr, usersErr, ifsErr, logsErr error

	cursor int
	filter Filter
	flash  string

	detailUser *dsm.User
	detailIF   *dsm.NetworkInterface
	detailLog  *dsm.LogEntry

	confirm *Confirm
}

func NewAdminPage(c Ctx) tui.View {
	return &AdminPage{ctx: c, confirm: NewConfirm(c.Theme)}
}

func (a *AdminPage) Name() string                   { return "admin" }
func (a *AdminPage) Title() string                  { return "Admin" }
func (a *AdminPage) Icon() string                   { return "⌂" }
func (a *AdminPage) RefreshInterval() time.Duration { return 15 * time.Second }
func (a *AdminPage) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "reboot (confirm)")),
		key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "shutdown (confirm)")),
	)
}
func (a *AdminPage) IsTextEditing() bool { return a.confirm.Open() || a.filter.IsActive() }

func (a *AdminPage) Init() tea.Cmd {
	return tea.Batch(
		a.fetchInfo(), a.fetchUtil(), a.fetchUsers(), a.fetchNet(), a.fetchLogs(),
	)
}

func (a *AdminPage) fetchInfo() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.SystemInfo, error) { return c.SystemInfo(ctx) },
		func(v *dsm.SystemInfo, err error) tea.Msg { return sysViewInfoMsg{I: v, Err: err} },
	)
}
func (a *AdminPage) fetchUtil() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(5*time.Second,
		func(ctx context.Context) (*dsm.Utilization, error) { return c.Utilization(ctx) },
		func(u *dsm.Utilization, err error) tea.Msg { return sysViewUtilMsg{U: u, Err: err} },
	)
}
func (a *AdminPage) fetchUsers() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.User, error) { return c.Users(ctx) },
		func(u []dsm.User, err error) tea.Msg { return usersMsg{U: u, Err: err} },
	)
}
func (a *AdminPage) fetchNet() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.NetworkInterface, error) { return c.NetworkInterfaces(ctx) },
		func(i []dsm.NetworkInterface, err error) tea.Msg { return netMsg{I: i, Err: err} },
	)
}
func (a *AdminPage) fetchLogs() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.LogEntry, error) {
			items, _, err := c.Logs(ctx, dsm.LogQuery{Source: "system", Limit: 30})
			return items, err
		},
		func(l []dsm.LogEntry, err error) tea.Msg { return recentLogsMsg{L: l, Err: err} },
	)
}

// — flat-row model —

type adminRowKind int

const (
	adminRowUser adminRowKind = iota
	adminRowIF
	adminRowLog
)

type adminRow struct {
	kind  adminRowKind
	index int
}

func (a *AdminPage) filterUsers() []dsm.User {
	if a.filter.Value() == "" {
		return a.users
	}
	out := make([]dsm.User, 0)
	for _, u := range a.users {
		if MatchesAll(a.filter.Value(), u.Name, u.Description, u.Email) {
			out = append(out, u)
		}
	}
	return out
}

func (a *AdminPage) filterIFs() []dsm.NetworkInterface {
	if a.filter.Value() == "" {
		return a.ifs
	}
	out := make([]dsm.NetworkInterface, 0)
	for _, i := range a.ifs {
		if MatchesAll(a.filter.Value(), i.IFName, i.Type, i.IP, i.Gateway, i.MAC, i.Status) {
			out = append(out, i)
		}
	}
	return out
}

func (a *AdminPage) filterLogs() []dsm.LogEntry {
	if a.filter.Value() == "" {
		return a.logs
	}
	out := make([]dsm.LogEntry, 0)
	for _, l := range a.logs {
		if MatchesAll(a.filter.Value(), l.Time, l.Level, l.User, l.IP, l.Event, l.Descr) {
			out = append(out, l)
		}
	}
	return out
}

func (a *AdminPage) flatten() []adminRow {
	var out []adminRow
	for i := range a.filterUsers() {
		out = append(out, adminRow{adminRowUser, i})
	}
	for i := range a.filterIFs() {
		out = append(out, adminRow{adminRowIF, i})
	}
	for i := range a.filterLogs() {
		out = append(out, adminRow{adminRowLog, i})
	}
	return out
}

func (a *AdminPage) current() (adminRow, bool) {
	rows := a.flatten()
	if a.cursor < 0 || a.cursor >= len(rows) {
		return adminRow{}, false
	}
	return rows[a.cursor], true
}

// — power actions —

type adminActionMsg struct {
	Action string
	Err    error
}

func (a *AdminPage) issue(action string) tea.Cmd {
	c := a.ctx.Client
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			switch action {
			case "reboot":
				return struct{}{}, c.Reboot(ctx)
			case "shutdown":
				return struct{}{}, c.Shutdown(ctx)
			}
			return struct{}{}, nil
		},
		func(_ struct{}, err error) tea.Msg { return adminActionMsg{Action: action, Err: err} },
	)
}

func (a *AdminPage) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := a.confirm.Update(msg); handled {
		return a, cmd
	}
	switch m := msg.(type) {
	case ConfirmedMsg:
		switch m.Token {
		case "reboot":
			a.flash = "issuing reboot…"
			return a, a.issue("reboot")
		case "shutdown":
			a.flash = "issuing shutdown…"
			return a, a.issue("shutdown")
		}
	case CancelledMsg:
		a.flash = "cancelled"
		return a, nil
	case adminActionMsg:
		if m.Err != nil {
			a.flash = m.Action + " failed: " + m.Err.Error()
		} else {
			a.flash = m.Action + " accepted — the box should obey shortly"
		}
		return a, nil
	}

	if a.detailUser != nil || a.detailIF != nil || a.detailLog != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			a.detailUser, a.detailIF, a.detailLog = nil, nil, nil
		}
		return a, nil
	}

	if a.filter.IsActive() {
		before := a.filter.Value()
		if a.filter.Update(msg) {
			if a.filter.Value() != before {
				a.cursor = 0
			}
			return a, nil
		}
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return a, tea.Batch(a.fetchInfo(), a.fetchUtil(), a.fetchUsers(), a.fetchNet(), a.fetchLogs())
	case sysViewInfoMsg:
		a.info, a.infoErr = m.I, m.Err
	case sysViewUtilMsg:
		a.util, a.utilErr = m.U, m.Err
	case usersMsg:
		a.users, a.usersErr = m.U, m.Err
		a.clampCursor()
	case netMsg:
		a.ifs, a.ifsErr = m.I, m.Err
		a.clampCursor()
	case recentLogsMsg:
		a.logs, a.logsErr = m.L, m.Err
		a.clampCursor()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := a.flatten()
			if a.cursor < len(rows)-1 {
				a.cursor++
			}
		case "k", "up":
			if a.cursor > 0 {
				a.cursor--
			}
		case "g":
			a.cursor = 0
		case "G":
			a.cursor = max(len(a.flatten())-1, 0)
		case "/":
			a.filter.Open()
			a.cursor = 0
		case "esc":
			if a.filter.Value() != "" {
				a.filter.Clear()
				a.cursor = 0
			}
		case "r":
			return a, tea.Batch(a.fetchInfo(), a.fetchUtil(), a.fetchUsers(), a.fetchNet(), a.fetchLogs())
		case "enter":
			if r, ok := a.current(); ok {
				switch r.kind {
				case adminRowUser:
					u := a.filterUsers()[r.index]
					a.detailUser = &u
				case adminRowIF:
					i := a.filterIFs()[r.index]
					a.detailIF = &i
				case adminRowLog:
					l := a.filterLogs()[r.index]
					a.detailLog = &l
				}
			}
		case "B":
			a.confirm.Ask("reboot", "Reboot deep-thought?",
				"The NAS will be unreachable for a few minutes.")
		case "S":
			a.confirm.Ask("shutdown", "Shut down deep-thought?",
				"The NAS will power off. You'll need physical access (or WoL) to bring it back.")
		}
	}
	return a, nil
}

func (a *AdminPage) clampCursor() {
	n := len(a.flatten())
	if a.cursor >= n {
		a.cursor = n - 1
	}
	if a.cursor < 0 {
		a.cursor = 0
	}
}

func (a *AdminPage) Render(width, height int) string {
	t := a.ctx.Theme
	if a.confirm.Open() {
		return a.confirm.Render(width, height)
	}
	if a.detailUser != nil {
		return renderUserDetail(t, width, *a.detailUser)
	}
	if a.detailIF != nil {
		return renderNetworkDetail(t, width, *a.detailIF)
	}
	if a.detailLog != nil {
		return renderLogDetail(t, width, *a.detailLog)
	}

	var parts []string
	parts = append(parts, a.renderSystemStrip(width))

	users := a.filterUsers()
	ifs := a.filterIFs()
	logs := a.filterLogs()
	cursor := a.cursor
	idx := 0

	parts = append(parts, "", sectionHeader(t, width, "Users", len(users), a.usersErr))
	if a.users == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(users) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for _, u := range users {
		parts = append(parts, a.renderUserRow(u, cursor == idx))
		idx++
	}

	parts = append(parts, "", sectionHeader(t, width, "Network interfaces", len(ifs), a.ifsErr))
	if a.ifs == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(ifs) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for _, i := range ifs {
		parts = append(parts, a.renderIFRow(i, cursor == idx))
		idx++
	}

	parts = append(parts, "", sectionHeader(t, width, "Recent system log", len(logs), a.logsErr))
	if a.logs == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(logs) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	maxLogRows := 12
	for li, l := range logs {
		if li >= maxLogRows {
			break
		}
		parts = append(parts, a.renderLogRow(l, cursor == idx))
		idx++
	}

	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · [B] reboot · [S] shutdown"))
	if a.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+a.flash))
	}
	if v := a.filter.Render(t); v != "" {
		parts = append(parts, v)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (a *AdminPage) renderSystemStrip(width int) string {
	t := a.ctx.Theme
	if a.info == nil {
		return t.Card(false).Width(width - 2).Render(
			t.Title().Render(" System ") + "\n  " + muted(t, "loading…"))
	}
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)

	pair := func(k, v string) string {
		if v == "" {
			v = "—"
		}
		return muted.Render(k+":") + " " + text.Render(v)
	}

	uptime := HumanDurationFromDSMUptime(a.info.UptimeSeconds).String()
	tempColored := lipgloss.NewStyle().Foreground(tempColor(t, a.info.Temperature)).Bold(true).
		Render(fmt.Sprintf("%d°C", a.info.Temperature))

	row1 := strings.Join([]string{
		pair("Model", a.info.Model),
		pair("Serial", a.info.Serial),
		pair("DSM", coalesce(a.info.DSMVersion, a.info.Version)),
	}, "   ")
	row2 := strings.Join([]string{
		pair("CPU", strings.TrimSpace(a.info.CPUVendor+" "+a.info.CPUFamily)+" · "+a.info.CPUCores+" cores"),
		pair("RAM", fmt.Sprintf("%d MB", a.info.RAMTotalMB)),
		pair("Uptime", uptime),
		muted.Render("Temp:") + " " + tempColored,
	}, "   ")

	body := t.Title().Render(" System ") + "\n" + row1 + "\n" + row2
	if a.util != nil {
		row3 := strings.Join([]string{
			pair("Load 1m", fmt.Sprintf("%d%%", a.util.CPU.OneMinLoad)),
			pair("Load 5m", fmt.Sprintf("%d%%", a.util.CPU.FiveMinLoad)),
			pair("Load 15m", fmt.Sprintf("%d%%", a.util.CPU.FifteenMinLoad)),
			pair("Mem", fmt.Sprintf("%d%% · %s used", a.util.Memory.RealUsage,
				HumanBytes(uint64(a.util.Memory.TotalReal-a.util.Memory.AvailReal)*1024))),
		}, "   ")
		body += "\n" + row3
	}
	return t.Card(false).Width(width - 2).Render(body)
}

func (a *AdminPage) renderUserRow(u dsm.User, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	status := u.Expired
	if status == "" {
		status = "normal"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(u.Name), 20), " ",
		padLeft(muted.Render(fmt.Sprintf("uid %d", u.UID)), 8), " ",
		padRight(muted.Render(u.Description), 30), " ",
		padRight(muted.Render(u.Email), 28), " ",
		t.HealthStyle(status).Render(status),
	)
}

func (a *AdminPage) renderIFRow(i dsm.NetworkInterface, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	ip := i.IP
	if ip != "" && i.Mask != "" {
		ip += "/" + i.Mask
	}
	speed := "—"
	if i.Speed > 0 {
		speed = fmt.Sprintf("%d Mbit/s", i.Speed)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(i.IFName), 12), " ",
		padRight(muted.Render(i.Type), 10), " ",
		padRight(text.Render(ip), 22), " ",
		padRight(muted.Render(i.Gateway), 18), " ",
		padLeft(muted.Render(speed), 14), " ",
		t.HealthStyle(i.Status).Render(i.Status),
	)
}

func (a *AdminPage) renderLogRow(l dsm.LogEntry, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	level := strings.ToLower(l.Level)
	var icon string
	switch level {
	case "err", "error":
		icon = lipgloss.NewStyle().Foreground(t.Error).Bold(true).Render("✗")
	case "warn", "warning":
		icon = lipgloss.NewStyle().Foreground(t.Warn).Bold(true).Render("⚠")
	default:
		icon = lipgloss.NewStyle().Foreground(t.Info).Render("•")
	}
	event := l.Event
	if l.Descr != "" {
		event = l.Descr
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		icon, " ",
		padRight(muted.Render(l.Time), 20), "  ",
		text.Render(clipTo(event, 60)),
	)
}
