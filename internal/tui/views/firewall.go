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

// FirewallView combines the global firewall status card, the list of
// configured profiles, and the ordered rules for the currently-active
// profile. Switching the cursor between profile rows refetches rules
// for that profile so you can compare without leaving the screen.
//
// CRUD lives at the rule level — `c` opens a create form scoped to the
// currently-loaded profile, `D` deletes the cursor'd rule via confirm,
// `e` / `d` toggle the enable flag. Profile rows can't be mutated from
// here; when the cursor is over a profile row the mutation keys flash
// a "select a rule first" hint instead.

type firewallStatusMsg struct {
	S   *dsm.FirewallConf
	Err error
}
type firewallProfilesMsg struct {
	P   []dsm.FirewallProfile
	Err error
}
type firewallRulesMsg struct {
	Profile string
	R       []dsm.FirewallRule
	Err     error
}

// firewallActionMsg carries the result of a CRUD call back to the loop.
// Action is the verb used in flash messages ("create", "delete",
// "enable", "disable") so the success/failure flash phrases naturally.
type firewallActionMsg struct {
	Action string
	Target string // rule name or "rule #<id>"
	Err    error
}

type FirewallView struct {
	ctx Ctx

	status   *dsm.FirewallConf
	profiles []dsm.FirewallProfile
	// rules are scoped to the profile we last fetched.
	rulesProfile string
	rules        []dsm.FirewallRule

	statusErr, profilesErr, rulesErr error

	cursor int
	filter Filter
	loaded bool

	detailRule    *dsm.FirewallRule
	detailProfile *dsm.FirewallProfile

	form    *FirewallRuleForm
	confirm *Confirm
	flash   string
}

// FirewallRuleSavedMsg fires when the create form is submitted. Like
// the UserForm pattern: the form is pure UI, the host issues the DSM
// call.
type FirewallRuleSavedMsg struct {
	Profile string
	Rule    dsm.NewFirewallRule
}

// FirewallRuleCancelledMsg fires when the user backs out of the form.
type FirewallRuleCancelledMsg struct{}

func NewFirewall(c Ctx) tui.View {
	return &FirewallView{
		ctx:     c,
		form:    NewFirewallRuleCreateForm(c.Theme),
		confirm: NewConfirm(c.Theme),
	}
}

func (v *FirewallView) Name() string                   { return "firewall" }
func (v *FirewallView) Title() string                  { return "Firewall" }
func (v *FirewallView) Icon() string                   { return "▤" }
func (v *FirewallView) RefreshInterval() time.Duration { return 60 * time.Second }
func (v *FirewallView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "create rule")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete rule (confirm)")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable rule")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable rule")),
	)
}

// IsTextEditing defers global keys while the form or confirm modal
// owns input.
func (v *FirewallView) IsTextEditing() bool {
	return v.form.Open() || v.confirm.Open() || v.filter.IsActive()
}

// Hint advertises the mutation keys when the cursor is over a rule row.
// On a profile row we shorten the strip — the mutation keys would just
// flash "select a rule first", which isn't worth advertising.
func (v *FirewallView) Hint() string {
	if r, ok := v.current(); ok && r.kind == fwRowRule {
		return "⏎ details · c create rule · D delete · e/d enable/disable · / filter · r refresh"
	}
	return "↑/↓ move (cursor on profile refetches its rules) · ⏎ details · / filter · r refresh"
}

func (v *FirewallView) Init() tea.Cmd {
	return tea.Batch(v.fetchStatus(), v.fetchProfiles(), v.fetchRules(""))
}

func (v *FirewallView) fetchStatus() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.FirewallConf, error) { return c.FirewallStatus(ctx) },
		func(s *dsm.FirewallConf, err error) tea.Msg { return firewallStatusMsg{S: s, Err: err} },
	)
}

func (v *FirewallView) fetchProfiles() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.FirewallProfile, error) { return c.FirewallProfiles(ctx) },
		func(p []dsm.FirewallProfile, err error) tea.Msg { return firewallProfilesMsg{P: p, Err: err} },
	)
}

func (v *FirewallView) fetchRules(profile string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.FirewallRule, error) { return c.FirewallRules(ctx, profile) },
		func(r []dsm.FirewallRule, err error) tea.Msg {
			return firewallRulesMsg{Profile: profile, R: r, Err: err}
		},
	)
}

func (v *FirewallView) createCmd(profile string, r dsm.NewFirewallRule) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	target := r.Name
	if target == "" {
		target = "rule"
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.CreateFirewallRule(ctx, profile, r)
		},
		func(_ struct{}, err error) tea.Msg {
			return firewallActionMsg{Action: "create", Target: target, Err: err}
		},
	)
}

func (v *FirewallView) deleteCmd(profile string, id int, label string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.DeleteFirewallRule(ctx, profile, id)
		},
		func(_ struct{}, err error) tea.Msg {
			return firewallActionMsg{Action: "delete", Target: label, Err: err}
		},
	)
}

func (v *FirewallView) setEnabledCmd(profile string, id int, label string, enabled bool) tea.Cmd {
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
			return struct{}{}, c.SetFirewallRuleEnabled(ctx, profile, id, enabled)
		},
		func(_ struct{}, err error) tea.Msg {
			return firewallActionMsg{Action: verb, Target: label, Err: err}
		},
	)
}

// flat row model: profiles first, then rules for the active profile.
type firewallRowKind int

const (
	fwRowProfile firewallRowKind = iota
	fwRowRule
)

type firewallRow struct {
	kind  firewallRowKind
	index int
}

func (v *FirewallView) filterProfiles() []dsm.FirewallProfile {
	if v.filter.Value() == "" {
		return v.profiles
	}
	out := make([]dsm.FirewallProfile, 0, len(v.profiles))
	for _, p := range v.profiles {
		if MatchesAll(v.filter.Value(), p.Name, p.Description) {
			out = append(out, p)
		}
	}
	return out
}

func (v *FirewallView) filterRules() []dsm.FirewallRule {
	if v.filter.Value() == "" {
		return v.rules
	}
	out := make([]dsm.FirewallRule, 0, len(v.rules))
	for _, r := range v.rules {
		if MatchesAll(v.filter.Value(), r.Policy, r.Protocol, r.PortDst, r.SrcIP, r.SrcSubnet, r.SrcType, r.Adapter, r.Comment) {
			out = append(out, r)
		}
	}
	return out
}

func (v *FirewallView) flatten() []firewallRow {
	var out []firewallRow
	for i := range v.filterProfiles() {
		out = append(out, firewallRow{fwRowProfile, i})
	}
	for i := range v.filterRules() {
		out = append(out, firewallRow{fwRowRule, i})
	}
	return out
}

func (v *FirewallView) current() (firewallRow, bool) {
	rows := v.flatten()
	if v.cursor < 0 || v.cursor >= len(rows) {
		return firewallRow{}, false
	}
	return rows[v.cursor], true
}

// activeProfile is the profile name we'd target with a mutation. We
// prefer the rules-loaded profile (so create/delete sit alongside the
// list the user is looking at) and fall back to the global active
// profile from the status card. Empty string means "we don't know" —
// the mutation keys flash a hint and skip the call in that case.
func (v *FirewallView) activeProfile() string {
	if v.rulesProfile != "" {
		return v.rulesProfile
	}
	if v.status != nil {
		return v.status.ProfileName
	}
	return ""
}

func (v *FirewallView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Modal routing first.
	if handled, cmd := v.form.Update(msg); handled {
		return v, cmd
	}
	if handled, cmd := v.confirm.Update(msg); handled {
		return v, cmd
	}

	switch m := msg.(type) {
	case FirewallRuleSavedMsg:
		v.flash = "creating rule in " + m.Profile + "…"
		return v, v.createCmd(m.Profile, m.Rule)
	case FirewallRuleCancelledMsg:
		v.flash = "cancelled"
		return v, nil
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "fw.delete:"); ok {
			// Token shape "fw.delete:<profile>/<id>/<label>".
			parts := strings.SplitN(rest, "/", 3)
			if len(parts) == 3 {
				profile := parts[0]
				var id int
				_, _ = fmt.Sscanf(parts[1], "%d", &id)
				label := parts[2]
				v.flash = "deleting " + label + "…"
				return v, v.deleteCmd(profile, id, label)
			}
		}
		return v, nil
	case CancelledMsg:
		v.flash = "cancelled"
		return v, nil
	case firewallActionMsg:
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
		return v, v.fetchRules(v.rulesProfile)
	}

	if v.detailRule != nil || v.detailProfile != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detailRule, v.detailProfile = nil, nil
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
		return v, tea.Batch(v.fetchStatus(), v.fetchProfiles(), v.fetchRules(v.rulesProfile))
	case firewallStatusMsg:
		v.status, v.statusErr = m.S, m.Err
		v.loaded = true
	case firewallProfilesMsg:
		v.profiles, v.profilesErr = m.P, m.Err
		v.loaded = true
		v.clampCursor()
	case firewallRulesMsg:
		v.rulesProfile, v.rules, v.rulesErr = m.Profile, m.R, m.Err
		v.loaded = true
		v.clampCursor()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := v.flatten()
			if v.cursor < len(rows)-1 {
				v.cursor++
				// When landing on a profile row, refetch rules for it.
				if r, ok := v.current(); ok && r.kind == fwRowProfile {
					return v, v.fetchRules(v.filterProfiles()[r.index].Name)
				}
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
				if r, ok := v.current(); ok && r.kind == fwRowProfile {
					return v, v.fetchRules(v.filterProfiles()[r.index].Name)
				}
			}
		case "g":
			v.cursor = 0
		case "G":
			v.cursor = max(len(v.flatten())-1, 0)
		case "/":
			v.filter.Open()
			v.cursor = 0
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, tea.Batch(v.fetchStatus(), v.fetchProfiles(), v.fetchRules(v.rulesProfile))
		case "enter":
			if r, ok := v.current(); ok {
				switch r.kind {
				case fwRowProfile:
					p := v.filterProfiles()[r.index]
					v.detailProfile = &p
				case fwRowRule:
					rule := v.filterRules()[r.index]
					v.detailRule = &rule
				}
			}
		case "c":
			// Create scoped to the active profile. The form needs a
			// profile name to attach the rule to; if we don't have one,
			// flash a hint and skip opening the form rather than
			// guessing.
			profile := v.activeProfile()
			if profile == "" {
				v.flash = "no profile loaded — move the cursor to a profile first"
				return v, nil
			}
			v.form.OpenCreate(profile)
		case "D":
			r, ok := v.current()
			if !ok {
				return v, nil
			}
			if r.kind != fwRowRule {
				v.flash = "select a rule first (current row is a profile)"
				return v, nil
			}
			rule := v.filterRules()[r.index]
			profile := v.activeProfile()
			if profile == "" {
				v.flash = "no profile loaded for this rule list"
				return v, nil
			}
			label := fmt.Sprintf("rule #%d", rule.Order)
			if rule.Comment != "" {
				label = rule.Comment
			}
			v.confirm.Ask(
				fmt.Sprintf("fw.delete:%s/%d/%s", profile, rule.RuleID, label),
				"Delete "+label+"?",
				"Removing a rule changes how DSM evaluates traffic. There is no undo — recreate from the create form if you change your mind.")
		case "e":
			r, ok := v.current()
			if !ok {
				return v, nil
			}
			if r.kind != fwRowRule {
				v.flash = "select a rule first (current row is a profile)"
				return v, nil
			}
			rule := v.filterRules()[r.index]
			profile := v.activeProfile()
			if profile == "" {
				v.flash = "no profile loaded for this rule list"
				return v, nil
			}
			label := fmt.Sprintf("rule #%d", rule.Order)
			if rule.Comment != "" {
				label = rule.Comment
			}
			v.flash = "enabling " + label + "…"
			return v, v.setEnabledCmd(profile, rule.RuleID, label, true)
		case "d":
			r, ok := v.current()
			if !ok {
				return v, nil
			}
			if r.kind != fwRowRule {
				v.flash = "select a rule first (current row is a profile)"
				return v, nil
			}
			rule := v.filterRules()[r.index]
			profile := v.activeProfile()
			if profile == "" {
				v.flash = "no profile loaded for this rule list"
				return v, nil
			}
			label := fmt.Sprintf("rule #%d", rule.Order)
			if rule.Comment != "" {
				label = rule.Comment
			}
			v.flash = "disabling " + label + "…"
			return v, v.setEnabledCmd(profile, rule.RuleID, label, false)
		}
	}
	return v, nil
}

func (v *FirewallView) clampCursor() {
	n := len(v.flatten())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *FirewallView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.form.Open() {
		return v.form.Render(width, height)
	}
	if v.confirm.Open() {
		return v.confirm.Render(width, height)
	}
	if v.detailRule != nil {
		return renderFirewallRuleDetail(t, width, *v.detailRule)
	}
	if v.detailProfile != nil {
		return renderFirewallProfileDetail(t, width, *v.detailProfile)
	}

	if v.loaded && v.status == nil && len(v.profiles) == 0 &&
		v.statusErr == nil && v.profilesErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"▤  Firewall",
			"Firewall API isn't advertised by this DSM build.",
			"Open Control Panel → Security → Firewall in DSM to set up profiles."), height)
	}

	profs := v.filterProfiles()
	rules := v.filterRules()
	cursor := v.cursor
	idx := 0

	var parts []string
	parts = append(parts, v.renderStatusCard(width))
	parts = append(parts, "")
	parts = append(parts, sectionHeader(t, width, "Profiles", len(profs), v.profilesErr))
	if v.profiles == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(profs) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for _, p := range profs {
		parts = append(parts, v.renderProfileRow(p, cursor == idx))
		idx++
	}

	ruleTitle := "Rules"
	if v.rulesProfile != "" {
		ruleTitle = "Rules · " + v.rulesProfile
	}
	parts = append(parts, "", sectionHeader(t, width, ruleTitle, len(rules), v.rulesErr))
	if v.rules == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(rules) == 0 {
		parts = append(parts, "  "+muted(t, "(no rules)"))
	}
	for _, r := range rules {
		parts = append(parts, v.renderRuleRow(r, cursor == idx))
		idx++
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

func (v *FirewallView) renderStatusCard(width int) string {
	t := v.ctx.Theme
	if v.status == nil {
		return t.Card(false).Width(width - 2).Render(
			t.Title().Render(" Firewall ") + "\n  " + muted(t, "loading…"))
	}
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	state := "disabled"
	if v.status.Enable.Bool() {
		state = "enabled"
	}
	chip := t.HealthStyle(state).Render(" " + state + " ")
	pair := func(k, val string) string {
		if val == "" {
			val = "—"
		}
		return mu.Render(k+":") + " " + text.Render(val)
	}
	row1 := strings.Join([]string{
		mu.Render("Status:") + " " + chip,
		pair("Active profile", v.status.ProfileName),
		pair("Default policy", v.status.DefaultPolicy),
	}, "   ")
	row2 := strings.Join([]string{
		pair("Geo-DB", v.status.GeoDBVersion),
		mu.Render("Log denied:") + " " + boolChip(t, v.status.LogDeny.Bool()),
		mu.Render("Notify denied:") + " " + boolChip(t, v.status.NotifyDeny.Bool()),
	}, "   ")
	body := t.Title().Render(" Firewall ") + "\n" + row1 + "\n" + row2
	return t.Card(false).Width(width - 2).Render(body)
}

func boolChip(t tui.Theme, on bool) string {
	if on {
		return t.HealthStyle("enabled").Render(" on ")
	}
	return t.HealthStyle("disabled").Render(" off ")
}

func (v *FirewallView) renderProfileRow(p dsm.FirewallProfile, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	active := ""
	if v.status != nil && v.status.ProfileName == p.Name {
		active = lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("● active")
	}
	defaultTag := ""
	if p.IsDefault.Bool() {
		defaultTag = mu.Render("default")
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(p.Name), 22), " ",
		padRight(mu.Render(clipTo(p.Description, 30)), 30), " ",
		padLeft(mu.Render(fmt.Sprintf("%d rules", p.RuleCount)), 12), " ",
		padRight(defaultTag, 10), " ",
		active,
	)
}

func (v *FirewallView) renderRuleRow(r dsm.FirewallRule, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	policy := r.Policy
	if policy == "" {
		policy = "—"
	}
	src := r.SrcIP
	if src == "" {
		src = r.SrcSubnet
	}
	if src == "" {
		src = r.SrcType
	}
	enabled := "disabled"
	if r.Enable.Bool() {
		enabled = "enabled"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padLeft(mu.Render(fmt.Sprintf("%d", r.Order)), 4), " ",
		padRight(t.HealthStyle(map[string]string{"accept": "ok", "drop": "stopped"}[strings.ToLower(policy)]).Render(strings.ToUpper(policy)), 10), " ",
		padRight(text.Render(r.Protocol), 8), " ",
		padRight(mu.Render(r.PortDst), 12), " ",
		padRight(mu.Render(clipTo(src, 22)), 22), " ",
		padRight(mu.Render(r.Adapter), 10), " ",
		t.HealthStyle(enabled).Render(enabled),
	)
}

// Inspect implements tui.Inspector — useful here because firewall
// rules carry comments and source-geo lists that won't fit in a row.
func (v *FirewallView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	r, ok := v.current()
	if !ok {
		return muted(t, "  (no selection)")
	}
	switch r.kind {
	case fwRowProfile:
		return renderFirewallProfileInspect(t, width, v.filterProfiles()[r.index])
	case fwRowRule:
		return renderFirewallRuleInspect(t, width, v.filterRules()[r.index])
	}
	return ""
}

func renderFirewallProfileDetail(t tui.Theme, width int, p dsm.FirewallProfile) string {
	if width < 60 {
		width = 60
	}
	parts := []string{
		hero(t, width, "▤", p.Name, "", p.Description),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", fmt.Sprintf("%d", p.ID)},
			{"Name", p.Name},
			{"Description", p.Description},
			{"Rule count", fmt.Sprintf("%d", p.RuleCount)},
			{"In use", yesNo(p.InUse.Bool())},
			{"Default", yesNo(p.IsDefault.Bool())},
		}),
		noteCard(t, width, "  esc to go back · move the cursor onto a profile in the list to load its rules"),
	}
	return strings.Join(parts, "\n")
}

func renderFirewallRuleDetail(t tui.Theme, width int, r dsm.FirewallRule) string {
	if width < 60 {
		width = 60
	}
	enabled := "disabled"
	if r.Enable.Bool() {
		enabled = "enabled"
	}
	parts := []string{
		hero(t, width, "▤", fmt.Sprintf("Rule #%d", r.Order), enabled, r.Policy),
		propsCard(t, width, " Properties ", [][2]string{
			{"Rule ID", fmt.Sprintf("%d", r.RuleID)},
			{"Profile ID", fmt.Sprintf("%d", r.ProfileID)},
			{"Order", fmt.Sprintf("%d", r.Order)},
			{"Policy", r.Policy},
			{"Protocol", r.Protocol},
			{"Dst port", r.PortDst},
			{"Source type", r.SrcType},
			{"Source IP", r.SrcIP},
			{"Source subnet", r.SrcSubnet},
			{"Adapter", r.Adapter},
		}),
	}
	if len(r.SrcGeo) > 0 {
		body := t.Title().Render(" Source geo ") + "\n  " +
			lipgloss.NewStyle().Foreground(t.Text).Render(strings.Join(r.SrcGeo, ", "))
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	if r.Comment != "" {
		body := t.Title().Render(" Comment ") + "\n" +
			wrap(lipgloss.NewStyle().Foreground(t.Text), r.Comment, width-6)
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back · D delete · e/d enable/disable"))
	return strings.Join(parts, "\n")
}

func renderFirewallProfileInspect(t tui.Theme, width int, p dsm.FirewallProfile) string {
	_ = width
	return strings.Join([]string{
		t.Title().Render(" Profile "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + p.Name),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + p.Description),
		"",
		muted(t, "  Rule count ") + fmt.Sprintf("%d", p.RuleCount),
		muted(t, "  In use     ") + yesNo(p.InUse.Bool()),
		muted(t, "  Default    ") + yesNo(p.IsDefault.Bool()),
	}, "\n")
}

func renderFirewallRuleInspect(t tui.Theme, width int, r dsm.FirewallRule) string {
	_ = width
	enabled := "disabled"
	if r.Enable.Bool() {
		enabled = "enabled"
	}
	src := r.SrcIP
	if src == "" {
		src = r.SrcSubnet
	}
	if src == "" {
		src = r.SrcType
	}
	geo := "—"
	if len(r.SrcGeo) > 0 {
		geo = strings.Join(r.SrcGeo, ", ")
	}
	return strings.Join([]string{
		t.Title().Render(" Rule "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(fmt.Sprintf("  #%d  %s", r.Order, strings.ToUpper(r.Policy))),
		"  " + t.HealthStyle(enabled).Render(enabled),
		"",
		muted(t, "  Protocol  ") + r.Protocol,
		muted(t, "  Dst port  ") + r.PortDst,
		muted(t, "  Source    ") + src,
		muted(t, "  Adapter   ") + r.Adapter,
		muted(t, "  Geo       ") + geo,
		"",
		muted(t, "  Comment") + "\n  " + r.Comment,
	}, "\n")
}

// — FirewallRuleForm —————————————————————————————————————————————————
//
// FirewallRuleForm is the create-only form for new firewall rules. It
// mirrors DDNSForm / UserForm: pure UI, no DSM calls — submission emits
// FirewallRuleSavedMsg, cancel emits FirewallRuleCancelledMsg.
//
// Action and Protocol are cycle fields rather than text inputs — DSM
// only accepts a small enumerated set for each, so a cycle is faster
// to operate and removes a class of typos. The cursor lands on the
// cycle by tabbing to it and toggles with space (or enter, since enter
// would otherwise advance + submit on the last field).

// fwRuleField identifies which form field has focus. Mixed text inputs
// + cycle fields can't share a single textinput slice cleanly, so we
// route keystrokes via this enum instead.
type fwRuleField int

const (
	fwFieldName fwRuleField = iota
	fwFieldAction
	fwFieldProtocol
	fwFieldSource
	fwFieldDestPort
	fwFieldCount
)

// FirewallRuleForm holds the create-form state. profile is the firewall
// profile we'll attach the rule to (set on OpenCreate, passed through
// the SavedMsg).
type FirewallRuleForm struct {
	theme tui.Theme

	open    bool
	profile string

	name     textinput.Model
	source   textinput.Model
	destPort textinput.Model

	action   string // cycle: "allow" / "deny"
	protocol string // cycle: "tcp" / "udp" / "all" / "icmp"

	focus fwRuleField
	flash string

	tabKey    key.Binding
	shiftTab  key.Binding
	spaceKey  key.Binding
	submitKey key.Binding
	cancelKey key.Binding
}

var (
	fwActionCycle   = []string{"allow", "deny"}
	fwProtocolCycle = []string{"tcp", "udp", "all", "icmp"}
)

// NewFirewallRuleCreateForm builds an idle form.
func NewFirewallRuleCreateForm(theme tui.Theme) *FirewallRuleForm {
	mk := func(placeholder string, charLimit int) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.CharLimit = charLimit
		ti.Width = 32
		return ti
	}
	return &FirewallRuleForm{
		theme:     theme,
		name:      mk("Allow HTTPS", 128),
		source:    mk("any · 10.0.0.0/8 · 1.2.3.4", 64),
		destPort:  mk("443 · 1024-65535 · all", 32),
		action:    "allow",
		protocol:  "tcp",
		tabKey:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next field")),
		shiftTab:  key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧⇥", "prev field")),
		spaceKey:  key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "cycle")),
		submitKey: key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("⌃s", "save")),
		cancelKey: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
	}
}

// OpenCreate opens the form scoped to profile. The profile is part of
// the submission payload — DSM keys rule mutations by profile.
func (f *FirewallRuleForm) OpenCreate(profile string) {
	f.open = true
	f.flash = ""
	f.profile = profile
	f.name.SetValue("")
	f.source.SetValue("")
	f.destPort.SetValue("")
	f.action = "allow"
	f.protocol = "tcp"
	f.focus = fwFieldName
	f.refocus()
}

func (f *FirewallRuleForm) Open() bool { return f.open }

func (f *FirewallRuleForm) Close() {
	f.open = false
	f.name.Blur()
	f.source.Blur()
	f.destPort.Blur()
}

func (f *FirewallRuleForm) textFieldFor(field fwRuleField) *textinput.Model {
	switch field {
	case fwFieldName:
		return &f.name
	case fwFieldSource:
		return &f.source
	case fwFieldDestPort:
		return &f.destPort
	}
	return nil
}

func (f *FirewallRuleForm) refocus() {
	for _, fi := range []*textinput.Model{&f.name, &f.source, &f.destPort} {
		fi.Blur()
	}
	if ti := f.textFieldFor(f.focus); ti != nil {
		ti.Focus()
	}
}

func (f *FirewallRuleForm) advance(delta int) {
	n := int(fwFieldCount)
	cur := int(f.focus) + delta
	if cur < 0 {
		cur = n - 1
	}
	if cur >= n {
		cur = 0
	}
	f.focus = fwRuleField(cur)
	f.refocus()
}

// cycleCurrent rotates the value of the cycle field at focus. Returns
// true when focus is on a cycle field (so the caller knows to swallow
// the key). Other fields fall through to the textinput.
func (f *FirewallRuleForm) cycleCurrent(delta int) bool {
	switch f.focus {
	case fwFieldAction:
		f.action = cycleValue(fwActionCycle, f.action, delta)
		return true
	case fwFieldProtocol:
		f.protocol = cycleValue(fwProtocolCycle, f.protocol, delta)
		return true
	}
	return false
}

// cycleValue returns the next value after cur in values, wrapping at
// either end. delta is +1 or -1.
func cycleValue(values []string, cur string, delta int) string {
	idx := 0
	for i, v := range values {
		if v == cur {
			idx = i
			break
		}
	}
	idx += delta
	if idx < 0 {
		idx = len(values) - 1
	}
	if idx >= len(values) {
		idx = 0
	}
	return values[idx]
}

func (f *FirewallRuleForm) submit() (handled bool, cmd tea.Cmd) {
	name := strings.TrimSpace(f.name.Value())
	src := strings.TrimSpace(f.source.Value())
	dst := strings.TrimSpace(f.destPort.Value())
	if dst == "" {
		dst = "all"
	}
	r := dsm.NewFirewallRule{
		Name:     name,
		Action:   f.action,
		Protocol: f.protocol,
		Source:   src,
		DestPort: dst,
		Profile:  f.profile,
	}
	profile := f.profile
	f.Close()
	return true, func() tea.Msg { return FirewallRuleSavedMsg{Profile: profile, Rule: r} }
}

// Update routes keys while the form is open.
func (f *FirewallRuleForm) Update(msg tea.Msg) (handled bool, cmd tea.Cmd) {
	if !f.open {
		return false, nil
	}
	km, isKey := msg.(tea.KeyMsg)
	if isKey {
		switch {
		case key.Matches(km, f.cancelKey):
			f.Close()
			return true, func() tea.Msg { return FirewallRuleCancelledMsg{} }
		case key.Matches(km, f.submitKey):
			return f.submit()
		case key.Matches(km, f.tabKey):
			f.advance(1)
			return true, nil
		case key.Matches(km, f.shiftTab):
			f.advance(-1)
			return true, nil
		case km.Type == tea.KeyEnter:
			// On a cycle field, Enter rotates forward — that's the most
			// natural "primary action" for a chip-like field. On a text
			// field, Enter on the last field submits, otherwise it
			// advances.
			if f.cycleCurrent(1) {
				return true, nil
			}
			if f.focus == fwFieldCount-1 {
				return f.submit()
			}
			f.advance(1)
			return true, nil
		case key.Matches(km, f.spaceKey):
			// Space cycles when focus is on a cycle field. For text
			// fields, we fall through so space inserts a literal space.
			if f.cycleCurrent(1) {
				return true, nil
			}
		}
	}
	// Text fields receive the key (cycle fields are exhausted by the
	// switch above).
	if ti := f.textFieldFor(f.focus); ti != nil {
		var c tea.Cmd
		*ti, c = ti.Update(msg)
		return true, c
	}
	return true, nil
}

// Render draws the form centered. Returns "" when closed.
func (f *FirewallRuleForm) Render(width, height int) string {
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
	labelStyle := lipgloss.NewStyle().Foreground(t.Muted).Width(14)
	row := func(label string, view string) string { return labelStyle.Render(label) + " " + view }

	f.name.Width = w - 20
	f.source.Width = w - 20
	f.destPort.Width = w - 20

	chip := func(label string, focused bool) string {
		if focused {
			return t.Chip(t.Accent).Render(" " + label + " ")
		}
		return t.SubtleChip().Render(" " + label + " ")
	}
	actionView := chip(f.action, f.focus == fwFieldAction) + "  " +
		hint.Render("(space / enter to cycle: "+strings.Join(fwActionCycle, " · ")+")")
	protoView := chip(f.protocol, f.focus == fwFieldProtocol) + "  " +
		hint.Render("(space / enter to cycle: "+strings.Join(fwProtocolCycle, " · ")+")")

	body := row("Name", f.name.View()) + "\n" +
		row("Action", actionView) + "\n" +
		row("Protocol", protoView) + "\n" +
		row("Source", f.source.View()) + "\n" +
		row("Dst port", f.destPort.View())

	footer := t.Chip(t.Accent2).Render(" ⌃s save ") + "  " +
		t.SubtleChip().Render(" tab · next ") + "  " +
		t.SubtleChip().Render(" space · cycle ") + "  " +
		t.SubtleChip().Render(" esc · cancel ")
	view := title.Render("Create firewall rule · "+f.profile) + "\n" +
		hint.Render("Tab between fields. ⌃s submits. Source 'any' / empty allows all sources.") + "\n\n" +
		body + "\n\n" + footer
	if f.flash != "" {
		view += "\n\n" + lipgloss.NewStyle().Foreground(t.Error).Render("  "+f.flash)
	}
	card := t.Card(true).Width(w).Render(view)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceForeground(t.Faint))
}
