package views

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Usage is the disk-usage analyzer. It renders an expandable "what is big"
// tree:
//
//   - At the root it lists FileStation shares ordered by used space.
//   - Expanding a directory inserts immediate children underneath it. Files
//     use their list metadata; folders are sized asynchronously via
//     SYNO.FileStation.DirSize.
//   - Results are cached for the duration of the TUI session.
//   - Concurrency is gentle (2 in-flight DirSize calls) so the box
//     doesn't get hammered.
//
// Pressing `e` toggles "extension breakdown" mode — aggregates currently
// visible files into one row per extension, sorted by total size.
type Usage struct {
	ctx Ctx

	// Per-path cache. sizeCache keys are absolute DSM paths; values are
	// directory totals returned by DSM.
	cache *usageCache

	// Root share rows. Expanded child rows live in children.
	entries []usageEntry

	children    map[string][]usageEntry
	childErr    map[string]error
	expanded    map[string]bool
	loadingDirs map[string]bool

	base       listBase
	extensionM bool // 'e' — extension breakdown mode

	roots    []dsm.FileShare
	rootsErr error
}

// usageEntry is one row in the analyzer.
type usageEntry struct {
	Name   string
	Path   string
	IsDir  bool
	Size   int64
	Sized  bool   // true once we have a real size (file → always, dir → after DirSize)
	Sizing bool   // dir sizing in flight
	Err    error  // dir sizing failed
	Ext    string // for extension-breakdown rows
	Count  int    // file count for extension-breakdown rows
	Level  int
	Status string
}

// usageCache stores DirSize results so re-entering a folder is instant.
// Concurrent access is gated by a mutex — bubbletea cmds can land in any
// goroutine.
type usageCache struct {
	mu sync.RWMutex
	v  map[string]int64
}

func newUsageCache() *usageCache { return &usageCache{v: map[string]int64{}} }

func (c *usageCache) get(p string) (int64, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.v[p]
	return v, ok
}

func (c *usageCache) set(p string, v int64) {
	c.mu.Lock()
	c.v[p] = v
	c.mu.Unlock()
}

func (c *usageCache) delete(p string) {
	c.mu.Lock()
	delete(c.v, p)
	c.mu.Unlock()
}

// NewUsage constructs the analyzer view.
func NewUsage(c Ctx) tui.View {
	return &Usage{
		ctx:         c,
		cache:       newUsageCache(),
		children:    map[string][]usageEntry{},
		childErr:    map[string]error{},
		expanded:    map[string]bool{},
		loadingDirs: map[string]bool{},
	}
}

func (u *Usage) Name() string                   { return "usage" }
func (u *Usage) Title() string                  { return "Usage Analyzer" }
func (u *Usage) Icon() string                   { return "◴" }
func (u *Usage) RefreshInterval() time.Duration { return 0 }
func (u *Usage) IsTextEditing() bool            { return u.base.filter.IsActive() }
func (u *Usage) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("backspace", "h"), key.WithHelp("⌫/h", "collapse")),
		key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "extension breakdown")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "re-size current dir")),
	)
}

func (u *Usage) Init() tea.Cmd { return u.fetchRoots() }

// — fetches —

func (u *Usage) fetchRoots() tea.Cmd {
	c := u.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.FileShare, error) { return c.FileShares(ctx) },
		func(s []dsm.FileShare, err error) tea.Msg { return usageRootsMsg{R: s, Err: err} },
	)
}

// loadDir lists the immediate children of dirPath and kicks off DirSize for
// each subdirectory (unless already cached). Files are inserted with
// their known sizes.
func (u *Usage) loadDir(dirPath string) tea.Cmd {
	c := u.ctx.Client
	if c == nil {
		return nil
	}
	u.ensureTreeMaps()
	u.loadingDirs[dirPath] = true
	delete(u.childErr, dirPath)
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) ([]dsm.FSEntry, error) {
			items, _, err := c.ListFiles(ctx, dirPath, 0, 1000)
			return items, err
		},
		func(items []dsm.FSEntry, err error) tea.Msg {
			return usageListedMsg{Path: dirPath, Items: items, Err: err}
		},
	)
}

// dirSizeCmd kicks off a single DirSize call. We use a 5-minute timeout
// because DirSize on a multi-TiB share can be slow on a DS220j.
func (u *Usage) dirSizeCmd(dirPath string) tea.Cmd {
	c := u.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(5*time.Minute,
		func(ctx context.Context) (dsm.DirSizeResult, error) { return c.DirSize(ctx, dirPath) },
		func(r dsm.DirSizeResult, err error) tea.Msg {
			return usageSizedMsg{Path: dirPath, Size: r.Total, Err: err}
		},
	)
}

// kickPendingSizes starts up to maxInflight DirSize calls for unsized
// directories in u.entries. Returns a batch cmd.
//
// Concurrency is deliberately low (2) because DirSize is server-side
// expensive on low-end Synology boxes — running three of them in
// parallel against a DS220j saturates the CPU and makes everything
// else (including key input) feel laggy. The trade-off is the user
// sees results trickle in over more time, but the rest of the UI
// stays responsive.
func (u *Usage) kickPendingSizes() tea.Cmd {
	const maxInflight = 2
	var cmds []tea.Cmd
	inflight := u.countSizing()
	u.startPendingSizes(&u.entries, &cmds, &inflight, maxInflight)
	if inflight < maxInflight {
		for p, entries := range u.children {
			u.startPendingSizes(&entries, &cmds, &inflight, maxInflight)
			u.children[p] = entries
			if inflight >= maxInflight {
				break
			}
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (u *Usage) countSizing() int {
	total := 0
	for _, e := range u.entries {
		if e.Sizing {
			total++
		}
	}
	for _, entries := range u.children {
		for _, e := range entries {
			if e.Sizing {
				total++
			}
		}
	}
	return total
}

func (u *Usage) startPendingSizes(entries *[]usageEntry, cmds *[]tea.Cmd, inflight *int, maxInflight int) {
	for i := range *entries {
		e := &(*entries)[i]
		if !e.IsDir || e.Sized || e.Sizing || e.Err != nil {
			continue
		}
		if cached, ok := u.cache.get(e.Path); ok {
			e.Size = cached
			e.Sized = true
			continue
		}
		e.Sizing = true
		*cmds = append(*cmds, u.dirSizeCmd(e.Path))
		(*inflight)++
		if *inflight >= maxInflight {
			return
		}
	}
}

// — messages —

type usageRootsMsg struct {
	R   []dsm.FileShare
	Err error
}
type usageListedMsg struct {
	Path  string
	Items []dsm.FSEntry
	Err   error
}
type usageSizedMsg struct {
	Path string
	Size int64
	Err  error
}

// — update —

func (u *Usage) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	switch m := msg.(type) {
	case usageRootsMsg:
		u.roots, u.rootsErr = m.R, m.Err
		// Build entries from the share list so the same kickPendingSizes /
		// sizing flow that handles directories drives share sizing too.
		// VolStatus.{free,total} reports per-VOLUME numbers (so every share
		// on volume1 reports the same 831 GiB used) — never use it for
		// per-share usage. DirSize on the share path is the only reliable
		// source for "how big is this share actually".
		u.entries = u.entries[:0]
		if m.Err == nil {
			for _, sh := range m.R {
				e := usageEntry{
					Name:  sh.Name,
					Path:  sh.Path,
					IsDir: true,
					Sized: false,
				}
				if cached, ok := u.cache.get(sh.Path); ok {
					e.Size = cached
					e.Sized = true
				}
				u.entries = append(u.entries, e)
			}
			u.sortBySize()
		}
		u.base.ClampCursor(u.rowCount())
		return u, u.kickPendingSizes()
	case usageListedMsg:
		u.ensureTreeMaps()
		u.loadingDirs[m.Path] = false
		if m.Err != nil {
			u.childErr[m.Path] = m.Err
			return u, nil
		}
		delete(u.childErr, m.Path)
		children := make([]usageEntry, 0, len(m.Items))
		for _, e := range m.Items {
			ue := usageEntry{
				Name:  e.Name,
				Path:  e.Path,
				IsDir: e.IsDir,
				Size:  e.Add.Size,
				Sized: !e.IsDir,
			}
			if e.IsDir {
				if cached, ok := u.cache.get(e.Path); ok {
					ue.Size = cached
					ue.Sized = true
				}
			}
			children = append(children, ue)
		}
		u.children[m.Path] = children
		u.sortBySize()
		u.base.ClampCursor(u.rowCount())
		return u, u.kickPendingSizes()
	case usageSizedMsg:
		if m.Err == nil {
			u.cache.set(m.Path, m.Size)
		}
		u.applySized(m)
		u.sortBySize()
		return u, u.kickPendingSizes()
	}

	if _, handled := u.base.HandleKey(msg, u.rowCount()); handled {
		return u, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			return u, u.drillDown()
		case "backspace", "h":
			return u, u.upDir()
		case "esc":
			return u, u.upDir()
		case "e":
			u.extensionM = !u.extensionM
			return u, nil
		case "R":
			u.forgetVisibleSizes()
			return u, u.kickPendingSizes()
		}
	}
	return u, nil
}

func (u *Usage) drillDown() tea.Cmd {
	rows := u.viewRows()
	if u.base.Cursor() >= len(rows) {
		return nil
	}
	e := rows[u.base.Cursor()]
	if !e.IsDir || e.Status != "" || u.extensionM {
		return nil
	}
	u.ensureTreeMaps()
	if u.expanded[e.Path] {
		delete(u.expanded, e.Path)
		u.base.ClampCursor(u.rowCount())
		return nil
	}
	u.expanded[e.Path] = true
	if _, ok := u.children[e.Path]; ok || u.loadingDirs[e.Path] {
		return nil
	}
	return u.loadDir(e.Path)
}

func (u *Usage) upDir() tea.Cmd {
	rows := u.viewRows()
	if u.base.Cursor() >= len(rows) {
		return nil
	}
	row := rows[u.base.Cursor()]
	if row.IsDir && row.Status == "" && u.expanded[row.Path] {
		delete(u.expanded, row.Path)
		u.base.ClampCursor(u.rowCount())
		return nil
	}
	for i := u.base.Cursor() - 1; i >= 0; i-- {
		parent := rows[i]
		if parent.Level >= row.Level || !parent.IsDir || parent.Status != "" {
			continue
		}
		delete(u.expanded, parent.Path)
		u.base.cursor = i
		u.base.ClampCursor(u.rowCount())
		return nil
	}
	return nil
}

func (u *Usage) applySized(m usageSizedMsg) {
	apply := func(entries []usageEntry) bool {
		for i := range entries {
			if entries[i].Path != m.Path {
				continue
			}
			entries[i].Sizing = false
			if m.Err != nil {
				entries[i].Err = m.Err
			} else {
				entries[i].Size = m.Size
				entries[i].Sized = true
			}
			return true
		}
		return false
	}
	if apply(u.entries) {
		return
	}
	for p, entries := range u.children {
		if apply(entries) {
			u.children[p] = entries
			return
		}
	}
}

func (u *Usage) forgetVisibleSizes() {
	reset := func(entries []usageEntry) {
		for i := range entries {
			if !entries[i].IsDir || entries[i].Status != "" {
				continue
			}
			u.cache.delete(entries[i].Path)
			entries[i].Size = 0
			entries[i].Sized = false
			entries[i].Sizing = false
			entries[i].Err = nil
		}
	}
	reset(u.entries)
	for p, entries := range u.children {
		reset(entries)
		u.children[p] = entries
	}
}

// sortBySize sorts the entries by descending size, but only once every
// directory has settled. Resorting while DirSize results are still
// trickling in makes the list jump under the cursor — what looks like
// lag is actually rows reshuffling. We keep the input order (files
// first, alphabetical) until everything is sized, then do one final
// sort.
func (u *Usage) sortBySize() {
	sortUsageEntries(u.entries)
	for p, entries := range u.children {
		sortUsageEntries(entries)
		u.children[p] = entries
	}
}

func sortUsageEntries(entries []usageEntry) {
	for _, e := range entries {
		if e.Sizing {
			return
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Size > entries[j].Size
	})
}

func (u *Usage) rowCount() int { return len(u.viewRows()) }

// dirRows returns the regular (non-extension) entries.
func (u *Usage) dirRows() []usageEntry {
	return u.flattenUsageRows(u.base.FilterValue())
}

func (u *Usage) ensureTreeMaps() {
	if u.children == nil {
		u.children = map[string][]usageEntry{}
	}
	if u.childErr == nil {
		u.childErr = map[string]error{}
	}
	if u.expanded == nil {
		u.expanded = map[string]bool{}
	}
	if u.loadingDirs == nil {
		u.loadingDirs = map[string]bool{}
	}
}

func (u *Usage) flattenUsageRows(filter string) []usageEntry {
	out := make([]usageEntry, 0, len(u.entries))
	for _, e := range u.entries {
		out = append(out, u.flattenUsageEntry(e, 0, filter)...)
	}
	return out
}

func (u *Usage) flattenUsageEntry(e usageEntry, level int, filter string) []usageEntry {
	row := e
	row.Level = level
	row.Status = ""
	childRows := u.flattenUsageChildren(e.Path, level+1, filter)
	selfMatch := filter == "" || MatchesAll(filter, row.Name, row.Path)
	if filter != "" && !selfMatch && len(childRows) == 0 {
		return nil
	}
	return append([]usageEntry{row}, childRows...)
}

func (u *Usage) flattenUsageChildren(parent string, level int, filter string) []usageEntry {
	if !u.expanded[parent] {
		return nil
	}
	var out []usageEntry
	if filter == "" {
		if u.loadingDirs[parent] {
			out = append(out, usageEntry{Level: level, Status: "listing..."})
		}
		if err := u.childErr[parent]; err != nil {
			out = append(out, usageEntry{Level: level, Status: err.Error(), Err: err})
		}
	}
	children, ok := u.children[parent]
	if !ok {
		return out
	}
	if filter == "" && len(children) == 0 && !u.loadingDirs[parent] && u.childErr[parent] == nil {
		out = append(out, usageEntry{Level: level, Status: "(empty)"})
		return out
	}
	for _, child := range children {
		out = append(out, u.flattenUsageEntry(child, level, filter)...)
	}
	return out
}

// viewRows returns either the directory entries or the extension breakdown,
// depending on mode.
func (u *Usage) viewRows() []usageEntry {
	if !u.extensionM {
		return u.dirRows()
	}
	// Extension mode: aggregate file extensions of visible files.
	totals := map[string]*usageEntry{}
	for _, e := range u.flattenUsageRows("") {
		if e.IsDir || e.Status != "" {
			continue
		}
		ext := strings.ToLower(path.Ext(e.Name))
		if ext == "" {
			ext = "(no ext)"
		}
		if cur, ok := totals[ext]; ok {
			cur.Size += e.Size
			cur.Count++
		} else {
			totals[ext] = &usageEntry{
				Name:  ext,
				Ext:   ext,
				Size:  e.Size,
				Count: 1,
				Sized: true,
			}
		}
	}
	out := make([]usageEntry, 0, len(totals))
	for _, v := range totals {
		out = append(out, *v)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

// — render —

func (u *Usage) Render(width, height int) string {
	t := u.ctx.Theme
	var parts []string
	parts = append(parts, u.renderBreadcrumb(width))
	parts = append(parts, u.renderEntries(width)...)
	parts = append(parts, "")
	help := "  ⏎ expand · ⌫ collapse · e ext breakdown · R re-size · / filter"
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(help))
	if f := u.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (u *Usage) renderBreadcrumb(width int) string {
	t := u.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	accent := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	_ = width
	return accent.Render(" usage / ") + muted.Render("(usage tree)")
}

// renderEntries renders the flattened usage tree with proportional size bars.
// The "Sizing…" placeholder shows for in-flight DirSize calls.
func (u *Usage) renderEntries(width int) []string {
	t := u.ctx.Theme
	rows := u.viewRows()
	title := "Tree (largest first)"
	if u.extensionM {
		title += " · by extension"
	}
	out := []string{sectionHeader(t, width, title, len(rows), u.rootsErr)}
	if u.roots == nil && u.rootsErr == nil {
		out = append(out, "  "+muted(t, "loading shares…"))
		return out
	}
	if len(rows) == 0 {
		out = append(out, "  "+muted(t, "(empty)"))
		return out
	}
	var maxSize int64
	for _, e := range rows {
		if e.Size > maxSize {
			maxSize = e.Size
		}
	}
	for i, e := range rows {
		out = append(out, u.renderDirRow(width, e, maxSize, i == u.base.Cursor()))
	}
	// Footer summary.
	var total int64
	for _, e := range u.entries {
		total += e.Size
	}
	summary := fmt.Sprintf("  total %s · %d rows · %d sizing", HumanBytes(uint64(total)), len(rows), u.countSizing())
	out = append(out, "", lipgloss.NewStyle().Foreground(t.Muted).Render(summary))
	return out
}

func (u *Usage) renderDirRow(width int, e usageEntry, maxSize int64, highlight bool) string {
	t := u.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	accent := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	indent := strings.Repeat("  ", e.Level)
	if e.Status != "" {
		style := muted
		if e.Err != nil {
			style = lipgloss.NewStyle().Foreground(t.Error)
		}
		return lipgloss.JoinHorizontal(lipgloss.Center,
			caretGlyph(t, highlight), " ",
			indent, "  ", style.Render(e.Status),
		)
	}
	barW := max(width-60, 20)
	ratio := 0.0
	if maxSize > 0 {
		ratio = float64(e.Size) / float64(maxSize)
	}
	expander := " "
	icon := "  "
	if e.IsDir {
		if u.expanded[e.Path] {
			expander = "▾"
		} else {
			expander = "▸"
		}
		icon = "📁"
	} else if e.Ext != "" {
		icon = "·"
	} else {
		icon = fileIcon(e.Name)
	}
	name := e.Name
	if e.IsDir {
		name = accent.Render(name)
	} else if e.Ext != "" {
		name = text.Render(name) + muted.Render(fmt.Sprintf("  (%d files)", e.Count))
	} else {
		name = text.Render(name)
	}
	size := "—"
	switch {
	case e.Err != nil:
		size = lipgloss.NewStyle().Foreground(t.Error).Render("err")
	case e.Sizing:
		size = muted.Render("sizing…")
	case e.Sized:
		size = text.Render(HumanBytes(uint64(e.Size)))
	}
	nameWidth := max(36-e.Level*2, 14)
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		indent, expander, " ", icon, " ",
		padRight(name, nameWidth), " ",
		Gauge(t, barW, ratio), " ",
		padLeft(size, 12),
	)
}

// Inspect renders a compact summary of the cursor'd row in the inspector.
func (u *Usage) Inspect(width, height int) string {
	t := u.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	rows := u.viewRows()
	if u.base.Cursor() >= len(rows) {
		return ""
	}
	e := rows[u.base.Cursor()]
	if e.Status != "" {
		return muted.Render(e.Status)
	}
	parts := []string{
		t.Title().Render(" " + e.Name + " "),
		"",
		muted.Render(e.Path),
		"",
		muted.Render("Size:    ") + text.Render(HumanBytes(uint64(e.Size))),
	}
	if e.IsDir {
		parts = append(parts, muted.Render("Type:    ")+text.Render("directory"))
	} else if e.Ext != "" {
		parts = append(parts, muted.Render("Files:   ")+text.Render(fmt.Sprintf("%d", e.Count)))
	} else {
		parts = append(parts, muted.Render("Type:    ")+text.Render(strings.TrimPrefix(path.Ext(e.Name), ".")))
	}
	if e.Sizing {
		parts = append(parts, "", muted.Render("sizing in progress…"))
	}
	if e.Err != nil {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(t.Error).Render(e.Err.Error()))
	}
	_ = height
	return strings.Join(parts, "\n")
}
