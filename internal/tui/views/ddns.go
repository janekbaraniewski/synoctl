package views

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// DDNSView lists configured Dynamic DNS hostnames and their last-known
// external IPs. The providers list is fetched alongside so the detail
// overlay (and the create-form's provider hint) can show a human-
// readable provider name even if the record only stores the provider's
// machine key. CRUD: `c` open the create form, `D` delete with confirm,
// `e` / `d` toggle the enable flag.

type ddnsRecordsMsg struct {
	R   []dsm.DDNSRecord
	Err error
}
type ddnsProvidersMsg struct {
	P   []dsm.DDNSProvider
	Err error
}

// ddnsActionMsg carries the result of a CRUD call back into the
// bubbletea loop. Action is the verb used in flash messages ("create",
// "delete", "enable", "disable") so the user sees the right phrasing.
type ddnsActionMsg struct {
	Action string
	Target string // hostname for delete / toggle; ditto for create
	Err    error
}

type DDNSView struct {
	ctx Ctx

	records   []dsm.DDNSRecord
	providers []dsm.DDNSProvider

	recordsErr, providersErr error

	cursor int
	filter Filter
	loaded bool

	detail  *dsm.DDNSRecord
	form    *DDNSForm
	confirm *Confirm
	flash   string
}

// DDNSSavedMsg fires when the DDNS form is submitted. Mirrors the
// UserForm pattern: the form itself stays pure (no DSM calls) so the
// host view drives the actual mutation, keeping the form re-usable.
type DDNSSavedMsg struct {
	Record dsm.NewDDNSRecord
}

// DDNSCancelledMsg fires when the user backs out of the form.
type DDNSCancelledMsg struct{}

func NewDDNS(c Ctx) tui.View {
	return &DDNSView{
		ctx:     c,
		form:    NewDDNSCreateForm(c.Theme),
		confirm: NewConfirm(c.Theme),
	}
}

func (v *DDNSView) Name() string                   { return "ddns" }
func (v *DDNSView) Title() string                  { return "DDNS" }
func (v *DDNSView) Icon() string                   { return "⌬" }
func (v *DDNSView) RefreshInterval() time.Duration { return 60 * time.Second }
func (v *DDNSView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "create record")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete record (confirm)")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
	)
}

// IsTextEditing tells the shell to defer global keys while the form or
// confirm modal owns input — typed runes need to reach those widgets.
func (v *DDNSView) IsTextEditing() bool {
	return v.form.Open() || v.confirm.Open() || v.filter.IsActive()
}

func (v *DDNSView) Init() tea.Cmd { return tea.Batch(v.fetchRecords(), v.fetchProviders()) }

func (v *DDNSView) fetchRecords() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.DDNSRecord, error) { return c.DDNSRecords(ctx) },
		func(r []dsm.DDNSRecord, err error) tea.Msg { return ddnsRecordsMsg{R: r, Err: err} },
	)
}

func (v *DDNSView) fetchProviders() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) ([]dsm.DDNSProvider, error) { return c.DDNSProviders(ctx) },
		func(p []dsm.DDNSProvider, err error) tea.Msg { return ddnsProvidersMsg{P: p, Err: err} },
	)
}

func (v *DDNSView) filtered() []dsm.DDNSRecord {
	if v.filter.Value() == "" {
		return v.records
	}
	out := make([]dsm.DDNSRecord, 0, len(v.records))
	for _, r := range v.records {
		if MatchesAll(v.filter.Value(), r.Hostname, r.Provider, r.Username, r.Status, r.ExternalIPv4, r.ExternalIPv6) {
			out = append(out, r)
		}
	}
	return out
}

func (v *DDNSView) providerDisplay(name string) string {
	for _, p := range v.providers {
		if p.Name == name {
			if p.DisplayName != "" {
				return p.DisplayName
			}
			return p.Name
		}
	}
	return name
}

// providerNames extracts the machine keys so the form can hint at the
// available providers. We pass the slice rather than the full list so
// the form stays decoupled from dsm types.
func (v *DDNSView) providerNames() []string {
	out := make([]string, 0, len(v.providers))
	for _, p := range v.providers {
		out = append(out, p.Name)
	}
	return out
}

// createCmd posts the new record to DSM and routes the result back as a
// ddnsActionMsg. 30s ceiling covers the slow handshake DSM does with
// the upstream provider on first insert.
func (v *DDNSView) createCmd(r dsm.NewDDNSRecord) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) { return struct{}{}, c.CreateDDNSRecord(ctx, r) },
		func(_ struct{}, err error) tea.Msg {
			return ddnsActionMsg{Action: "create", Target: r.Hostname, Err: err}
		},
	)
}

func (v *DDNSView) deleteCmd(hostname string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) { return struct{}{}, c.DeleteDDNSRecord(ctx, hostname) },
		func(_ struct{}, err error) tea.Msg {
			return ddnsActionMsg{Action: "delete", Target: hostname, Err: err}
		},
	)
}

func (v *DDNSView) setEnabledCmd(hostname string, enabled bool) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.SetDDNSRecordEnabled(ctx, hostname, enabled)
		},
		func(_ struct{}, err error) tea.Msg {
			return ddnsActionMsg{Action: verb, Target: hostname, Err: err}
		},
	)
}

func (v *DDNSView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Modal routing first.
	if handled, cmd := v.form.Update(msg); handled {
		return v, cmd
	}
	if handled, cmd := v.confirm.Update(msg); handled {
		return v, cmd
	}

	switch m := msg.(type) {
	case DDNSSavedMsg:
		v.flash = "creating " + m.Record.Hostname + "…"
		return v, v.createCmd(m.Record)
	case DDNSCancelledMsg:
		v.flash = "cancelled"
		return v, nil
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "ddns.delete:"); ok {
			v.flash = "deleting " + rest + "…"
			return v, v.deleteCmd(rest)
		}
		return v, nil
	case CancelledMsg:
		v.flash = "cancelled"
		return v, nil
	case ddnsActionMsg:
		if m.Err != nil {
			v.flash = m.Action + " " + m.Target + " failed: " + m.Err.Error()
		} else {
			switch m.Action {
			case "create":
				v.flash = m.Target + " created"
			case "delete":
				v.flash = m.Target + " deleted"
			case "enable":
				v.flash = m.Target + " enabled"
			case "disable":
				v.flash = m.Target + " disabled"
			default:
				v.flash = m.Action + " " + m.Target + " ok"
			}
		}
		return v, v.fetchRecords()
	}

	if v.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detail = nil
		}
		return v, nil
	}
	if v.filter.IsActive() {
		before := v.filter.Value()
		if v.filter.Update(msg) {
			if v.filter.Value() != before {
				v.cursor = 0
			}
			return v, nil
		}
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchRecords(), v.fetchProviders())
	case ddnsRecordsMsg:
		v.records, v.recordsErr = m.R, m.Err
		v.loaded = true
		v.clampCursor()
	case ddnsProvidersMsg:
		v.providers, v.providersErr = m.P, m.Err
		v.loaded = true
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := v.filtered()
			if v.cursor < len(rows)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g":
			v.cursor = 0
		case "G":
			v.cursor = max(len(v.filtered())-1, 0)
		case "/":
			v.filter.Open()
			v.cursor = 0
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, tea.Batch(v.fetchRecords(), v.fetchProviders())
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				r := rows[v.cursor]
				v.detail = &r
			}
		case "c":
			v.form.OpenCreate(v.providerNames())
		case "D":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				host := rows[v.cursor].Hostname
				v.confirm.Ask("ddns.delete:"+host,
					"Delete DDNS record "+host+"?",
					"DSM stops updating the hostname. The DNS record at the provider stays until they prune it.")
			}
		case "e":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				host := rows[v.cursor].Hostname
				v.flash = "enabling " + host + "…"
				return v, v.setEnabledCmd(host, true)
			}
		case "d":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				host := rows[v.cursor].Hostname
				v.flash = "disabling " + host + "…"
				return v, v.setEnabledCmd(host, false)
			}
		}
	}
	return v, nil
}

func (v *DDNSView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *DDNSView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.form.Open() {
		return v.form.Render(width, height)
	}
	if v.confirm.Open() {
		return v.confirm.Render(width, height)
	}
	if v.detail != nil {
		return renderDDNSDetail(t, width, *v.detail, v.providerDisplay(v.detail.Provider))
	}

	if v.loaded && len(v.records) == 0 && v.recordsErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⌬  Dynamic DNS",
			"No Dynamic DNS hostnames configured.",
			"Press `c` to add one, or open Control Panel → External Access → DDNS in DSM."), height)
	}

	records := v.filtered()
	var parts []string
	parts = append(parts, sectionHeader(t, width, "DDNS records", len(records), v.recordsErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(records) == 0 {
		parts = append(parts, "  "+muted(t, "(none matching)"))
	}
	for i, r := range records {
		parts = append(parts, v.renderRow(r, i == v.cursor))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · c create · D delete · e/d enable/disable · / filter · r refresh"))
	if v.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+v.flash))
	}
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *DDNSView) renderRow(r dsm.DDNSRecord, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	ip := r.ExternalIPv4
	if ip == "" {
		ip = r.ExternalIPv6
	}
	if ip == "" {
		ip = "—"
	}
	status := r.Status
	if status == "" {
		if r.Enable.Bool() {
			status = "enabled"
		} else {
			status = "disabled"
		}
	}
	updated := "—"
	if r.LastUpdated > 0 {
		updated = time.Unix(r.LastUpdated, 0).Format("2006-01-02 15:04")
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(r.Hostname, 30)), 30), " ",
		padRight(mu.Render(v.providerDisplay(r.Provider)), 16), " ",
		padRight(mu.Render(ip), 18), " ",
		padRight(mu.Render(updated), 18), " ",
		t.HealthStyle(status).Render(status),
	)
}

func renderDDNSDetail(t tui.Theme, width int, r dsm.DDNSRecord, providerDisplay string) string {
	if width < 60 {
		width = 60
	}
	status := r.Status
	if status == "" {
		if r.Enable.Bool() {
			status = "enabled"
		} else {
			status = "disabled"
		}
	}
	updated := "—"
	if r.LastUpdated > 0 {
		updated = time.Unix(r.LastUpdated, 0).Format("2006-01-02 15:04")
	}
	parts := []string{
		hero(t, width, "⌬", r.Hostname, status, providerDisplay),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", fmt.Sprintf("%d", r.ID)},
			{"Hostname", r.Hostname},
			{"Provider", providerDisplay},
			{"Provider key", r.Provider},
			{"Username", r.Username},
			{"External IPv4", r.ExternalIPv4},
			{"External IPv6", r.ExternalIPv6},
			{"Last updated", updated},
			{"Status", r.Status},
		}),
	}
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	parts = append(parts, chipsCard(t, width, " Flags ", []string{
		chip("enabled", r.Enable.Bool()),
		chip("heartbeat", r.HeartbeatEnable.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · c create · D delete · e/d enable/disable"))
	return strings.Join(parts, "\n")
}

// DDNSForm is the create-only form for new Dynamic DNS records. It
// follows the UserForm pattern: pure UI, no DSM calls — submission
// emits DDNSSavedMsg, cancel emits DDNSCancelledMsg. We keep it in
// this file rather than forms.go because the form is single-purpose
// (no edit / no password-reset mode), and bloating forms.go with
// every per-view variant would obscure the canonical UserForm.
type DDNSForm struct {
	theme tui.Theme

	open bool

	provider  textinput.Model
	hostname  textinput.Model
	username  textinput.Model
	password  textinput.Model
	providers []string

	focus int
	flash string

	tabKey    key.Binding
	shiftTab  key.Binding
	submitKey key.Binding
	cancelKey key.Binding
}

// NewDDNSCreateForm builds an idle form. The provider names list is
// populated on OpenCreate so the form picks up the latest fetch.
func NewDDNSCreateForm(theme tui.Theme) *DDNSForm {
	mk := func(placeholder string, charLimit int) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = charLimit
		ti.Width = 32
		return ti
	}
	return &DDNSForm{
		theme:     theme,
		provider:  mk("Synology", 64),
		hostname:  mk("home.synology.me", 128),
		username:  mk("user@example.com", 200),
		password:  mk("provider api key", 200),
		tabKey:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		shiftTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧⇥", "prev field")),
		submitKey: key.NewBinding(key.WithKeys("ctrl+s", "enter"), key.WithHelp("⏎", "save")),
		cancelKey: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// OpenCreate opens the form ready for a new record. providers is the
// list of provider machine keys to hint at in the form footer.
func (f *DDNSForm) OpenCreate(providers []string) {
	f.open = true
	f.flash = ""
	f.providers = providers
	f.password.EchoMode = textinput.EchoPassword
	f.password.EchoCharacter = '•'
	f.provider.SetValue("")
	f.hostname.SetValue("")
	f.username.SetValue("")
	f.password.SetValue("")
	f.focus = 0
	f.refocus()
}

func (f *DDNSForm) Open() bool { return f.open }

// Close dismisses the form without firing a message — used by the host
// to close after a successful save.
func (f *DDNSForm) Close() {
	f.open = false
	f.provider.Blur()
	f.hostname.Blur()
	f.username.Blur()
	f.password.Blur()
}

func (f *DDNSForm) fields() []*textinput.Model {
	return []*textinput.Model{&f.provider, &f.hostname, &f.username, &f.password}
}

func (f *DDNSForm) refocus() {
	for i, fi := range f.fields() {
		if i == f.focus {
			fi.Focus()
		} else {
			fi.Blur()
		}
	}
}

func (f *DDNSForm) advance(delta int) {
	fields := f.fields()
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
	f.refocus()
}

func (f *DDNSForm) submit() (handled bool, cmd tea.Cmd) {
	provider := strings.TrimSpace(f.provider.Value())
	hostname := strings.TrimSpace(f.hostname.Value())
	username := strings.TrimSpace(f.username.Value())
	password := f.password.Value()
	if provider == "" {
		f.flash = "provider is required"
		return true, nil
	}
	if hostname == "" {
		f.flash = "hostname is required"
		return true, nil
	}
	if username == "" {
		f.flash = "username is required"
		return true, nil
	}
	if password == "" {
		f.flash = "password is required"
		return true, nil
	}
	r := dsm.NewDDNSRecord{
		Provider: provider,
		Hostname: hostname,
		Username: username,
		Password: password,
	}
	f.Close()
	return true, func() tea.Msg { return DDNSSavedMsg{Record: r} }
}

// Update routes keys while the form is open.
func (f *DDNSForm) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !f.open {
		return false, nil
	}
	km, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch {
		case key.Matches(km, f.cancelKey):
			f.Close()
			return true, func() tea.Msg { return DDNSCancelledMsg{} }
		case key.Matches(km, f.tabKey):
			f.advance(1)
			return true, nil
		case key.Matches(km, f.shiftTab):
			f.advance(-1)
			return true, nil
		case km.Type == tea.KeyEnter:
			fields := f.fields()
			if f.focus >= len(fields)-1 {
				return f.submit()
			}
			f.advance(1)
			return true, nil
		}
	}
	fields := f.fields()
	if f.focus >= 0 && f.focus < len(fields) {
		ti := fields[f.focus]
		var c tea.Cmd
		*ti, c = ti.Update(msg)
		return true, c
	}
	return true, nil
}

// Render draws the form centered. Returns "" when closed.
func (f *DDNSForm) Render(width, height int) string {
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
	row := func(label string, ti *textinput.Model) string {
		labelStyle := lipgloss.NewStyle().Foreground(t.Muted).Width(14)
		ti.Width = w - 20
		return labelStyle.Render(label) + " " + ti.View()
	}
	body := row("Provider", &f.provider) + "\n" +
		row("Hostname", &f.hostname) + "\n" +
		row("Username", &f.username) + "\n" +
		row("Password", &f.password)

	providerHint := ""
	if len(f.providers) > 0 {
		providerHint = "Known providers: " + strings.Join(f.providers, ", ")
	}

	footer := t.Chip(t.Accent2).Render(" ⏎ save ") + "  " +
		t.SubtleChip().Render(" tab · next ") + "  " +
		t.SubtleChip().Render(" esc · cancel ")
	view := title.Render("Create DDNS record") + "\n" +
		hint.Render("Tab between fields. Enter on the last field submits.")
	if providerHint != "" {
		view += "\n" + hint.Render(providerHint)
	}
	view += "\n\n" + body + "\n\n" + footer
	if f.flash != "" {
		view += "\n\n" + lipgloss.NewStyle().Foreground(t.Error).Render("  "+f.flash)
	}
	card := t.Card(true).Width(w).Render(view)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceForeground(t.Faint))
}
