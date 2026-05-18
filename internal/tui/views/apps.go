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

// appMode is which tab the unified Apps view is showing.
type appMode int

const (
	appModeInstalled appMode = iota
	appModeAvailable
	appModeServices
)

func (m appMode) String() string {
	switch m {
	case appModeInstalled:
		return "Installed"
	case appModeAvailable:
		return "Available"
	case appModeServices:
		return "Services"
	}
	return "?"
}

// Apps is the unified package & service management view. One screen
// for everything that ships in DSM Package Center plus the DSM service
// togglables — replaces the original AppsPage + Services + PackageCenter
// trio which had the same concepts split across three sidebar entries.
//
// Three modes, switched with `t` or Tab (or `1`/`2`/`3`):
//
//   - Installed — running packages. Actions: s/x/R start/stop/restart,
//     U uninstall (with confirm). Drill-down opens structured detail.
//   - Available — DSM's package catalogue. I install (with confirm),
//     r refreshes the catalog (slow — DSM walks the upstream index).
//   - Services — DSM's enable/disable togglables. e/d to flip.
//
// Each mode owns its own cursor and filter so switching tabs preserves
// the user's place.
type Apps struct {
	ctx Ctx

	mode appMode

	// Per-mode data
	installed []dsm.Package
	available []dsm.ServerPackage
	services  []dsm.Service

	installedErr, availableErr, servicesErr error
	availableLoaded                         bool      // we lazy-load the catalog
	availableLoadStarted                    time.Time // when the in-flight catalog fetch began (zero if none in flight)

	bases [3]listBase

	// Detail overlay state — at most one is non-nil at a time.
	detailPkg     *dsm.Package
	detailAvail   *dsm.ServerPackage
	detailService *dsm.Service

	confirm    *Confirm
	flash      string
	pending    map[string]string // id → action in flight
	installing map[string]bool
}

// NewApps constructs the unified packages + services view.
func NewApps(c Ctx) tui.View {
	return &Apps{
		ctx:        c,
		confirm:    NewConfirm(c.Theme),
		pending:    map[string]string{},
		installing: map[string]bool{},
	}
}

func (a *Apps) Name() string  { return "apps" }
func (a *Apps) Title() string { return "Apps" }
func (a *Apps) Icon() string  { return "▣" }

// RefreshInterval is intentionally long — a single Packages or Services
// fetch can take 20–40s on a low-end NAS, so a 30s tick was kicking off
// new fetches before the previous ones returned and piling up
// concurrent calls that made the box slower. Press `r` for an explicit
// manual refresh.
func (a *Apps) RefreshInterval() time.Duration { return 2 * time.Minute }
func (a *Apps) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "next mode")),
		key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "installed")),
		key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "available")),
		key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "services")),
		key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "start")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "stop")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "restart")),
		key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "uninstall")),
		key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "install")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "enable")),
		key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "disable")),
	)
}

// Hint returns the context-aware bottom-bar hint for the current mode.
// Implements the tui.Hinter interface so the shell can render only the
// keys that actually do something in this view.
func (a *Apps) Hint() string {
	switch a.mode {
	case appModeInstalled:
		return "t mode · 1/2/3 jump · ⏎ details · / filter · s start · x stop · R restart · U uninstall"
	case appModeAvailable:
		return "t mode · 1/2/3 jump · ⏎ details · / filter · I install · r refresh catalog"
	case appModeServices:
		return "t mode · 1/2/3 jump · ⏎ details · / filter · e enable · d disable"
	}
	return ""
}

// IsTextEditing defers global keys while a modal or inline filter owns input.
func (a *Apps) IsTextEditing() bool { return a.confirm.Open() || a.base().filter.IsActive() }

func (a *Apps) Init() tea.Cmd { return tea.Batch(a.fetchInstalled(), a.fetchServices()) }

// — fetches —

type appsCatalogMsg struct {
	C   []dsm.ServerPackage
	Err error
}

func (a *Apps) fetchInstalled() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	// 60s — DS220j on DSM 7.0.1 routinely needs 20–40s to return
	// SYNO.Core.Package.list with ~25 packages and the additional
	// fields the TUI renders. Anything shorter and the very first
	// fetch dies before it ever shows real data.
	return tui.Fetch(60*time.Second,
		func(ctx context.Context) ([]dsm.Package, error) { return c.Packages(ctx) },
		func(v []dsm.Package, err error) tea.Msg { return packagesMsg{P: v, Err: err} },
	)
}

func (a *Apps) fetchServices() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(60*time.Second,
		func(ctx context.Context) ([]dsm.Service, error) { return c.Services(ctx) },
		func(v []dsm.Service, err error) tea.Msg { return servicesMsg{S: v, Err: err} },
	)
}

// fetchCatalog is intentionally NOT called on Init — the upstream
// package-server lookup can take 60+ seconds on a low-end box, which
// would block the view from loading the Installed list. We trigger it
// the first time the user switches to Available mode.
//
// The fetch + a 1s elapsed-time ticker run concurrently so the UI
// stays alive while DSM walks Synology's upstream index.
func (a *Apps) fetchCatalog() tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	a.availableLoadStarted = time.Now()
	return tea.Batch(
		tui.Fetch(170*time.Second,
			func(ctx context.Context) ([]dsm.ServerPackage, error) { return c.PackageServerList(ctx) },
			func(v []dsm.ServerPackage, err error) tea.Msg { return appsCatalogMsg{C: v, Err: err} },
		),
		tea.Tick(1*time.Second, func(t time.Time) tea.Msg { return appsCatalogTick{} }),
	)
}

// appsCatalogTick fires once per second while a catalog fetch is in
// flight so the rendered "Xs elapsed" counter actually advances.
type appsCatalogTick struct{}

func (a *Apps) installCmd(id string) tea.Cmd {
	c := a.ctx.Client
	if c == nil {
		return nil
	}
	a.installing[id] = true
	return tui.Fetch(15*time.Minute,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.PackageInstall(ctx, id, dsm.InstallOpts{
				CheckCodesign: true,
				CheckDsm:      true,
			})
		},
		func(_ struct{}, err error) tea.Msg { return pkgActionMsg{ID: id, Action: "install", Err: err} },
	)
}

// — visibility helpers (filtered per mode) —

func (a *Apps) visibleInstalled() []dsm.Package {
	if a.bases[appModeInstalled].FilterValue() == "" {
		return a.installed
	}
	out := make([]dsm.Package, 0, len(a.installed))
	for _, p := range a.installed {
		if MatchesAll(a.bases[appModeInstalled].FilterValue(), p.ID, p.Name, p.Maintainer, p.Status, p.Version) {
			out = append(out, p)
		}
	}
	return out
}

func (a *Apps) visibleAvailable() []dsm.ServerPackage {
	if a.bases[appModeAvailable].FilterValue() == "" {
		return a.available
	}
	out := make([]dsm.ServerPackage, 0, len(a.available))
	for _, p := range a.available {
		if MatchesAll(a.bases[appModeAvailable].FilterValue(),
			p.Identifier(), p.DisplayName(), p.Maintainer, p.Description) {
			out = append(out, p)
		}
	}
	return out
}

func (a *Apps) visibleServices() []dsm.Service {
	if a.bases[appModeServices].FilterValue() == "" {
		return a.services
	}
	out := make([]dsm.Service, 0, len(a.services))
	for _, s := range a.services {
		if MatchesAll(a.bases[appModeServices].FilterValue(), s.ID, s.DisplayName(), s.EnableStatus) {
			out = append(out, s)
		}
	}
	return out
}

func (a *Apps) visibleCount() int {
	switch a.mode {
	case appModeInstalled:
		return len(a.visibleInstalled())
	case appModeAvailable:
		return len(a.visibleAvailable())
	case appModeServices:
		return len(a.visibleServices())
	}
	return 0
}

func (a *Apps) base() *listBase { return &a.bases[a.mode] }

// — mode switching —

func (a *Apps) switchMode(m appMode) tea.Cmd {
	a.mode = m
	if m == appModeAvailable && !a.availableLoaded {
		a.availableLoaded = true
		return a.fetchCatalog()
	}
	return nil
}

// — update —

func (a *Apps) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := a.confirm.Update(msg); handled {
		return a, cmd
	}
	switch m := msg.(type) {
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "uninstall:"); ok {
			a.pending[rest] = "uninstall"
			a.flash = "uninstalling " + rest + "…"
			c := a.ctx.Client
			id := rest
			return a, tui.Fetch(60*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.PackageUninstall(ctx, id) },
				func(_ struct{}, err error) tea.Msg { return pkgActionMsg{ID: id, Action: "uninstall", Err: err} },
			)
		}
		if rest, ok := strings.CutPrefix(m.Token, "install:"); ok {
			a.flash = "installing " + rest + " — this can take a few minutes…"
			return a, a.installCmd(rest)
		}
	case CancelledMsg:
		a.flash = "cancelled"
		return a, nil
	case tui.TickMsg:
		// Refresh installed + services; available stays cached.
		return a, tea.Batch(a.fetchInstalled(), a.fetchServices())
	case packagesMsg:
		a.installed, a.installedErr = m.P, m.Err
		a.bases[appModeInstalled].ClampCursor(len(a.visibleInstalled()))
		return a, nil
	case servicesMsg:
		a.services, a.servicesErr = m.S, m.Err
		a.bases[appModeServices].ClampCursor(len(a.visibleServices()))
		return a, nil
	case appsCatalogMsg:
		a.available, a.availableErr = m.C, m.Err
		a.availableLoadStarted = time.Time{} // fetch done, stop ticking
		a.bases[appModeAvailable].ClampCursor(len(a.visibleAvailable()))
		return a, nil
	case appsCatalogTick:
		// Only keep ticking while the fetch is still in flight.
		if !a.availableLoadStarted.IsZero() && a.available == nil && a.availableErr == nil {
			return a, tea.Tick(1*time.Second, func(t time.Time) tea.Msg { return appsCatalogTick{} })
		}
		return a, nil
	case pkgActionMsg:
		delete(a.pending, m.ID)
		delete(a.installing, m.ID)
		if m.Err != nil {
			a.flash = m.Action + " " + m.ID + " failed: " + m.Err.Error()
		} else {
			a.flash = m.Action + " " + m.ID + " ok"
		}
		// Re-fetch installed so the new state shows up.
		return a, a.fetchInstalled()
	case svcActionMsg:
		if m.Err != nil {
			a.flash = m.Action + " " + m.ID + " failed: " + m.Err.Error()
		} else {
			a.flash = m.Action + " " + m.ID + " ok"
		}
		return a, a.fetchServices()
	}

	// Detail overlay consumes esc/q + per-detail action keys.
	if a.anyDetailOpen() {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				a.closeDetail()
				return a, nil
			}
		}
		return a, nil
	}

	// Forward to listBase for cursor + filter.
	if _, handled := a.base().HandleKey(msg, a.visibleCount()); handled {
		return a, nil
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "t":
			// Tab is reserved for sidebar nav at the shell level, so the
			// in-view mode switcher is just `t` (plus 1/2/3 to jump
			// directly to a mode).
			next := appMode((int(a.mode) + 1) % 3)
			return a, a.switchMode(next)
		case "1":
			return a, a.switchMode(appModeInstalled)
		case "2":
			return a, a.switchMode(appModeAvailable)
		case "3":
			return a, a.switchMode(appModeServices)
		case "r":
			switch a.mode {
			case appModeInstalled:
				return a, a.fetchInstalled()
			case appModeAvailable:
				return a, a.fetchCatalog()
			case appModeServices:
				return a, a.fetchServices()
			}
		case "enter":
			a.openDetail()
		default:
			cmd := a.handleAction(km.String())
			if cmd != nil {
				return a, cmd
			}
		}
	}
	return a, nil
}

func (a *Apps) anyDetailOpen() bool {
	return a.detailPkg != nil || a.detailAvail != nil || a.detailService != nil
}

func (a *Apps) closeDetail() {
	a.detailPkg, a.detailAvail, a.detailService = nil, nil, nil
}

func (a *Apps) openDetail() {
	switch a.mode {
	case appModeInstalled:
		rows := a.visibleInstalled()
		if a.base().Cursor() < len(rows) {
			p := rows[a.base().Cursor()]
			a.detailPkg = &p
		}
	case appModeAvailable:
		rows := a.visibleAvailable()
		if a.base().Cursor() < len(rows) {
			p := rows[a.base().Cursor()]
			a.detailAvail = &p
		}
	case appModeServices:
		rows := a.visibleServices()
		if a.base().Cursor() < len(rows) {
			s := rows[a.base().Cursor()]
			a.detailService = &s
		}
	}
}

func (a *Apps) handleAction(k string) tea.Cmd {
	switch a.mode {
	case appModeInstalled:
		rows := a.visibleInstalled()
		if a.base().Cursor() >= len(rows) {
			return nil
		}
		pkg := rows[a.base().Cursor()]
		switch k {
		case "s", "x", "R":
			action := map[string]string{"s": "start", "x": "stop", "R": "restart"}[k]
			a.pending[pkg.ID] = action
			c := a.ctx.Client
			id := pkg.ID
			return tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.PackageControl(ctx, id, action) },
				func(_ struct{}, err error) tea.Msg { return pkgActionMsg{ID: id, Action: action, Err: err} },
			)
		case "U":
			a.confirm.Ask("uninstall:"+pkg.ID, "Uninstall "+pkg.Name+"?",
				"This permanently removes the package and its settings.")
		}
	case appModeAvailable:
		rows := a.visibleAvailable()
		if a.base().Cursor() >= len(rows) {
			return nil
		}
		pkg := rows[a.base().Cursor()]
		switch k {
		case "I":
			a.confirm.Ask("install:"+pkg.Identifier(), "Install "+pkg.DisplayName()+"?",
				"This downloads + installs the package on /volume1.\n"+
					"Signature and firmware checks remain enabled.")
		}
	case appModeServices:
		rows := a.visibleServices()
		if a.base().Cursor() >= len(rows) {
			return nil
		}
		svc := rows[a.base().Cursor()]
		switch k {
		case "e", "d":
			action := "enable"
			if k == "d" {
				action = "disable"
			}
			c := a.ctx.Client
			id := svc.ID
			return tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.ServiceControl(ctx, id, action) },
				func(_ struct{}, err error) tea.Msg { return svcActionMsg{ID: id, Action: action, Err: err} },
			)
		}
	}
	return nil
}

// — render —

func (a *Apps) Render(width, height int) string {
	t := a.ctx.Theme
	if a.confirm.Open() {
		return a.confirm.Render(width, height)
	}
	switch {
	case a.detailPkg != nil:
		return renderPackageDetail(t, width, *a.detailPkg)
	case a.detailService != nil:
		return renderServiceDetail(t, width, *a.detailService)
	case a.detailAvail != nil:
		return a.renderAvailDetail(width, *a.detailAvail)
	}

	var parts []string
	parts = append(parts, a.renderTabs(width))
	parts = append(parts, "")

	switch a.mode {
	case appModeInstalled:
		parts = append(parts, a.renderInstalled(width)...)
	case appModeAvailable:
		parts = append(parts, a.renderAvailable(width)...)
	case appModeServices:
		parts = append(parts, a.renderServices(width)...)
	}

	// No inline bottom-hint — the shell's global hint bar reads from
	// Hint() and renders mode-specific keys there.
	if a.flash != "" {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(t.Muted).Render("  "+a.flash))
	}
	if f := a.base().FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (a *Apps) renderTabs(width int) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	idle := lipgloss.NewStyle().Foreground(t.Text)
	active := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)

	tab := func(m appMode, count int, loading bool) string {
		label := m.String()
		if loading {
			label += " (…)"
		} else if count >= 0 {
			label += " (" + itoa(count) + ")"
		}
		if m == a.mode {
			return "▎ " + active.Render(label)
		}
		return "  " + idle.Render(label)
	}

	installedCount := len(a.installed)
	servicesCount := len(a.services)
	var availLabel string
	switch {
	case !a.availableLoaded:
		availLabel = tab(appModeAvailable, -1, false)
		availLabel = strings.Replace(availLabel, "Available (-1)", "Available (press 2)", 1)
	case a.available == nil && a.availableErr == nil:
		availLabel = tab(appModeAvailable, -1, true)
	default:
		availLabel = tab(appModeAvailable, len(a.available), false)
	}

	row := strings.Join([]string{
		tab(appModeInstalled, installedCount, false),
		availLabel,
		tab(appModeServices, servicesCount, false),
	}, "   ")

	// Faint underline rule below the tabs.
	rule := muted.Render(strings.Repeat("─", maxInt(width-2, 0)))
	return row + "\n" + rule
}

func (a *Apps) renderInstalled(width int) []string {
	t := a.ctx.Theme
	rows := a.visibleInstalled()
	out := []string{sectionHeader(t, width, "Installed packages", len(rows), a.installedErr)}
	if a.installed == nil && a.installedErr == nil {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	for i, p := range rows {
		out = append(out, a.renderInstalledRow(p, i == a.base().Cursor()))
	}
	return out
}

func (a *Apps) renderAvailable(width int) []string {
	t := a.ctx.Theme
	rows := a.visibleAvailable()
	hdr := sectionHeader(t, width, "Available packages", len(rows), a.availableErr)
	out := []string{hdr}
	if !a.availableLoaded {
		out = append(out, "  "+muted(t, "press 2 or `r` to fetch the catalog (slow — DSM walks the upstream index)"))
		return out
	}
	if a.available == nil && a.availableErr == nil {
		elapsed := ""
		if !a.availableLoadStarted.IsZero() {
			secs := int(time.Since(a.availableLoadStarted).Seconds())
			elapsed = fmt.Sprintf("  (%ds elapsed — DSM walks Synology's upstream index, sometimes hits 2 min)", secs)
		}
		out = append(out, "  "+muted(t, "fetching catalog…"+elapsed))
		return out
	}
	if a.availableErr != nil {
		out = append(out, errLine(t, a.availableErr))
		out = append(out, "  "+muted(t, "press `r` to retry"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(no packages advertised — try `r` to refresh)"))
		return out
	}
	for i, p := range rows {
		out = append(out, a.renderAvailableRow(p, i == a.base().Cursor()))
	}
	return out
}

func (a *Apps) renderServices(width int) []string {
	t := a.ctx.Theme
	rows := a.visibleServices()
	out := []string{sectionHeader(t, width, "DSM services", len(rows), a.servicesErr)}
	if a.services == nil && a.servicesErr == nil {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	for i, s := range rows {
		out = append(out, a.renderServiceRow(s, i == a.base().Cursor()))
	}
	return out
}

func (a *Apps) renderInstalledRow(p dsm.Package, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	status := p.Status
	if act, ok := a.pending[p.ID]; ok {
		status = act + "…"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(p.Name), 28), " ",
		padRight(muted.Render(p.Version), 18), " ",
		padRight(muted.Render(p.Maintainer), 24), " ",
		t.HealthStyle(status).Render(status),
	)
}

func (a *Apps) renderAvailableRow(p dsm.ServerPackage, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	pid := p.Identifier()
	state := "available"
	// Already installed?
	for _, ip := range a.installed {
		if ip.ID == pid {
			state = "installed"
			break
		}
	}
	if a.installing[pid] {
		state = "installing…"
	}
	beta := ""
	if p.Beta {
		beta = t.Chip(t.Warn).Render(" beta ") + " "
	}
	size := "—"
	if p.Size > 0 {
		size = HumanBytes(uint64(p.Size))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(p.DisplayName()), 28), " ",
		padRight(muted.Render(p.Version), 14), " ",
		padRight(muted.Render(p.Maintainer), 22), " ",
		padLeft(muted.Render(size), 10), "  ",
		beta+t.HealthStyle(state).Render(state),
	)
}

func (a *Apps) renderServiceRow(s dsm.Service, highlight bool) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	state := s.EnableStatus
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(s.ID), 30), " ",
		padRight(muted.Render(s.DisplayName()), 28), " ",
		t.HealthStyle(state).Render(state),
	)
}

func (a *Apps) renderAvailDetail(width int, p dsm.ServerPackage) string {
	t := a.ctx.Theme
	pid := p.Identifier()
	state := "available"
	if a.installing[pid] {
		state = "installing…"
	}
	parts := []string{hero(t, width, "⬇", p.DisplayName(), state, p.Version)}
	props := [][2]string{
		{"Identifier", pid},
		{"Name", p.DisplayName()},
		{"Version", p.Version},
		{"Maintainer", p.Maintainer},
	}
	if p.Size > 0 {
		props = append(props, [2]string{"Size", HumanBytes(uint64(p.Size))})
	}
	if p.DownloadURL != "" {
		props = append(props, [2]string{"Source", p.DownloadURL})
	}
	parts = append(parts, propsCard(t, width, " Package ", props))
	if p.Description != "" {
		body := t.Title().Render(" Description ") + "\n" +
			wrap(lipgloss.NewStyle().Foreground(t.Text), p.Description, width-6)
		parts = append(parts, t.Card(false).Width(width-2).Render(body))
	}
	parts = append(parts, noteCard(t, width, "  esc back · I install (confirm)"))
	if a.flash != "" {
		parts = append(parts, noteCard(t, width, "  "+a.flash))
	}
	return strings.Join(parts, "\n")
}

// Inspect renders the cursor'd row in the inspector pane.
func (a *Apps) Inspect(width, height int) string {
	t := a.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	switch a.mode {
	case appModeInstalled:
		rows := a.visibleInstalled()
		if a.base().Cursor() >= len(rows) {
			return ""
		}
		p := rows[a.base().Cursor()]
		parts := []string{
			t.Title().Render(" " + p.Name + " "),
			"",
			muted.Render(p.ID),
			"",
			muted.Render("Version:    ") + text.Render(p.Version),
			muted.Render("Maintainer: ") + text.Render(p.Maintainer),
			muted.Render("Status:     ") + t.HealthStyle(p.Status).Render(p.Status),
		}
		if p.Beta {
			parts = append(parts, "", t.Chip(t.Warn).Render(" beta "))
		}
		if p.Description != "" {
			parts = append(parts, "", muted.Render("Description"),
				text.Render("  "+wrapText(p.Description, width-4)))
		}
		_ = height
		return strings.Join(parts, "\n")
	case appModeAvailable:
		rows := a.visibleAvailable()
		if a.base().Cursor() >= len(rows) {
			return ""
		}
		p := rows[a.base().Cursor()]
		pid := p.Identifier()
		state := "available"
		for _, ip := range a.installed {
			if ip.ID == pid {
				state = "installed"
				break
			}
		}
		if a.installing[pid] {
			state = "installing…"
		}
		parts := []string{
			t.Title().Render(" " + p.DisplayName() + " "),
			"",
			muted.Render(pid),
			"",
			muted.Render("Version:    ") + text.Render(p.Version),
			muted.Render("Maintainer: ") + text.Render(p.Maintainer),
			muted.Render("State:      ") + t.HealthStyle(state).Render(state),
		}
		if p.Size > 0 {
			parts = append(parts, muted.Render("Size:       ")+text.Render(HumanBytes(uint64(p.Size))))
		}
		if p.Description != "" {
			parts = append(parts, "", muted.Render("Description"),
				text.Render("  "+wrapText(p.Description, width-4)))
		}
		_ = height
		return strings.Join(parts, "\n")
	case appModeServices:
		rows := a.visibleServices()
		if a.base().Cursor() >= len(rows) {
			return ""
		}
		s := rows[a.base().Cursor()]
		parts := []string{
			t.Title().Render(" " + s.DisplayName() + " "),
			"",
			muted.Render(s.ID),
			"",
			muted.Render("State:      ") + t.HealthStyle(s.EnableStatus).Render(s.EnableStatus),
			muted.Render("Togglable:  ") + text.Render(yesNo(s.Toggleable())),
		}
		if !s.Toggleable() {
			parts = append(parts, "", muted.Render("DSM runs this service unconditionally."))
		}
		_ = height
		return strings.Join(parts, "\n")
	}
	return ""
}

// bottomHint is retained for the help overlay only — the active hint
// strip uses Hint() which the shell renders globally. Kept in sync so
// the two never drift.
func (a *Apps) bottomHint() string { return a.Hint() }

// itoa avoids importing strconv just for an int → string conversion.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	digits := [20]byte{}
	i := len(digits)
	for n > 0 {
		i--
		digits[i] = '0' + byte(n%10)
		n /= 10
	}
	if neg {
		i--
		digits[i] = '-'
	}
	return string(digits[i:])
}

// wrapText word-wraps to width.
func wrapText(s string, width int) string {
	if width <= 10 {
		return s
	}
	words := strings.Fields(s)
	var b strings.Builder
	col := 0
	for _, w := range words {
		if col+len(w)+1 > width {
			b.WriteByte('\n')
			col = 0
		}
		if col > 0 {
			b.WriteByte(' ')
			col++
		}
		b.WriteString(w)
		col += len(w)
	}
	return b.String()
}
