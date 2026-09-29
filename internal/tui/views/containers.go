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

// containerMode is which tab the unified Containers view is showing.
// The view mirrors the Apps pattern: one screen, three modes cycled with
// `t` or jumped via `1`/`2`/`3`, each owning its own cursor + filter
// state so switching tabs preserves the user's place.
type containerMode int

const (
	containerModeContainers containerMode = iota
	containerModeImages
	containerModeNetworks
)

func (m containerMode) String() string {
	switch m {
	case containerModeContainers:
		return "Containers"
	case containerModeImages:
		return "Images"
	case containerModeNetworks:
		return "Networks"
	}
	return "?"
}

// containersMsg / dockerImagesMsg / dockerNetworksMsg are the Fetch
// completion messages — one per data source.
type containersMsg struct {
	C   []dsm.Container
	Err error
}
type dockerImagesMsg struct {
	I   []dsm.Image
	Err error
}
type dockerNetworksMsg struct {
	N   []dsm.DockerNetwork
	Err error
}

// ContainersView is the tabbed Container Manager overview.
//
// Three modes — Containers / Images / Networks — replace the previous
// stacked single-list layout which crammed all three concepts into one
// long scrolling page. Each mode has its own listBase (cursor + filter)
// so the user can /-filter images by repo without losing their place in
// the Containers tab.
type ContainersView struct {
	ctx Ctx

	mode containerMode

	containers []dsm.Container
	images     []dsm.Image
	networks   []dsm.DockerNetwork

	cErr, iErr, nErr error

	bases [3]listBase

	// Detail overlay state — at most one is non-nil at a time.
	detailContainer *dsm.Container
	detailImage     *dsm.Image
	detailNetwork   *dsm.DockerNetwork
}

// NewContainers constructs the tabbed Containers view.
func NewContainers(c Ctx) tui.View { return &ContainersView{ctx: c} }

func (v *ContainersView) Name() string                   { return "containers" }
func (v *ContainersView) Title() string                  { return "Containers" }
func (v *ContainersView) Icon() string                   { return "▦" }
func (v *ContainersView) RefreshInterval() time.Duration { return 30 * time.Second }

func (v *ContainersView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "next mode")),
		key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "containers")),
		key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "images")),
		key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "networks")),
		key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	)
}

// Hint implements tui.Hinter. The bottom strip surfaces the keys that
// actually do something on the active tab.
func (v *ContainersView) Hint() string {
	switch v.mode {
	case containerModeContainers:
		return "t mode · 1/2/3 jump · ⏎ details · / filter · r refresh"
	case containerModeImages:
		return "t mode · 1/2/3 jump · ⏎ details · / filter · r refresh"
	case containerModeNetworks:
		return "t mode · 1/2/3 jump · ⏎ details · / filter · r refresh"
	}
	return ""
}

// IsTextEditing defers global keys while the inline filter owns input.
func (v *ContainersView) IsTextEditing() bool { return v.base().filter.IsActive() }

func (v *ContainersView) Init() tea.Cmd {
	return tea.Batch(v.fetchContainers(), v.fetchImages(), v.fetchNetworks())
}

// — fetches —

func (v *ContainersView) fetchContainers() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Container, error) { return c.Containers(ctx) },
		func(x []dsm.Container, err error) tea.Msg { return containersMsg{C: x, Err: err} },
	)
}

func (v *ContainersView) fetchImages() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Image, error) { return c.DockerImages(ctx) },
		func(x []dsm.Image, err error) tea.Msg { return dockerImagesMsg{I: x, Err: err} },
	)
}

func (v *ContainersView) fetchNetworks() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.DockerNetwork, error) { return c.DockerNetworks(ctx) },
		func(x []dsm.DockerNetwork, err error) tea.Msg { return dockerNetworksMsg{N: x, Err: err} },
	)
}

// — visibility helpers (filtered per mode) —

func (v *ContainersView) visibleContainers() []dsm.Container {
	f := v.bases[containerModeContainers].FilterValue()
	if f == "" {
		return v.containers
	}
	out := make([]dsm.Container, 0, len(v.containers))
	for _, c := range v.containers {
		if MatchesAll(f, c.ID, c.Name, c.Image, c.Status, c.State) {
			out = append(out, c)
		}
	}
	return out
}

func (v *ContainersView) visibleImages() []dsm.Image {
	f := v.bases[containerModeImages].FilterValue()
	if f == "" {
		return v.images
	}
	out := make([]dsm.Image, 0, len(v.images))
	for _, i := range v.images {
		if MatchesAll(f, i.ID, i.Repository, i.Tag, i.RepoTag, i.Description) {
			out = append(out, i)
		}
	}
	return out
}

func (v *ContainersView) visibleNetworks() []dsm.DockerNetwork {
	f := v.bases[containerModeNetworks].FilterValue()
	if f == "" {
		return v.networks
	}
	out := make([]dsm.DockerNetwork, 0, len(v.networks))
	for _, n := range v.networks {
		if MatchesAll(f, n.ID, n.Name, n.Driver, n.Subnet, n.Gateway) {
			out = append(out, n)
		}
	}
	return out
}

func (v *ContainersView) visibleCount() int {
	switch v.mode {
	case containerModeContainers:
		return len(v.visibleContainers())
	case containerModeImages:
		return len(v.visibleImages())
	case containerModeNetworks:
		return len(v.visibleNetworks())
	}
	return 0
}

func (v *ContainersView) base() *listBase { return &v.bases[v.mode] }

// — mode switching —

func (v *ContainersView) switchMode(m containerMode) {
	v.mode = m
}

// — update —

func (v *ContainersView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchContainers(), v.fetchImages(), v.fetchNetworks())
	case containersMsg:
		v.containers, v.cErr = m.C, m.Err
		v.bases[containerModeContainers].ClampCursor(len(v.visibleContainers()))
		return v, nil
	case dockerImagesMsg:
		v.images, v.iErr = m.I, m.Err
		v.bases[containerModeImages].ClampCursor(len(v.visibleImages()))
		return v, nil
	case dockerNetworksMsg:
		v.networks, v.nErr = m.N, m.Err
		v.bases[containerModeNetworks].ClampCursor(len(v.visibleNetworks()))
		return v, nil
	}

	// Detail overlay swallows everything except esc/q.
	if v.anyDetailOpen() {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				v.closeDetail()
				return v, nil
			}
		}
		return v, nil
	}

	// Forward to listBase for cursor + filter editing.
	if _, handled := v.base().HandleKey(msg, v.visibleCount()); handled {
		return v, nil
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "t":
			v.switchMode(containerMode((int(v.mode) + 1) % 3))
			return v, nil
		case "1":
			v.switchMode(containerModeContainers)
			return v, nil
		case "2":
			v.switchMode(containerModeImages)
			return v, nil
		case "3":
			v.switchMode(containerModeNetworks)
			return v, nil
		case "r":
			switch v.mode {
			case containerModeContainers:
				return v, v.fetchContainers()
			case containerModeImages:
				return v, v.fetchImages()
			case containerModeNetworks:
				return v, v.fetchNetworks()
			}
		case "enter":
			v.openDetail()
		}
	}
	return v, nil
}

func (v *ContainersView) anyDetailOpen() bool {
	return v.detailContainer != nil || v.detailImage != nil || v.detailNetwork != nil
}

func (v *ContainersView) closeDetail() {
	v.detailContainer, v.detailImage, v.detailNetwork = nil, nil, nil
}

func (v *ContainersView) openDetail() {
	switch v.mode {
	case containerModeContainers:
		rows := v.visibleContainers()
		if v.base().Cursor() < len(rows) {
			c := rows[v.base().Cursor()]
			v.detailContainer = &c
		}
	case containerModeImages:
		rows := v.visibleImages()
		if v.base().Cursor() < len(rows) {
			i := rows[v.base().Cursor()]
			v.detailImage = &i
		}
	case containerModeNetworks:
		rows := v.visibleNetworks()
		if v.base().Cursor() < len(rows) {
			n := rows[v.base().Cursor()]
			v.detailNetwork = &n
		}
	}
}

// — render —

func (v *ContainersView) Render(width, height int) string {
	t := v.ctx.Theme
	switch {
	case v.detailContainer != nil:
		return renderContainerDetail(t, width, *v.detailContainer)
	case v.detailImage != nil:
		return renderDockerImageDetail(t, width, *v.detailImage)
	case v.detailNetwork != nil:
		return renderDockerNetworkDetail(t, width, *v.detailNetwork)
	}

	// Empty-state when Container Manager isn't installed at all.
	if v.containers != nil && v.images != nil && v.networks != nil &&
		len(v.containers) == 0 && len(v.images) == 0 && len(v.networks) == 0 &&
		v.cErr == nil && v.iErr == nil && v.nErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"▦  Container Manager",
			"Container Manager isn't installed, or no containers / images / networks exist yet.",
			"Install Container Manager from Package Center to see your containers here."), height)
	}

	var parts []string
	parts = append(parts, v.renderTabs(width))
	parts = append(parts, "")

	switch v.mode {
	case containerModeContainers:
		parts = append(parts, v.renderContainers(width)...)
	case containerModeImages:
		parts = append(parts, v.renderImages(width)...)
	case containerModeNetworks:
		parts = append(parts, v.renderNetworks(width)...)
	}

	if f := v.base().FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

// renderTabs draws the top tab strip: `▎ Active (N)` for the focused
// tab, `  Idle (N)` for the others, with a faint horizontal rule
// underneath. Matches the Apps view exactly so the two screens feel
// consistent.
func (v *ContainersView) renderTabs(width int) string {
	t := v.ctx.Theme
	muStyle := lipgloss.NewStyle().Foreground(t.Muted)
	idle := lipgloss.NewStyle().Foreground(t.Text)
	active := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)

	tab := func(m containerMode, count int, loading bool) string {
		label := m.String()
		if loading {
			label += " (…)"
		} else if count >= 0 {
			label += " (" + itoaShort(count) + ")"
		}
		if m == v.mode {
			return "▎ " + active.Render(label)
		}
		return "  " + idle.Render(label)
	}

	cTab := tab(containerModeContainers, len(v.containers), v.containers == nil && v.cErr == nil)
	iTab := tab(containerModeImages, len(v.images), v.images == nil && v.iErr == nil)
	nTab := tab(containerModeNetworks, len(v.networks), v.networks == nil && v.nErr == nil)

	row := strings.Join([]string{cTab, iTab, nTab}, "   ")
	rule := muStyle.Render(strings.Repeat("─", maxInt(width-2, 0)))
	return row + "\n" + rule
}

// — per-mode list bodies —

func (v *ContainersView) renderContainers(width int) []string {
	t := v.ctx.Theme
	rows := v.visibleContainers()
	out := []string{sectionHeader(t, width, "Containers", len(rows), v.cErr)}
	if v.containers == nil && v.cErr == nil {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	// Compute data-driven column widths so long names / images don't
	// shove the rest of the row off-screen.
	nameW, imageW := 12, 20
	for _, c := range rows {
		if w := lipgloss.Width(c.Name); w > nameW {
			nameW = w
		}
		if w := lipgloss.Width(c.Image); w > imageW {
			imageW = w
		}
	}
	// Reserve space for caret(2) + name + image + cpu(7) + mem(10) + status + gaps.
	// Cap image width so the status chip stays on-screen.
	if imageW > 48 {
		imageW = 48
	}
	if nameW > 28 {
		nameW = 28
	}
	// Shrink the image column on narrow panes so the status stays visible.
	imageW = max(min(imageW, width-(2+nameW+1+1+8+11+10)), 8)
	for i, c := range rows {
		out = append(out, v.renderContainerRow(c, i == v.base().Cursor(), nameW, imageW))
	}
	return out
}

func (v *ContainersView) renderImages(width int) []string {
	t := v.ctx.Theme
	rows := v.visibleImages()
	out := []string{sectionHeader(t, width, "Images", len(rows), v.iErr)}
	if v.images == nil && v.iErr == nil {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	// Compute repo + tag widths from data — long repos like
	// ghcr.io/gethomepage/homepage shouldn't push the size column off
	// the right edge.
	repoW, tagW := 16, 8
	for _, i := range rows {
		repo, tag := imageRepoTag(i)
		if w := lipgloss.Width(repo); w > repoW {
			repoW = w
		}
		if w := lipgloss.Width(tag); w > tagW {
			tagW = w
		}
	}
	if repoW > 44 {
		repoW = 44
	}
	if tagW > 20 {
		tagW = 20
	}
	repoW = max(min(repoW, width-(2+1+tagW+1+10+1+12+1+6+1)), 8)
	for i, img := range rows {
		out = append(out, v.renderImageRow(img, i == v.base().Cursor(), repoW, tagW))
	}
	return out
}

func (v *ContainersView) renderNetworks(width int) []string {
	_ = width
	t := v.ctx.Theme
	rows := v.visibleNetworks()
	out := []string{sectionHeader(t, width, "Networks", len(rows), v.nErr)}
	if v.networks == nil && v.nErr == nil {
		out = append(out, "  "+muted(t, "loading…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
		return out
	}
	nameW, drvW, subW, gwW := 12, 6, 16, 14
	for _, n := range rows {
		if w := lipgloss.Width(n.Name); w > nameW {
			nameW = w
		}
		if w := lipgloss.Width(n.Driver); w > drvW {
			drvW = w
		}
		if w := lipgloss.Width(n.Subnet); w > subW {
			subW = w
		}
		if w := lipgloss.Width(n.Gateway); w > gwW {
			gwW = w
		}
	}
	if nameW > 28 {
		nameW = 28
	}
	if drvW > 14 {
		drvW = 14
	}
	if subW > 24 {
		subW = 24
	}
	if gwW > 20 {
		gwW = 20
	}
	for i, n := range rows {
		out = append(out, v.renderNetworkRow(n, i == v.base().Cursor(), nameW, drvW, subW, gwW))
	}
	return out
}

// — row renderers —

func (v *ContainersView) renderContainerRow(c dsm.Container, highlight bool, nameW, imageW int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	status := c.Status
	if status == "" {
		status = c.State
	}
	cpu := fmt.Sprintf("%4.1f%%", c.CPU)
	mem := HumanBytes(uint64(c.Memory))
	if c.Memory == 0 {
		mem = "—"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(c.Name, nameW)), nameW), " ",
		padRight(mu.Render(clipTo(c.Image, imageW)), imageW), " ",
		padLeft(mu.Render(cpu), 7), " ",
		padLeft(mu.Render(mem), 10), " ",
		t.HealthStyle(status).Render(status),
	)
}

func (v *ContainersView) renderImageRow(i dsm.Image, highlight bool, repoW, tagW int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	repo, tag := imageRepoTag(i)
	used := "unused"
	if i.InUse.Bool() || i.Containers > 0 {
		used = "in use"
	}
	created := "—"
	if i.Created > 0 {
		created = time.Unix(i.Created, 0).Format("2006-01-02")
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(repo, repoW)), repoW), " ",
		padRight(mu.Render(clipTo(tag, tagW)), tagW), " ",
		padLeft(mu.Render(HumanBytes(uint64(i.Size))), 10), " ",
		padRight(mu.Render(created), 12), " ",
		t.HealthStyle(used).Render(used),
	)
}

func (v *ContainersView) renderNetworkRow(n dsm.DockerNetwork, highlight bool, nameW, drvW, subW, gwW int) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(n.Name, nameW)), nameW), " ",
		padRight(mu.Render(clipTo(n.Driver, drvW)), drvW), " ",
		padRight(mu.Render(clipTo(n.Subnet, subW)), subW), " ",
		padRight(mu.Render(clipTo(n.Gateway, gwW)), gwW), " ",
		padLeft(mu.Render(fmt.Sprintf("%d ctrs", n.Containers)), 10),
	)
}

// imageRepoTag splits an image into (repository, tag) for display.
// Prefers the separately-modelled Repository field but falls back to
// the joined RepoTag (DSM 7.0.1 sometimes returns only that). We
// deliberately never display ":latest" with an empty repo prefix — a
// bare "latest" is a UX bug we hit in 0.1.x.
func imageRepoTag(i dsm.Image) (string, string) {
	if i.Repository != "" {
		tag := i.Tag
		if tag == "" {
			tag = "—"
		}
		return i.Repository, tag
	}
	// Fallback: parse RepoTag (e.g. "ghcr.io/foo/bar:v1.2").
	rt := i.RepoTag
	if rt == "" {
		// Last resort — short id, no tag.
		return clipTo(i.ID, 12), "—"
	}
	if idx := strings.LastIndex(rt, ":"); idx > 0 && !strings.Contains(rt[idx:], "/") {
		return rt[:idx], rt[idx+1:]
	}
	return rt, coalesce(i.Tag, "—")
}

// Inspect implements tui.Inspector: render the cursored row of the
// active tab in the right pane so the operator can read full ids,
// digests, subnets without leaving the list.
func (v *ContainersView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	switch v.mode {
	case containerModeContainers:
		rows := v.visibleContainers()
		if v.base().Cursor() >= len(rows) {
			return muted(t, "  (no selection)")
		}
		return renderContainerInspect(t, width, rows[v.base().Cursor()])
	case containerModeImages:
		rows := v.visibleImages()
		if v.base().Cursor() >= len(rows) {
			return muted(t, "  (no selection)")
		}
		return renderDockerImageInspect(t, width, rows[v.base().Cursor()])
	case containerModeNetworks:
		rows := v.visibleNetworks()
		if v.base().Cursor() >= len(rows) {
			return muted(t, "  (no selection)")
		}
		return renderDockerNetworkInspect(t, width, rows[v.base().Cursor()])
	}
	return ""
}

// — detail renderers (unchanged from the previous design) —

func renderContainerDetail(t tui.Theme, width int, c dsm.Container) string {
	if width < 60 {
		width = 60
	}
	status := c.Status
	if status == "" {
		status = c.State
	}
	parts := []string{
		hero(t, width, "▦", c.Name, status, c.Image),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", c.ID},
			{"Name", c.Name},
			{"Image", c.Image},
			{"Status", status},
			{"Command", c.Command},
			{"Created", c.CreatedAt},
			{"Started", c.StartedAt},
			{"Finished", c.FinishedAt},
			{"CPU", fmt.Sprintf("%.2f%%", c.CPU)},
			{"Memory", HumanBytes(uint64(c.Memory))},
			{"Mem %", fmt.Sprintf("%.2f%%", c.MemoryPct)},
			{"Net up", HumanBytes(uint64(c.NetworkUp))},
			{"Net down", HumanBytes(uint64(c.NetworkDown))},
			{"Block in", HumanBytes(uint64(c.BlockIn))},
			{"Block out", HumanBytes(uint64(c.BlockOut))},
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
		chip("enabled", c.Enabled.Bool()),
		chip("package container", c.IsPackage.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · read-only — synoctl doesn't run container actions yet"))
	return strings.Join(parts, "\n")
}

func renderDockerImageDetail(t tui.Theme, width int, i dsm.Image) string {
	if width < 60 {
		width = 60
	}
	repo, tag := imageRepoTag(i)
	used := "unused"
	if i.InUse.Bool() || i.Containers > 0 {
		used = "in use"
	}
	created := ""
	if i.Created > 0 {
		created = time.Unix(i.Created, 0).Format("2006-01-02 15:04")
	}
	parts := []string{
		hero(t, width, "⬚", repo, used, tag),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", i.ID},
			{"Repository", i.Repository},
			{"Tag", i.Tag},
			{"RepoTag", i.RepoTag},
			{"Size", HumanBytes(uint64(i.Size))},
			{"Virtual size", HumanBytes(uint64(i.VirtualSize))},
			{"Created", created},
			{"Containers", fmt.Sprintf("%d", i.Containers)},
		}),
	}
	if i.Description != "" {
		parts = append(parts, t.Card(false).Width(width-2).Render(
			t.Title().Render(" Description ")+"\n"+
				wrap(lipgloss.NewStyle().Foreground(t.Text), i.Description, width-6)))
	}
	parts = append(parts, noteCard(t, width, "  esc to go back"))
	return strings.Join(parts, "\n")
}

func renderDockerNetworkDetail(t tui.Theme, width int, n dsm.DockerNetwork) string {
	if width < 60 {
		width = 60
	}
	scope := n.Scope
	if scope == "" {
		scope = "local"
	}
	parts := []string{
		hero(t, width, "≋", n.Name, scope, n.Driver),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", n.ID},
			{"Name", n.Name},
			{"Driver", n.Driver},
			{"Scope", n.Scope},
			{"Subnet", n.Subnet},
			{"Gateway", n.Gateway},
			{"IP range", n.IPRange},
			{"Containers", fmt.Sprintf("%d", n.Containers)},
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
		chip("IPv6", n.EnableIPv6.Bool()),
		chip("internal", n.Internal.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back"))
	return strings.Join(parts, "\n")
}

func renderContainerInspect(t tui.Theme, width int, c dsm.Container) string {
	status := c.Status
	if status == "" {
		status = c.State
	}
	parts := []string{
		t.Title().Render(" Container ") + "\n" +
			lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  "+c.Name) + "\n" +
			lipgloss.NewStyle().Foreground(t.Muted).Render("  "+clipTo(c.Image, width-4)) + "\n" +
			"  " + t.HealthStyle(status).Render(status),
	}
	parts = append(parts, "",
		muted(t, "  ID")+"\n  "+clipTo(c.ID, width-4),
		muted(t, "  Command")+"\n  "+clipTo(c.Command, width-4),
		"",
		muted(t, "  CPU       ")+fmt.Sprintf("%5.1f%%", c.CPU),
		muted(t, "  Memory    ")+HumanBytes(uint64(c.Memory)),
		muted(t, "  Mem %     ")+fmt.Sprintf("%5.1f%%", c.MemoryPct),
		muted(t, "  Net up    ")+HumanBytes(uint64(c.NetworkUp)),
		muted(t, "  Net down  ")+HumanBytes(uint64(c.NetworkDown)),
	)
	return strings.Join(parts, "\n")
}

func renderDockerImageInspect(t tui.Theme, width int, i dsm.Image) string {
	repo, tag := imageRepoTag(i)
	created := "—"
	if i.Created > 0 {
		created = time.Unix(i.Created, 0).Format("2006-01-02 15:04")
	}
	return strings.Join([]string{
		t.Title().Render(" Image "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + clipTo(repo, width-4)),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + tag),
		"",
		muted(t, "  ID") + "\n  " + clipTo(i.ID, width-4),
		"",
		muted(t, "  Size       ") + HumanBytes(uint64(i.Size)),
		muted(t, "  Virtual    ") + HumanBytes(uint64(i.VirtualSize)),
		muted(t, "  Containers ") + fmt.Sprintf("%d", i.Containers),
		muted(t, "  Created    ") + created,
	}, "\n")
}

func renderDockerNetworkInspect(t tui.Theme, width int, n dsm.DockerNetwork) string {
	_ = width
	scope := n.Scope
	if scope == "" {
		scope = "local"
	}
	return strings.Join([]string{
		t.Title().Render(" Network "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + n.Name),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + n.Driver + " · " + scope),
		"",
		muted(t, "  Subnet   ") + n.Subnet,
		muted(t, "  Gateway  ") + n.Gateway,
		muted(t, "  IP range ") + n.IPRange,
		muted(t, "  Used by  ") + fmt.Sprintf("%d containers", n.Containers),
	}, "\n")
}

// emptyStateCard is the standard "not installed / nothing here yet"
// placeholder. We render it via the same card primitive as the rest of
// the views so it sits well next to populated rows on partial-empty
// pages too.
func emptyStateCard(t tui.Theme, width int, title, headline, hint string) string {
	body := lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  "+title) + "\n\n" +
		lipgloss.NewStyle().Foreground(t.Text).Render("  "+headline) + "\n" +
		lipgloss.NewStyle().Foreground(t.Faint).Render("  "+hint)
	return t.Card(false).Width(width - 2).Render(body)
}
