package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
)

// App is the root bubbletea Model. It owns the top bar, sidebar nav,
// main+inspector body, hint bar, command palette, help overlay, and the
// contextual action menu. Views render into the main pane (and optionally
// the inspector pane via the Inspector interface); the shell handles all
// chrome.
type App struct {
	client *dsm.Client
	theme  Theme
	keys   KeyMap
	logger *log.Logger

	flat    []flatItem
	active  int // index into flat — never a header
	byName  map[string]int

	width, height int

	// Layout state.
	sidebarHidden bool
	inspectorOff  bool // user toggled inspector off explicitly

	// Modal state.
	paletteOpen bool
	palette     textinput.Model
	helpOpen    bool
	actions     *actionMenu

	// Last system snapshot, for the top bar.
	sysInfo *dsm.SystemInfo

	// Last error to surface in the status bar (transient).
	lastErr   error
	errExpire time.Time
}

// NewApp constructs the root model from a list of nav sections. The first
// non-header item becomes active.
func NewApp(client *dsm.Client, theme Theme, logger *log.Logger, sections []NavSection) *App {
	pal := textinput.New()
	pal.Prompt = ""
	pal.CharLimit = 64
	pal.Placeholder = "type a view name and press enter…"

	flat := flattenSections(sections)
	byName := map[string]int{}
	for i, it := range flat {
		if it.view != nil {
			byName[it.view.Name()] = i
		}
	}
	first := firstViewIndex(flat)
	if first < 0 {
		first = 0
	}
	return &App{
		client:  client,
		theme:   theme,
		keys:    DefaultKeys(),
		logger:  logger,
		flat:    flat,
		byName:  byName,
		active:  first,
		palette: pal,
		actions: newActionMenu(theme),
	}
}

// activeView returns the currently-focused view, or nil if the flat list
// is empty (shouldn't happen in practice).
func (a *App) activeView() View {
	if a.active < 0 || a.active >= len(a.flat) {
		return nil
	}
	return a.flat[a.active].view
}

// Init kicks off the active view and a system-info fetch for the top bar.
func (a *App) Init() tea.Cmd {
	v := a.activeView()
	if v == nil {
		return nil
	}
	cmds := []tea.Cmd{
		v.Init(),
		scheduleTick(v.Name(), v.RefreshInterval()),
		a.fetchSysInfo(),
		tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return sysInfoTick{} }),
	}
	return tea.Batch(cmds...)
}

type sysInfoTick struct{}
type sysInfoMsg struct {
	Info *dsm.SystemInfo
	Err  error
}

func (a *App) fetchSysInfo() tea.Cmd {
	if a.client == nil {
		return nil
	}
	c := a.client
	return Fetch(10*time.Second,
		func(ctx context.Context) (*dsm.SystemInfo, error) { return c.SystemInfo(ctx) },
		func(info *dsm.SystemInfo, err error) tea.Msg { return sysInfoMsg{Info: info, Err: err} },
	)
}

// Update is the main event router. Modal handling (palette/help/actions)
// runs first; otherwise messages are forwarded to the active view subject
// to global key bindings.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		return a, nil

	case sysInfoTick:
		return a, tea.Batch(a.fetchSysInfo(), tea.Tick(30*time.Second, func(t time.Time) tea.Msg { return sysInfoTick{} }))

	case sysInfoMsg:
		// Top-bar info is nice-to-have — don't pollute the hint bar with a
		// recurring error every 30s.
		if m.Err == nil {
			a.sysInfo = m.Info
		} else if a.logger != nil {
			a.logger.Debug("sysinfo fetch failed", "err", m.Err)
		}
		return a, nil

	case TickMsg:
		idx, ok := a.byName[m.View]
		if !ok {
			return a, nil
		}
		v := a.flat[idx].view
		nv, cmd := v.Update(m)
		a.flat[idx].view = nv
		a.byName[nv.Name()] = idx
		// Re-schedule only for the active view; off-screen ticks pause to
		// avoid burning network on hidden tabs.
		var resched tea.Cmd
		if a.activeView() != nil && a.activeView().Name() == m.View {
			resched = scheduleTick(m.View, nv.RefreshInterval())
		}
		return a, tea.Batch(cmd, resched)

	case tea.KeyMsg:
		// Modal modes consume keys first.
		if a.paletteOpen {
			return a.updatePalette(m)
		}
		if a.helpOpen {
			if key.Matches(m, a.keys.Help, a.keys.Back, a.keys.Quit) {
				a.helpOpen = false
				return a, nil
			}
			return a, nil
		}
		if a.actions.IsOpen() {
			handled, cmd := a.actions.Update(m)
			if handled {
				// Action-invoked messages must reach the active view.
				if cmd != nil {
					return a, a.routeActionResult(cmd)
				}
				return a, nil
			}
		}

		// If the active view is text-editing (form / prompt / OTP / etc.),
		// forward the key directly to it so q, a, /, r etc. land in the
		// input instead of triggering the global binding. The view will
		// surface back up the moment IsTextEditing flips false again.
		if te, ok := a.activeView().(TextEditing); ok && te.IsTextEditing() {
			nv, cmd := a.activeView().Update(msg)
			a.flat[a.active].view = nv
			a.byName[nv.Name()] = a.active
			return a, cmd
		}

		// Global key bindings.
		switch {
		case key.Matches(m, a.keys.Quit):
			return a, tea.Quit
		case key.Matches(m, a.keys.Help):
			a.helpOpen = true
			return a, nil
		case key.Matches(m, a.keys.Palette):
			a.paletteOpen = true
			a.palette.SetValue("")
			a.palette.Focus()
			return a, textinput.Blink
		case key.Matches(m, a.keys.NavNext):
			a.active = stepView(a.flat, a.active, +1)
			return a, a.activate()
		case key.Matches(m, a.keys.NavPrev):
			a.active = stepView(a.flat, a.active, -1)
			return a, a.activate()
		case key.Matches(m, a.keys.NavSection):
			a.active = jumpSection(a.flat, a.active, +1)
			return a, a.activate()
		case key.Matches(m, a.keys.NavSectionP):
			a.active = jumpSection(a.flat, a.active, -1)
			return a, a.activate()
		case key.Matches(m, a.keys.ToggleInsp):
			a.inspectorOff = !a.inspectorOff
			return a, nil
		case key.Matches(m, a.keys.ToggleSide):
			a.sidebarHidden = !a.sidebarHidden
			return a, nil
		case key.Matches(m, a.keys.Action):
			if act, ok := a.activeView().(Actor); ok {
				a.actions.Open(a.activeView().Title()+" — actions", act.Actions())
				return a, nil
			}
		}
	}

	// Forward everything else to the active view.
	v := a.activeView()
	if v == nil {
		return a, nil
	}
	nv, cmd := v.Update(msg)
	a.flat[a.active].view = nv
	a.byName[nv.Name()] = a.active
	return a, cmd
}

// routeActionResult turns the cmd returned by the action menu into a cmd
// that delivers the ActionInvokedMsg to the active view.
func (a *App) routeActionResult(cmd tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		msg := cmd()
		if msg == nil {
			return nil
		}
		// The menu already produced the message; forward it through the
		// active view's Update so the view can act on it. Return the
		// resulting msg/cmd here is awkward — easier: just publish the
		// ActionInvokedMsg and let the main Update loop route it to the
		// active view via the default-forward path on the next tick.
		return msg
	}
}

func (a *App) activate() tea.Cmd {
	v := a.activeView()
	if v == nil {
		return nil
	}
	return tea.Batch(v.Init(), scheduleTick(v.Name(), v.RefreshInterval()))
}

func (a *App) updatePalette(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.Type {
	case tea.KeyEsc:
		a.paletteOpen = false
		a.palette.Blur()
		return a, nil
	case tea.KeyEnter:
		val := strings.TrimSpace(a.palette.Value())
		a.paletteOpen = false
		a.palette.Blur()
		if val == "" {
			return a, nil
		}
		if target := a.resolveView(val); target >= 0 {
			a.active = target
			return a, a.activate()
		}
		a.flashErr(fmt.Errorf("no view matches %q", val))
		return a, nil
	}
	var cmd tea.Cmd
	a.palette, cmd = a.palette.Update(m)
	return a, cmd
}

// resolveView matches by name (exact > prefix > substring) and falls back
// to title-substring. Section headers are ignored.
func (a *App) resolveView(q string) int {
	q = strings.ToLower(strings.TrimSpace(q))
	type cand struct{ i int; name, title string }
	var cs []cand
	for i, it := range a.flat {
		if it.view == nil {
			continue
		}
		cs = append(cs, cand{i, strings.ToLower(it.view.Name()), strings.ToLower(it.view.Title())})
	}
	for _, c := range cs {
		if c.name == q {
			return c.i
		}
	}
	for _, c := range cs {
		if strings.HasPrefix(c.name, q) {
			return c.i
		}
	}
	for _, c := range cs {
		if strings.Contains(c.name, q) {
			return c.i
		}
	}
	for _, c := range cs {
		if strings.Contains(c.title, q) {
			return c.i
		}
	}
	return -1
}

func (a *App) flashErr(err error) {
	a.lastErr = err
	a.errExpire = time.Now().Add(5 * time.Second)
}

// View renders the entire screen.
func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	top := a.renderTopBar()
	hint := a.renderHintBar()
	palLine := ""
	if a.paletteOpen {
		palLine = a.renderPalette()
	}

	chromeLines := 2 // top + hint
	if a.paletteOpen {
		chromeLines++
	}
	bodyH := a.height - chromeLines
	if bodyH < 1 {
		bodyH = 1
	}

	body := a.renderBody(a.width, bodyH)

	if a.helpOpen {
		return a.renderHelpOverlay()
	}
	if a.actions.IsOpen() {
		// Render the menu over the current body for context retention.
		base := top + "\n" + body
		if palLine != "" {
			base += "\n" + palLine
		}
		base += "\n" + hint
		return overlay(base, a.actions.Render(a.width, a.height), a.width, a.height, a.theme)
	}

	parts := []string{top, body}
	if palLine != "" {
		parts = append(parts, palLine)
	}
	parts = append(parts, hint)
	return strings.Join(parts, "\n")
}

// renderBody composes sidebar + main + inspector, padding each column to
// the requested height so they render as parallel blocks.
func (a *App) renderBody(width, height int) string {
	t := a.theme
	sideW := 0
	if !a.sidebarHidden {
		sideW = sidebarWidth(width)
	}
	inspW := 0
	if !a.inspectorOff {
		if _, ok := a.activeView().(Inspector); ok {
			inspW = InspectorWidth(width)
		}
	}
	// Reserve 1 col for each visible separator.
	mainW := width - sideW - inspW
	if sideW > 0 {
		mainW--
	}
	if inspW > 0 {
		mainW--
	}
	if mainW < 20 {
		mainW = 20
	}

	side := ""
	if sideW > 0 {
		side = a.renderSidebar(sideW, height)
	}
	mainOut := ""
	if v := a.activeView(); v != nil {
		mainOut = fitToHeight(v.Render(mainW, height), height)
	}
	inspOut := ""
	if inspW > 0 {
		if insp, ok := a.activeView().(Inspector); ok {
			inspOut = fitToHeight(a.renderInspectorPane(insp, inspW, height), height)
		}
	}

	sep := lipgloss.NewStyle().Foreground(t.Border)
	cols := []string{}
	if sideW > 0 {
		cols = append(cols, side, sep.Render(verticalLine(height)))
	}
	cols = append(cols, mainOut)
	if inspW > 0 {
		cols = append(cols, sep.Render(verticalLine(height)), inspOut)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

func verticalLine(h int) string {
	return strings.TrimRight(strings.Repeat("│\n", h), "\n")
}

// renderInspectorPane wraps the view's Inspect output in the inspector
// surface — a subtly different background so the column reads as its own
// region without screaming for attention.
func (a *App) renderInspectorPane(insp Inspector, width, height int) string {
	t := a.theme
	body := insp.Inspect(width-2, height-1)
	if strings.TrimSpace(body) == "" {
		body = lipgloss.NewStyle().Foreground(t.Faint).Render(" " + "select a row to inspect")
	}
	return lipgloss.NewStyle().
		Background(t.SurfaceAlt).
		Foreground(t.Text).
		Width(width).
		Height(height).
		Padding(0, 1).
		Render(body)
}

func sidebarWidth(total int) int {
	switch {
	case total < 80:
		return 0
	case total < 120:
		return 18
	default:
		return 22
	}
}

func (a *App) renderSidebar(width, height int) string {
	t := a.theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	faint := lipgloss.NewStyle().Foreground(t.Faint)
	headerStyle := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	itemActive := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	itemIdle := lipgloss.NewStyle().Foreground(t.Text)

	var lines []string
	lines = append(lines, "") // breathing room from top bar

	for i, it := range a.flat {
		if it.isHeader {
			if len(lines) > 1 {
				lines = append(lines, "") // gap between sections
			}
			label := " " + strings.ToUpper(it.header)
			lines = append(lines, headerStyle.Render(clipFitLeft(label, width)))
			continue
		}
		active := i == a.active
		marker := t.SidebarMarker(active)
		icon := it.view.Icon()
		label := it.view.Title()
		row := marker + " " + icon + "  "
		labelStyle := itemIdle
		if active {
			labelStyle = itemActive
		}
		row += labelStyle.Render(label)
		// Pad to width so highlighted rows extend to the edge.
		visW := lipgloss.Width(row)
		if visW < width {
			row += strings.Repeat(" ", width-visW)
		} else if visW > width {
			row = ansiClipLeft(row, width)
		}
		lines = append(lines, row)
	}

	// Footer: tiny legend so first-timers know what the marker means.
	if len(lines) < height-2 {
		for len(lines) < height-2 {
			lines = append(lines, "")
		}
		lines = append(lines, faint.Render(clipFitLeft("  tab · view  } · sect", width)))
		lines = append(lines, muted.Render(clipFitLeft("  : · cmd  ? · help", width)))
	}
	out := strings.Join(lines, "\n")
	return fitToHeight(out, height)
}

// fitToHeight pads with blank lines or truncates so the rendered body is
// exactly `n` lines tall.
func fitToHeight(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		return strings.Join(lines[:n], "\n")
	}
	if len(lines) < n {
		return s + strings.Repeat("\n", n-len(lines))
	}
	return s
}

func (a *App) renderTopBar() string {
	t := a.theme
	host := "(no connection)"
	dsmVer := ""
	uptime := ""
	if a.client != nil {
		host = a.client.Host()
	}
	if a.sysInfo != nil {
		raw := a.sysInfo.DSMVersion
		if raw == "" {
			raw = a.sysInfo.Version
		}
		if raw != "" {
			if strings.HasPrefix(strings.ToUpper(raw), "DSM") {
				dsmVer = raw
			} else {
				dsmVer = "DSM " + raw
			}
		}
		if a.sysInfo.UptimeSeconds != "" {
			uptime = "up " + humanizeUptime(a.sysInfo.UptimeSeconds)
		}
	}

	brand := t.Wordmark()
	hostS := lipgloss.NewStyle().Foreground(t.Text).Render(host)
	dot := lipgloss.NewStyle().Foreground(t.Success).Render("●")
	if a.client == nil || !a.client.Authenticated() {
		dot = lipgloss.NewStyle().Foreground(t.Error).Render("●")
	}

	left := lipgloss.JoinHorizontal(lipgloss.Center,
		" ", brand,
		"   ", dot, " ", hostS,
	)
	mid := lipgloss.NewStyle().Foreground(t.Muted).Render(dsmVer)
	clock := time.Now().Format("15:04")
	right := lipgloss.NewStyle().Foreground(t.Muted).Render(uptime + "  " + clock + " ")

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(mid) - lipgloss.Width(right)
	if gap < 2 {
		gap = 2
	}
	pad := strings.Repeat(" ", gap/2)
	tail := strings.Repeat(" ", gap-gap/2)

	return lipgloss.NewStyle().
		Background(t.BgAlt).
		Width(a.width).
		Render(left + pad + mid + tail + right)
}

func (a *App) renderHintBar() string {
	t := a.theme
	if a.lastErr != nil && time.Now().Before(a.errExpire) {
		msg := " ⚠ " + a.lastErr.Error()
		if lipgloss.Width(msg) > a.width {
			r := []rune(msg)
			if len(r) > a.width-1 {
				msg = string(r[:a.width-1]) + "…"
			}
		}
		return lipgloss.NewStyle().
			Background(t.BgAlt).
			Foreground(t.Error).
			Width(a.width).
			Render(msg)
	}

	muted := lipgloss.NewStyle().Foreground(t.Muted)
	chip := func(k, label string) string {
		return t.Chip(t.Accent2).Render(k) + muted.Render(" "+label+"  ")
	}

	// Left half — view-specific hint, set by the active view if it
	// implements the Hinter interface. Falls back to a generic line
	// when the view didn't bother (so we never show a *wrong* hint).
	left := " "
	if h, ok := a.activeView().(Hinter); ok {
		if s := strings.TrimSpace(h.Hint()); s != "" {
			left = " " + muted.Render(s)
		}
	}
	if strings.TrimSpace(left) == "" {
		left = " " + chip("⏎", "select") + chip("/", "filter") + chip("r", "refresh")
	}

	// Right half — globals that work everywhere. ⇥ navigates the
	// sidebar; } jumps sections; : opens the palette; ? shows help;
	// q quits.
	right := chip("⇥", "nav") + chip(":", "cmd") + chip("?", "help") + chip("q", "quit") + " "

	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		// Trim left to fit; the right side has the more critical hints.
		left = ansiClipLeft(left, a.width-lipgloss.Width(right))
		gap = a.width - lipgloss.Width(left) - lipgloss.Width(right)
		if gap < 0 {
			gap = 0
		}
	}
	row := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().Background(t.BgAlt).Render(row)
}

func (a *App) renderPalette() string {
	t := a.theme
	prompt := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render(" : ")
	a.palette.Width = a.width - 6
	return lipgloss.NewStyle().
		Background(t.BgAlt).
		Width(a.width).
		Render(prompt + a.palette.View())
}

func (a *App) renderHelpOverlay() string {
	t := a.theme
	var lines []string
	add := func(k key.Binding) {
		h := k.Help()
		lines = append(lines,
			t.Chip(t.Accent).Render(" "+h.Key+" ")+"  "+lipgloss.NewStyle().Foreground(t.Text).Render(h.Desc),
		)
	}
	global := []key.Binding{
		a.keys.Help, a.keys.Quit, a.keys.Palette, a.keys.Filter, a.keys.Refresh,
		a.keys.NavNext, a.keys.NavPrev, a.keys.NavSection, a.keys.NavSectionP,
		a.keys.Action, a.keys.ToggleInsp, a.keys.ToggleSide, a.keys.YankPath,
		a.keys.Up, a.keys.Down, a.keys.Top, a.keys.Bottom, a.keys.PageUp, a.keys.PageDown,
		a.keys.Enter, a.keys.Back,
	}
	lines = append(lines, t.Title().Render("Global"))
	for _, k := range global {
		add(k)
	}
	if v := a.activeView(); v != nil {
		local := v.Bindings()
		if len(local) > 0 {
			lines = append(lines, "", t.Title().Render(v.Title()+" — view"))
			sort.Slice(local, func(i, j int) bool { return local[i].Help().Desc < local[j].Help().Desc })
			for _, k := range local {
				add(k)
			}
		}
	}

	content := strings.Join(lines, "\n")
	card := t.Card(true).Render(t.Title().Render(" Help — press ? or esc to close ") + "\n\n" + content)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, card,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(t.Faint),
	)
}

// overlay renders b centered over base. For now it just delegates to
// lipgloss.Place over the existing body — the action menu doesn't need
// transparent compositing.
func overlay(base, b string, width, height int, t Theme) string {
	_ = base
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, b,
		lipgloss.WithWhitespaceForeground(t.Faint))
}

// humanizeUptime accepts DSM's uptime strings ("ddd:hh:mm:ss" on DSM 6,
// "hhh:mm:ss" on DSM 7) and returns a compact "Nd Mh" form.
func humanizeUptime(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 4:
		return parts[0] + "d " + parts[1] + "h"
	case 3:
		hours, err := strconv.Atoi(parts[0])
		if err != nil {
			return s
		}
		days := hours / 24
		rem := hours % 24
		if days == 0 {
			return fmt.Sprintf("%dh %sm", rem, parts[1])
		}
		return fmt.Sprintf("%dd %dh", days, rem)
	}
	return s
}

// jumpSection moves the cursor to the first view of the next (delta=+1) or
// previous (delta=-1) section.
func jumpSection(items []flatItem, from, delta int) int {
	if len(items) == 0 {
		return from
	}
	// Find the section the cursor is currently in.
	curSection := ""
	if from >= 0 && from < len(items) {
		curSection = items[from].section
	}
	// Walk through, collecting section boundaries (first view of each section).
	type bound struct {
		idx     int
		section string
	}
	var bounds []bound
	seen := map[string]bool{}
	for i, it := range items {
		if it.view != nil && !seen[it.section] {
			bounds = append(bounds, bound{idx: i, section: it.section})
			seen[it.section] = true
		}
	}
	if len(bounds) == 0 {
		return from
	}
	cur := 0
	for i, b := range bounds {
		if b.section == curSection {
			cur = i
			break
		}
	}
	next := cur + delta
	if next < 0 {
		next = len(bounds) - 1
	}
	if next >= len(bounds) {
		next = 0
	}
	return bounds[next].idx
}

// clipFitLeft truncates with ellipsis or right-pads a left-aligned label.
func clipFitLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s + strings.Repeat(" ", w-lipgloss.Width(s))
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return string(r[:w-1]) + "…"
}

// ansiClipLeft is a crude truncation for ANSI-containing strings. Used
// only by the sidebar where we control the rendered glyphs.
func ansiClipLeft(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	// Naive but acceptable: walk runes and stop at width. For our sidebar
	// rows the colour spans are short enough that this rarely truncates
	// mid-escape; if it ever does, lipgloss tolerates the broken sequence
	// without crashing the terminal.
	out := []rune{}
	for _, r := range s {
		if lipgloss.Width(string(out)) >= w {
			break
		}
		out = append(out, r)
	}
	return string(out)
}
