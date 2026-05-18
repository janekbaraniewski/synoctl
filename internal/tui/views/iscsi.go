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

// ISCSIView is the read-only iSCSI / SAN Manager view. It's tabbed:
//
//   - Targets: iSCSI targets, their IQNs, current initiator connection
//     counts, and auth mode.
//   - LUNs:    iSCSI LUNs (the actual block-storage units), their sizes,
//     backing type, and which targets they're mapped to.
//
// Modes cycle with `t` or jump directly via `1`/`2`. Each mode owns its
// own cursor + filter. Refresh cadence is 30s — connection counts move on
// initiator timescales and a tight loop would just spam DSM.

// iscsiMode is which tab the iSCSI view is showing.
type iscsiMode int

const (
	iscsiModeTargets iscsiMode = iota
	iscsiModeLUNs
)

func (m iscsiMode) String() string {
	switch m {
	case iscsiModeTargets:
		return "Targets"
	case iscsiModeLUNs:
		return "LUNs"
	}
	return "?"
}

type iscsiTargetsMsg struct {
	T   []dsm.ISCSITarget
	Err error
}

type iscsiLUNsMsg struct {
	L   []dsm.ISCSILUN
	Err error
}

// ISCSIView is the iSCSI / SAN Manager tabbed view.
type ISCSIView struct {
	ctx Ctx

	mode iscsiMode

	targets    []dsm.ISCSITarget
	targetsErr error
	luns       []dsm.ISCSILUN
	lunsErr    error

	bases  [2]listBase
	loaded [2]bool

	detailTarget *dsm.ISCSITarget
	detailLUN    *dsm.ISCSILUN
}

// NewISCSI constructs the iSCSI / SAN Manager view.
func NewISCSI(c Ctx) tui.View { return &ISCSIView{ctx: c} }

func (v *ISCSIView) Name() string                   { return "iscsi" }
func (v *ISCSIView) Title() string                  { return "iSCSI" }
func (v *ISCSIView) Icon() string                   { return "⇆" }
func (v *ISCSIView) RefreshInterval() time.Duration { return 30 * time.Second }

func (v *ISCSIView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "next mode")),
		key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "targets")),
		key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "luns")),
	)
}

// Hint returns the mode-aware bottom-bar hint.
func (v *ISCSIView) Hint() string {
	switch v.mode {
	case iscsiModeTargets:
		return "t mode · 1/2 jump · ⏎ details · / filter · r refresh"
	case iscsiModeLUNs:
		return "t mode · 1/2 jump · ⏎ details · / filter · r refresh"
	}
	return ""
}

func (v *ISCSIView) Init() tea.Cmd {
	return tea.Batch(v.fetchTargets(), v.fetchLUNs())
}

func (v *ISCSIView) fetchTargets() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.ISCSITarget, error) { return c.ISCSITargets(ctx) },
		func(x []dsm.ISCSITarget, err error) tea.Msg { return iscsiTargetsMsg{T: x, Err: err} },
	)
}

func (v *ISCSIView) fetchLUNs() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.ISCSILUN, error) { return c.ISCSILUNs(ctx) },
		func(x []dsm.ISCSILUN, err error) tea.Msg { return iscsiLUNsMsg{L: x, Err: err} },
	)
}

func (v *ISCSIView) base() *listBase { return &v.bases[v.mode] }

func (v *ISCSIView) visibleTargets() []dsm.ISCSITarget {
	if v.bases[iscsiModeTargets].FilterValue() == "" {
		return v.targets
	}
	out := make([]dsm.ISCSITarget, 0, len(v.targets))
	for _, tg := range v.targets {
		if MatchesAll(v.bases[iscsiModeTargets].FilterValue(),
			tg.Name, tg.IQN, tg.Auth, tg.NAAID) {
			out = append(out, tg)
		}
	}
	return out
}

func (v *ISCSIView) visibleLUNs() []dsm.ISCSILUN {
	if v.bases[iscsiModeLUNs].FilterValue() == "" {
		return v.luns
	}
	out := make([]dsm.ISCSILUN, 0, len(v.luns))
	for _, l := range v.luns {
		if MatchesAll(v.bases[iscsiModeLUNs].FilterValue(),
			l.Name, l.Type, l.DevicePath) {
			out = append(out, l)
		}
	}
	return out
}

func (v *ISCSIView) visibleCount() int {
	switch v.mode {
	case iscsiModeTargets:
		return len(v.visibleTargets())
	case iscsiModeLUNs:
		return len(v.visibleLUNs())
	}
	return 0
}

func (v *ISCSIView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Detail overlay swallows everything except esc/q.
	if v.detailTarget != nil || v.detailLUN != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detailTarget, v.detailLUN = nil, nil
		}
		return v, nil
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchTargets(), v.fetchLUNs())
	case iscsiTargetsMsg:
		v.targets, v.targetsErr = m.T, m.Err
		v.loaded[iscsiModeTargets] = true
		v.bases[iscsiModeTargets].ClampCursor(len(v.visibleTargets()))
		return v, nil
	case iscsiLUNsMsg:
		v.luns, v.lunsErr = m.L, m.Err
		v.loaded[iscsiModeLUNs] = true
		v.bases[iscsiModeLUNs].ClampCursor(len(v.visibleLUNs()))
		return v, nil
	}

	if _, handled := v.base().HandleKey(msg, v.visibleCount()); handled {
		return v, nil
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "t":
			v.mode = iscsiMode((int(v.mode) + 1) % 2)
			return v, nil
		case "1":
			v.mode = iscsiModeTargets
			return v, nil
		case "2":
			v.mode = iscsiModeLUNs
			return v, nil
		case "r":
			switch v.mode {
			case iscsiModeTargets:
				return v, v.fetchTargets()
			case iscsiModeLUNs:
				return v, v.fetchLUNs()
			}
		case "enter":
			v.openDetail()
		}
	}
	return v, nil
}

func (v *ISCSIView) openDetail() {
	switch v.mode {
	case iscsiModeTargets:
		rows := v.visibleTargets()
		if c := v.base().Cursor(); c < len(rows) {
			tg := rows[c]
			v.detailTarget = &tg
		}
	case iscsiModeLUNs:
		rows := v.visibleLUNs()
		if c := v.base().Cursor(); c < len(rows) {
			l := rows[c]
			v.detailLUN = &l
		}
	}
}

func (v *ISCSIView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detailTarget != nil {
		return renderISCSITargetDetail(t, width, *v.detailTarget)
	}
	if v.detailLUN != nil {
		return renderISCSILUNDetail(t, width, *v.detailLUN)
	}

	// Empty-state when iSCSI / SAN Manager isn't installed.
	if v.loaded[iscsiModeTargets] && v.loaded[iscsiModeLUNs] &&
		len(v.targets) == 0 && len(v.luns) == 0 &&
		v.targetsErr == nil && v.lunsErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⇆  iSCSI / SAN Manager",
			"iSCSI / SAN Manager is not installed, or no targets / LUNs are configured.",
			"Open SAN Manager in DSM to define iSCSI targets and LUNs — they'll show up here."), height)
	}

	var parts []string
	parts = append(parts, v.renderTabs(width))
	parts = append(parts, "")

	switch v.mode {
	case iscsiModeTargets:
		parts = append(parts, v.renderTargets(width)...)
	case iscsiModeLUNs:
		parts = append(parts, v.renderLUNs(width)...)
	}

	if f := v.base().FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *ISCSIView) renderTabs(width int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	idle := lipgloss.NewStyle().Foreground(t.Text)
	active := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)

	tab := func(m iscsiMode, count int) string {
		label := m.String() + " (" + itoa(count) + ")"
		if m == v.mode {
			return "▎ " + active.Render(label)
		}
		return "  " + idle.Render(label)
	}
	row := tab(iscsiModeTargets, len(v.targets)) + "   " + tab(iscsiModeLUNs, len(v.luns))
	rule := mu.Render(strings.Repeat("─", maxInt(width-2, 0)))
	return row + "\n" + rule
}

func (v *ISCSIView) renderTargets(width int) []string {
	t := v.ctx.Theme
	rows := v.visibleTargets()
	out := []string{sectionHeader(t, width, "iSCSI targets", len(rows), v.targetsErr)}
	if !v.loaded[iscsiModeTargets] {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	cursor := v.bases[iscsiModeTargets].Cursor()
	for i, tg := range rows {
		out = append(out, v.renderTargetRow(tg, i == cursor))
	}
	return out
}

func (v *ISCSIView) renderLUNs(width int) []string {
	t := v.ctx.Theme
	rows := v.visibleLUNs()
	out := []string{sectionHeader(t, width, "iSCSI LUNs", len(rows), v.lunsErr)}
	if !v.loaded[iscsiModeLUNs] {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	cursor := v.bases[iscsiModeLUNs].Cursor()
	for i, l := range rows {
		out = append(out, v.renderLUNRow(l, i == cursor))
	}
	return out
}

func (v *ISCSIView) renderTargetRow(tg dsm.ISCSITarget, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	state := "disabled"
	if tg.Enabled.Bool() {
		state = "enabled"
	}
	auth := tg.Auth
	if auth == "" {
		auth = "none"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(tg.Name, 22)), 22), " ",
		padRight(mu.Render(clipTo(tg.IQN, 40)), 40), " ",
		padLeft(mu.Render(fmt.Sprintf("%d conn", tg.ConnectionCount)), 9), " ",
		padRight(mu.Render(auth), 8), " ",
		t.HealthStyle(state).Render(state),
	)
}

func (v *ISCSIView) renderLUNRow(l dsm.ISCSILUN, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	kind := l.Type
	if kind == "" {
		kind = "—"
	}
	mapped := fmt.Sprintf("%d → tgt", len(l.MappedTargets))
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(l.Name, 24)), 24), " ",
		padRight(mu.Render(kind), 12), " ",
		padLeft(mu.Render(HumanBytes(uint64(l.Size))), 12), " ",
		padRight(mu.Render(mapped), 12), " ",
		mu.Render(clipTo(l.DevicePath, 30)),
	)
}

// Inspect implements tui.Inspector for the side preview pane.
func (v *ISCSIView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	switch v.mode {
	case iscsiModeTargets:
		rows := v.visibleTargets()
		if v.bases[iscsiModeTargets].Cursor() >= len(rows) {
			return muted(t, "  (no selection)")
		}
		return renderISCSITargetInspect(t, width, rows[v.bases[iscsiModeTargets].Cursor()])
	case iscsiModeLUNs:
		rows := v.visibleLUNs()
		if v.bases[iscsiModeLUNs].Cursor() >= len(rows) {
			return muted(t, "  (no selection)")
		}
		return renderISCSILUNInspect(t, width, rows[v.bases[iscsiModeLUNs].Cursor()])
	}
	return ""
}

func renderISCSITargetDetail(t tui.Theme, width int, tg dsm.ISCSITarget) string {
	if width < 60 {
		width = 60
	}
	state := "disabled"
	if tg.Enabled.Bool() {
		state = "enabled"
	}
	auth := tg.Auth
	if auth == "" {
		auth = "none"
	}
	parts := []string{
		hero(t, width, "⇆", tg.Name, state, auth),
		propsCard(t, width, " Properties ", [][2]string{
			{"Target ID", fmt.Sprintf("%d", tg.TargetID)},
			{"Name", tg.Name},
			{"IQN", tg.IQN},
			{"NAA ID", tg.NAAID},
			{"Auth", auth},
			{"Connections", fmt.Sprintf("%d", tg.ConnectionCount)},
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
		chip("enabled", tg.Enabled.Bool()),
		chip("CHAP", strings.EqualFold(tg.Auth, "chap") || strings.EqualFold(tg.Auth, "mutual")),
		chip("mutual", strings.EqualFold(tg.Auth, "mutual")),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · read-only — SAN Manager write actions aren't wired up yet"))
	return strings.Join(parts, "\n")
}

func renderISCSILUNDetail(t tui.Theme, width int, l dsm.ISCSILUN) string {
	if width < 60 {
		width = 60
	}
	kind := l.Type
	if kind == "" {
		kind = "unknown"
	}
	mapped := "—"
	if len(l.MappedTargets) > 0 {
		ids := make([]string, 0, len(l.MappedTargets))
		for _, id := range l.MappedTargets {
			ids = append(ids, fmt.Sprintf("%d", id))
		}
		mapped = strings.Join(ids, ", ")
	}
	parts := []string{
		hero(t, width, "⊞", l.Name, kind, HumanBytes(uint64(l.Size))),
		propsCard(t, width, " Properties ", [][2]string{
			{"LUN ID", fmt.Sprintf("%d", l.LUNID)},
			{"Name", l.Name},
			{"Type", kind},
			{"Size", HumanBytes(uint64(l.Size))},
			{"Device path", l.DevicePath},
			{"Mapped targets", mapped},
		}),
	}
	parts = append(parts, noteCard(t, width, "  esc to go back"))
	return strings.Join(parts, "\n")
}

func renderISCSITargetInspect(t tui.Theme, width int, tg dsm.ISCSITarget) string {
	state := "disabled"
	if tg.Enabled.Bool() {
		state = "enabled"
	}
	auth := tg.Auth
	if auth == "" {
		auth = "none"
	}
	return strings.Join([]string{
		t.Title().Render(" iSCSI target "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + clipTo(tg.Name, width-4)),
		"  " + t.HealthStyle(state).Render(state),
		"",
		muted(t, "  IQN"),
		"  " + clipTo(tg.IQN, width-4),
		"",
		muted(t, "  NAA ID      ") + clipTo(tg.NAAID, width-16),
		muted(t, "  Auth        ") + auth,
		muted(t, "  Connections ") + fmt.Sprintf("%d", tg.ConnectionCount),
	}, "\n")
}

func renderISCSILUNInspect(t tui.Theme, width int, l dsm.ISCSILUN) string {
	kind := l.Type
	if kind == "" {
		kind = "unknown"
	}
	mapped := "—"
	if len(l.MappedTargets) > 0 {
		ids := make([]string, 0, len(l.MappedTargets))
		for _, id := range l.MappedTargets {
			ids = append(ids, fmt.Sprintf("%d", id))
		}
		mapped = strings.Join(ids, ", ")
	}
	return strings.Join([]string{
		t.Title().Render(" iSCSI LUN "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + clipTo(l.Name, width-4)),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + kind),
		"",
		muted(t, "  Size        ") + HumanBytes(uint64(l.Size)),
		muted(t, "  Mapped      ") + mapped,
		"",
		muted(t, "  Device path"),
		"  " + clipTo(l.DevicePath, width-4),
	}, "\n")
}
