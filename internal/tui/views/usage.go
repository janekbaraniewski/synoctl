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

// Usage is the disk-usage analyzer. It mirrors ncdu's "drill in, see
// what's big" model:
//
//   - At the root it lists FileStation shares ordered by used space.
//   - Inside a directory it lists immediate children, sorted descending
//     by size. Files use their list metadata; folders are sized
//     asynchronously via SYNO.FileStation.DirSize.
//   - Results are cached for the duration of the TUI session.
//   - Concurrency is gentle (3 in-flight DirSize calls) so the box
//     doesn't get hammered.
//
// Pressing `e` toggles "extension breakdown" mode — aggregates files at
// the current level (and any sized descendants we've already walked) into
// one row per extension, sorted by total size.
type Usage struct {
	ctx Ctx

	cwd string // "" = share-list root

	// Per-path cache. sizeCache keys are absolute DSM paths; values are
	// directory totals returned by DSM.
	cache *usageCache

	// Current listing.
	entries []usageEntry
	loading bool
	err     error

	base       listBase
	extensionM bool // 'e' — extension breakdown mode
	stack      []string

	// Roots (when cwd == "").
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

// NewUsage constructs the analyzer view.
func NewUsage(c Ctx) tui.View {
	return &Usage{ctx: c, cache: newUsageCache()}
}

func (u *Usage) Name() string                   { return "usage" }
func (u *Usage) Title() string                  { return "Usage Analyzer" }
func (u *Usage) Icon() string                   { return "◴" }
func (u *Usage) RefreshInterval() time.Duration { return 0 }
func (u *Usage) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("backspace", "h"), key.WithHelp("⌫/h", "up one")),
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

// loadDir lists the immediate children of u.cwd and kicks off DirSize for
// each subdirectory (unless already cached). Files are inserted with
// their known sizes.
func (u *Usage) loadDir() tea.Cmd {
	c := u.ctx.Client
	if c == nil {
		return nil
	}
	u.loading = true
	cwd := u.cwd
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) ([]dsm.FSEntry, error) {
			items, _, err := c.ListFiles(ctx, cwd, 0, 1000)
			return items, err
		},
		func(items []dsm.FSEntry, err error) tea.Msg {
			return usageListedMsg{Path: cwd, Items: items, Err: err}
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
	inflight := 0
	for i := range u.entries {
		e := &u.entries[i]
		if !e.IsDir || e.Sized || e.Sizing || e.Err != nil {
			continue
		}
		if cached, ok := u.cache.get(e.Path); ok {
			e.Size = cached
			e.Sized = true
			continue
		}
		e.Sizing = true
		cmds = append(cmds, u.dirSizeCmd(e.Path))
		inflight++
		if inflight >= maxInflight {
			break
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
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
		if m.Path != u.cwd {
			return u, nil // late arrival — user moved on
		}
		u.loading = false
		if m.Err != nil {
			u.err = m.Err
			return u, nil
		}
		u.err = nil
		u.entries = u.entries[:0]
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
			u.entries = append(u.entries, ue)
		}
		u.sortBySize()
		u.base.ClampCursor(u.rowCount())
		return u, u.kickPendingSizes()
	case usageSizedMsg:
		if m.Err == nil {
			u.cache.set(m.Path, m.Size)
		}
		for i := range u.entries {
			if u.entries[i].Path == m.Path {
				u.entries[i].Sizing = false
				if m.Err != nil {
					u.entries[i].Err = m.Err
				} else {
					u.entries[i].Size = m.Size
					u.entries[i].Sized = true
				}
				break
			}
		}
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
			if u.cwd != "" {
				return u, u.upDir()
			}
		case "e":
			u.extensionM = !u.extensionM
			return u, nil
		case "R":
			// Re-size: forget cache for cwd's children and refetch.
			for _, e := range u.entries {
				if e.IsDir {
					u.cache.set(e.Path, -1) // negative sentinel ignored on read
				}
			}
			if u.cwd == "" {
				return u, u.fetchRoots()
			}
			return u, u.loadDir()
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
	if !e.IsDir {
		return nil
	}
	u.stack = append(u.stack, u.cwd)
	u.cwd = e.Path
	u.base.ResetCursor()
	// Clear entries up front so the user sees a "listing…" placeholder
	// instead of the previous folder's stale rows for the 5–15 seconds
	// the DS220j takes to answer the new ListFiles call.
	u.entries = nil
	u.err = nil
	return u.loadDir()
}

func (u *Usage) upDir() tea.Cmd {
	if u.cwd == "" {
		return nil
	}
	u.entries = nil
	u.err = nil
	if len(u.stack) > 0 {
		prev := u.stack[len(u.stack)-1]
		u.stack = u.stack[:len(u.stack)-1]
		u.cwd = prev
		u.base.ResetCursor()
		if prev == "" {
			return u.fetchRoots()
		}
		return u.loadDir()
	}
	parent := path.Dir(u.cwd)
	if parent == "." || parent == "/" {
		u.cwd = ""
		u.base.ResetCursor()
		return u.fetchRoots()
	}
	u.cwd = parent
	u.base.ResetCursor()
	return u.loadDir()
}

// sortBySize sorts the entries by descending size, but only once every
// directory has settled. Resorting while DirSize results are still
// trickling in makes the list jump under the cursor — what looks like
// lag is actually rows reshuffling. We keep the input order (files
// first, alphabetical) until everything is sized, then do one final
// sort.
func (u *Usage) sortBySize() {
	for _, e := range u.entries {
		if e.Sizing {
			return
		}
	}
	sort.SliceStable(u.entries, func(i, j int) bool {
		return u.entries[i].Size > u.entries[j].Size
	})
}

func (u *Usage) rowCount() int { return len(u.viewRows()) }

// dirRows returns the regular (non-extension) entries.
func (u *Usage) dirRows() []usageEntry {
	if u.base.FilterValue() == "" {
		return u.entries
	}
	out := make([]usageEntry, 0, len(u.entries))
	for _, e := range u.entries {
		if MatchesAll(u.base.FilterValue(), e.Name, e.Path) {
			out = append(out, e)
		}
	}
	return out
}

// viewRows returns either the directory entries or the extension breakdown,
// depending on mode.
func (u *Usage) viewRows() []usageEntry {
	if !u.extensionM {
		return u.dirRows()
	}
	// Extension mode: aggregate file extensions of files at this level.
	totals := map[string]*usageEntry{}
	for _, e := range u.entries {
		if e.IsDir {
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
	help := "  ⏎ drill in · ⌫ up · e ext breakdown · R re-size · / filter"
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
	if u.cwd == "" {
		return accent.Render(" usage / ") + muted.Render("(pick a share to drill in)")
	}
	parts := []string{accent.Render(" usage / ")}
	segs := strings.Split(strings.TrimPrefix(u.cwd, "/"), "/")
	for i, s := range segs {
		if s == "" {
			continue
		}
		if i == len(segs)-1 {
			parts = append(parts, accent.Render(s))
		} else {
			parts = append(parts, muted.Render(s), muted.Render(" › "))
		}
	}
	_ = width
	return strings.Join(parts, "")
}

// renderEntries renders u.entries with proportional size bars. At root
// (u.cwd == "") the entries are shares; deeper down they're files +
// folders. The "Sizing…" placeholder shows for in-flight DirSize calls.
func (u *Usage) renderEntries(width int) []string {
	t := u.ctx.Theme
	rows := u.viewRows()
	title := "Children"
	var loadErr error = u.err
	if u.cwd == "" {
		title = "Shares (largest first)"
		loadErr = u.rootsErr
	}
	if u.extensionM {
		title += " · by extension"
	}
	out := []string{sectionHeader(t, width, title, len(rows), loadErr)}
	if u.cwd == "" && u.roots == nil && u.rootsErr == nil {
		out = append(out, "  "+muted(t, "loading shares…"))
		return out
	}
	if u.cwd != "" && u.loading && len(rows) == 0 {
		out = append(out, "  "+muted(t, "listing…"))
		return out
	}
	if !u.loading && len(rows) == 0 {
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
	var sizing int
	for _, e := range u.entries {
		total += e.Size
		if e.Sizing {
			sizing++
		}
	}
	summary := fmt.Sprintf("  total %s · %d items · %d sizing", HumanBytes(uint64(total)), len(u.entries), sizing)
	out = append(out, "", lipgloss.NewStyle().Foreground(t.Muted).Render(summary))
	return out
}

func (u *Usage) renderDir(width int) []string {
	t := u.ctx.Theme
	rows := u.viewRows()
	titleSfx := ""
	if u.extensionM {
		titleSfx = " · by extension"
	}
	header := sectionHeader(t, width, "Children"+titleSfx, len(rows), u.err)
	out := []string{header}
	if u.loading && len(rows) == 0 {
		out = append(out, "  "+muted(t, "listing…"))
		return out
	}
	if !u.loading && len(rows) == 0 {
		out = append(out, "  "+muted(t, "(empty)"))
		return out
	}
	// Compute max known size for bars (including in-flight ones use prior size 0 → fine).
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
	var sized, sizing int
	for _, e := range u.entries {
		total += e.Size
		if e.Sized {
			sized++
		}
		if e.Sizing {
			sizing++
		}
	}
	summary := fmt.Sprintf("  total %s · %d items · %d sizing", HumanBytes(uint64(total)), len(u.entries), sizing)
	out = append(out, "", lipgloss.NewStyle().Foreground(t.Muted).Render(summary))
	return out
}

func (u *Usage) renderDirRow(width int, e usageEntry, maxSize int64, highlight bool) string {
	t := u.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	accent := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	barW := max(width-60, 20)
	ratio := 0.0
	if maxSize > 0 {
		ratio = float64(e.Size) / float64(maxSize)
	}
	icon := "  "
	if e.IsDir {
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
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ", icon, " ",
		padRight(name, 36), " ",
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
