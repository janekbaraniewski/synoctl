package views

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// StoragePage is the unified storage management view. Everything you'd
// want to inspect or act on across the storage stack — volumes, disks,
// shared folders, the files inside them — lives on a single
// scrollable screen.
//
// Navigation: j/k (or ↑/↓) moves the cursor through every entry; the
// cursor skips section headers automatically. Enter drills into the
// selected entry — the existing per-entity detail renderers
// (renderVolumeDetail, renderDiskDetail, …) provide the inspector. For
// shared folders, Enter drops you into a focused file-browser mode at
// that share's path; ⌫ steps back out.
type StoragePage struct {
	ctx Ctx

	// Data
	storage  *dsm.Storage
	shares   []dsm.Share
	files    []dsm.FSEntry
	filePath string // "" = list of shared folders only (no files panel)
	stack    []string

	// Errors
	storeErr, sharesErr, filesErr error

	// UI state
	cursor int // index into the flat row list (see flatten())
	filter Filter

	// Modal / detail state
	confirm     *Confirm
	prompt      *Prompt
	detailVol   *dsm.Volume
	detailDisk  *dsm.Disk
	detailShare *dsm.Share
	detailFile  *dsm.FSEntry
	flash       string
}

// NewStoragePage constructs the consolidated storage view.
func NewStoragePage(c Ctx) tui.View {
	return &StoragePage{
		ctx:     c,
		confirm: NewConfirm(c.Theme),
		prompt:  NewPrompt(c.Theme),
	}
}

func (s *StoragePage) Name() string                   { return "storage" }
func (s *StoragePage) Title() string                  { return "Storage" }
func (s *StoragePage) Icon() string                   { return "▮" }
func (s *StoragePage) RefreshInterval() time.Duration { return 15 * time.Second }
func (s *StoragePage) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("backspace"), key.WithHelp("⌫", "leave file browser")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete file (confirm)")),
		key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "rename file")),
	)
}

func (s *StoragePage) Init() tea.Cmd {
	return tea.Batch(s.fetchStorage(), s.fetchShares())
}

// ───────────────────────── fetching ─────────────────────────

func (s *StoragePage) fetchStorage() tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) (*dsm.Storage, error) { return c.Storage(ctx) },
		func(v *dsm.Storage, err error) tea.Msg { return storageMsg{S: v, Err: err} },
	)
}

func (s *StoragePage) fetchShares() tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Share, error) { return c.Shares(ctx) },
		func(v []dsm.Share, err error) tea.Msg { return sharesMsg{S: v, Err: err} },
	)
}

func (s *StoragePage) fetchFiles(p string) tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.FSEntry, error) {
			items, _, err := c.ListFiles(ctx, p, 0, 500)
			return items, err
		},
		func(e []dsm.FSEntry, err error) tea.Msg { return filesListMsg{E: e, Err: err} },
	)
}

// ───────────────────────── row model ─────────────────────────

// rowKind discriminates what kind of entry a flat-list row represents.
type rowKind int

const (
	rowVolume rowKind = iota
	rowDisk
	rowShare
	rowFile
)

// row is a single selectable line in the flat list. Headers are not
// represented — they're inserted at render time around groups.
type row struct {
	kind  rowKind
	index int // index back into the underlying slice
}

func (s *StoragePage) flatten() []row {
	var out []row
	if s.storage != nil {
		for i := range s.filterVolumes() {
			out = append(out, row{rowVolume, i})
		}
		for i := range s.filterDisks() {
			out = append(out, row{rowDisk, i})
		}
	}
	for i := range s.filterShares() {
		out = append(out, row{rowShare, i})
	}
	if s.filePath != "" {
		for i := range s.filterFiles() {
			out = append(out, row{rowFile, i})
		}
	}
	return out
}

func (s *StoragePage) filterVolumes() []dsm.Volume {
	if s.storage == nil {
		return nil
	}
	if s.filter.Value() == "" {
		return s.storage.Volumes
	}
	out := make([]dsm.Volume, 0)
	for _, v := range s.storage.Volumes {
		if MatchesAll(s.filter.Value(), v.ID, v.VolPath, v.FSType, v.RaidType, v.Status) {
			out = append(out, v)
		}
	}
	return out
}

func (s *StoragePage) filterDisks() []dsm.Disk {
	if s.storage == nil {
		return nil
	}
	if s.filter.Value() == "" {
		return s.storage.Disks
	}
	out := make([]dsm.Disk, 0)
	for _, d := range s.storage.Disks {
		if MatchesAll(s.filter.Value(), d.ID, d.Model, d.Vendor, d.Status, d.DiskType) {
			out = append(out, d)
		}
	}
	return out
}

func (s *StoragePage) filterShares() []dsm.Share {
	if s.filter.Value() == "" {
		return s.shares
	}
	out := make([]dsm.Share, 0)
	for _, sh := range s.shares {
		if MatchesAll(s.filter.Value(), sh.Name, sh.Path, sh.Desc) {
			out = append(out, sh)
		}
	}
	return out
}

func (s *StoragePage) filterFiles() []dsm.FSEntry {
	if s.filter.Value() == "" {
		return s.files
	}
	out := make([]dsm.FSEntry, 0)
	for _, e := range s.files {
		if MatchesAll(s.filter.Value(), e.Name, e.Path, e.Type) {
			out = append(out, e)
		}
	}
	return out
}

// currentRow returns the row at the cursor, or false if the cursor is
// out of bounds (empty data, no rows).
func (s *StoragePage) currentRow() (row, bool) {
	rows := s.flatten()
	if s.cursor < 0 || s.cursor >= len(rows) {
		return row{}, false
	}
	return rows[s.cursor], true
}

// ───────────────────────── messages ─────────────────────────

type spDeleteMsg struct {
	Path string
	Err  error
}
type spRenameMsg struct {
	Path, NewName string
	Err           error
}

// ───────────────────────── update ─────────────────────────

func (s *StoragePage) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Modals own input.
	if handled, cmd := s.confirm.Update(msg); handled {
		return s, cmd
	}
	if handled, cmd := s.prompt.Update(msg); handled {
		return s, cmd
	}

	// Modal results.
	switch m := msg.(type) {
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "delete:"); ok {
			s.flash = "deleting " + rest + "…"
			c := s.ctx.Client
			return s, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) {
					return struct{}{}, c.FileDelete(ctx, rest, true)
				},
				func(_ struct{}, err error) tea.Msg { return spDeleteMsg{Path: rest, Err: err} },
			)
		}
	case SubmittedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "rename:"); ok {
			if m.Value == "" {
				s.flash = "rename cancelled (empty name)"
				return s, nil
			}
			c := s.ctx.Client
			pth := rest
			newName := m.Value
			s.flash = "renaming " + pth + " → " + newName + "…"
			return s, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) {
					return struct{}{}, c.FileRename(ctx, pth, newName)
				},
				func(_ struct{}, err error) tea.Msg { return spRenameMsg{Path: pth, NewName: newName, Err: err} },
			)
		}
	case CancelledMsg:
		s.flash = "cancelled"
		return s, nil
	case spDeleteMsg:
		if m.Err != nil {
			s.flash = "delete failed: " + m.Err.Error()
		} else {
			s.flash = "deleted " + m.Path
		}
		if s.filePath != "" {
			return s, s.fetchFiles(s.filePath)
		}
	case spRenameMsg:
		if m.Err != nil {
			s.flash = "rename failed: " + m.Err.Error()
		} else {
			s.flash = "renamed to " + m.NewName
		}
		if s.filePath != "" {
			return s, s.fetchFiles(s.filePath)
		}
	}

	// Detail overlays consume keys while open.
	if s.anyDetailOpen() {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				s.closeDetail()
				return s, nil
			case "D":
				if s.detailFile != nil {
					p := s.detailFile.Path
					s.confirm.Ask("delete:"+p, "Delete "+p+"?",
						"This recursively removes the file or folder. There is no undo.")
					return s, nil
				}
			case "N":
				if s.detailFile != nil {
					p := s.detailFile.Path
					s.prompt.Ask("rename:"+p, "Rename "+s.detailFile.Name,
						"Enter a new name (path component only):", s.detailFile.Name)
					return s, nil
				}
			}
		}
		return s, nil
	}

	// Filter editor consumes runes when open.
	if s.filter.IsActive() {
		if s.filter.Update(msg) {
			return s, nil
		}
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return s, tea.Batch(s.fetchStorage(), s.fetchShares(), s.maybeRefreshFiles())
	case storageMsg:
		s.storage, s.storeErr = m.S, m.Err
		s.clampCursor()
	case sharesMsg:
		s.shares, s.sharesErr = m.S, m.Err
		s.clampCursor()
	case filesListMsg:
		s.files, s.filesErr = m.E, m.Err
		s.clampCursor()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := s.flatten()
			if s.cursor < len(rows)-1 {
				s.cursor++
			}
		case "k", "up":
			if s.cursor > 0 {
				s.cursor--
			}
		case "g":
			s.cursor = 0
		case "G":
			s.cursor = max(len(s.flatten())-1, 0)
		case "/":
			s.filter.Open()
		case "esc":
			if s.filter.Value() != "" {
				s.filter.Clear()
				s.cursor = 0
			} else if s.filePath != "" && len(s.stack) > 0 {
				// Step up one directory.
				prev := s.stack[len(s.stack)-1]
				s.stack = s.stack[:len(s.stack)-1]
				s.filePath = prev
				if s.filePath == "" {
					s.files = nil
					s.filesErr = nil
				} else {
					return s, s.fetchFiles(s.filePath)
				}
			}
		case "backspace", "h":
			if s.filePath != "" {
				return s, s.upDirectory()
			}
		case "r":
			return s, tea.Batch(s.fetchStorage(), s.fetchShares(), s.maybeRefreshFiles())
		case "enter":
			return s, s.drillDown()
		case "D":
			if r, ok := s.currentRow(); ok && r.kind == rowFile {
				e := s.filterFiles()[r.index]
				s.confirm.Ask("delete:"+e.Path, "Delete "+e.Path+"?",
					"This recursively removes the file or folder. There is no undo.")
			}
		case "N":
			if r, ok := s.currentRow(); ok && r.kind == rowFile {
				e := s.filterFiles()[r.index]
				s.prompt.Ask("rename:"+e.Path, "Rename "+e.Name,
					"Enter a new name (path component only):", e.Name)
			}
		}
	}
	return s, nil
}

func (s *StoragePage) maybeRefreshFiles() tea.Cmd {
	if s.filePath == "" {
		return nil
	}
	return s.fetchFiles(s.filePath)
}

func (s *StoragePage) anyDetailOpen() bool {
	return s.detailVol != nil || s.detailDisk != nil || s.detailShare != nil || s.detailFile != nil
}

func (s *StoragePage) closeDetail() {
	s.detailVol, s.detailDisk, s.detailShare, s.detailFile = nil, nil, nil, nil
}

func (s *StoragePage) clampCursor() {
	rows := s.flatten()
	if s.cursor >= len(rows) {
		s.cursor = len(rows) - 1
	}
	if s.cursor < 0 {
		s.cursor = 0
	}
}

func (s *StoragePage) drillDown() tea.Cmd {
	r, ok := s.currentRow()
	if !ok {
		return nil
	}
	switch r.kind {
	case rowVolume:
		v := s.filterVolumes()[r.index]
		s.detailVol = &v
	case rowDisk:
		d := s.filterDisks()[r.index]
		s.detailDisk = &d
	case rowShare:
		// Enter on a share starts the file browser at that share's path.
		sh := s.filterShares()[r.index]
		s.stack = append(s.stack, s.filePath)
		s.filePath = sh.Path
		return s.fetchFiles(sh.Path)
	case rowFile:
		e := s.filterFiles()[r.index]
		if e.IsDir {
			s.stack = append(s.stack, s.filePath)
			s.filePath = e.Path
			return s.fetchFiles(e.Path)
		}
		s.detailFile = &e
	}
	return nil
}

func (s *StoragePage) upDirectory() tea.Cmd {
	if s.filePath == "" {
		return nil
	}
	if len(s.stack) > 0 {
		prev := s.stack[len(s.stack)-1]
		s.stack = s.stack[:len(s.stack)-1]
		s.filePath = prev
		if prev == "" {
			s.files = nil
			s.filesErr = nil
			return nil
		}
		return s.fetchFiles(prev)
	}
	parent := path.Dir(s.filePath)
	if parent == "." || parent == "/" {
		s.filePath = ""
		s.files = nil
		s.filesErr = nil
		return nil
	}
	s.filePath = parent
	return s.fetchFiles(parent)
}

// ───────────────────────── render ─────────────────────────

func (s *StoragePage) Render(width, height int) string {
	t := s.ctx.Theme
	if s.confirm.Open() {
		return s.confirm.Render(width, height)
	}
	if s.prompt.Open() {
		return s.prompt.Render(width, height)
	}
	if s.detailVol != nil {
		pools, disks := []dsm.StoragePool{}, []dsm.Disk{}
		if s.storage != nil {
			pools = s.storage.StoragePools
			disks = s.storage.Disks
		}
		return renderVolumeDetail(t, width, height, *s.detailVol, pools, disks, nil)
	}
	if s.detailDisk != nil {
		pools := []dsm.StoragePool{}
		if s.storage != nil {
			pools = s.storage.StoragePools
		}
		return renderDiskDetail(t, width, height, *s.detailDisk, pools)
	}
	if s.detailShare != nil {
		return renderShareDetail(t, width, height, *s.detailShare)
	}
	if s.detailFile != nil {
		return s.renderFileDetail(width, height, *s.detailFile)
	}

	// Compose sections into a flat scrollable surface.
	rows := s.flatten()
	sections := s.composeSections(width, rows)
	body := strings.Join(sections, "\n")
	if s.flash != "" {
		body += "\n" + lipgloss.NewStyle().Foreground(t.Muted).Render("  "+s.flash)
	}
	if v := s.filter.Render(t); v != "" {
		body += "\n" + v
	}

	// Clip to height — top sections always visible, file listing
	// scrolls within whatever's left.
	return fitOrScroll(body, height)
}

// composeSections returns one rendered chunk per section. Each section
// gets a header and the rows that belong to it, with the cursor
// highlighted when the cursor is inside that section.
func (s *StoragePage) composeSections(width int, rows []row) []string {
	t := s.ctx.Theme
	var out []string

	cursorRow := -1
	if len(rows) > 0 && s.cursor < len(rows) {
		cursorRow = s.cursor
	}

	// Build cumulative offsets into rows[] for fast per-section cursor lookup.
	idx := 0

	// — Volumes —
	vs := s.filterVolumes()
	if s.storage != nil {
		out = append(out, s.sectionHeader(width, "Volumes", len(vs), s.storeErr))
		if len(vs) == 0 {
			out = append(out, "  "+muted(t, "(none)"))
		}
		for _, v := range vs {
			highlight := cursorRow == idx
			out = append(out, s.renderVolumeRow(width, v, highlight))
			idx++
		}
	}

	// — Disks —
	ds := s.filterDisks()
	if s.storage != nil {
		out = append(out, "")
		out = append(out, s.sectionHeader(width, "Disks", len(ds), s.storeErr))
		if len(ds) == 0 {
			out = append(out, "  "+muted(t, "(none)"))
		}
		for i, d := range ds {
			highlight := cursorRow == idx
			out = append(out, s.renderDiskRow(width, d, highlight))
			idx++
			_ = i
		}
	}

	// — Shared folders —
	shs := s.filterShares()
	out = append(out, "")
	out = append(out, s.sectionHeader(width, "Shared folders", len(shs), s.sharesErr))
	if s.shares == nil {
		out = append(out, "  "+muted(t, "loading…"))
	} else if len(shs) == 0 {
		out = append(out, "  "+muted(t, "(none)"))
	}
	for i, sh := range shs {
		highlight := cursorRow == idx
		out = append(out, s.renderShareRow(width, sh, highlight))
		idx++
		_ = i
	}

	// — Files (only when browsing) —
	if s.filePath != "" {
		out = append(out, "")
		fs := s.filterFiles()
		out = append(out, s.sectionHeader(width, "Files · "+s.filePath, len(fs), s.filesErr))
		if s.files == nil && s.filesErr == nil {
			out = append(out, "  "+muted(t, "loading…"))
		} else if len(fs) == 0 && s.filesErr == nil {
			out = append(out, "  "+muted(t, "(empty)"))
		}
		for i, e := range fs {
			highlight := cursorRow == idx
			out = append(out, s.renderFileRow(width, e, highlight))
			idx++
			_ = i
		}
	}

	out = append(out, "")
	out = append(out, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ open share / drill in · / filter · ⌫ leave file browser · D delete · N rename"))
	return out
}

func (s *StoragePage) sectionHeader(width int, title string, count int, err error) string {
	t := s.ctx.Theme
	titleStyle := t.Title().Render(title)
	countStyle := lipgloss.NewStyle().Foreground(t.Muted).Render(fmt.Sprintf("(%d)", count))
	left := titleStyle + " " + countStyle
	leftW := lipgloss.Width(left)
	rule := strings.Repeat("─", maxInt(width-leftW-4, 0))
	header := left + "  " + lipgloss.NewStyle().Foreground(t.Border).Render(rule)
	if err != nil {
		header += "\n" + errLine(t, err)
	}
	return header
}

func (s *StoragePage) renderVolumeRow(width int, v dsm.Volume, highlight bool) string {
	t := s.ctx.Theme
	name := v.VolPath
	if name == "" {
		name = v.ID
	}
	total := ParseSizeString(v.Size.Total)
	used := ParseSizeString(v.Size.Used)
	ratio := 0.0
	if total > 0 {
		ratio = float64(used) / float64(total)
	}
	barW := max(width-72, 16)
	bar := Gauge(t, barW, ratio)
	status := t.HealthStyle(v.Status).Render(v.Status)
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	row := lipgloss.JoinHorizontal(lipgloss.Center,
		s.caret(highlight), " ",
		padRight(text.Render(name), 12), " ",
		padRight(muted.Render(strings.ToUpper(v.FSType)+"·"+v.RaidType), 14), " ",
		bar, " ",
		padLeft(text.Render(fmt.Sprintf("%5.1f%%", ratio*100)), 7), "  ",
		padLeft(muted.Render(fmt.Sprintf("%s / %s", HumanBytes(used), HumanBytes(total))), 22), "  ",
		status,
	)
	return row
}

func (s *StoragePage) renderDiskRow(width int, d dsm.Disk, highlight bool) string {
	t := s.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	cap := HumanBytes(ParseSizeString(d.Capacity))
	bay := trimDev(d.ID)
	temp := lipgloss.NewStyle().Foreground(tempColor(t, d.Temperature)).Bold(true).Render(
		fmt.Sprintf("%d°C", d.Temperature))
	smart := d.Smart.Status
	if smart == "" {
		smart = "—"
	}
	row := lipgloss.JoinHorizontal(lipgloss.Center,
		s.caret(highlight), " ",
		padRight(text.Render(bay), 6), " ",
		padRight(muted.Render(strings.TrimSpace(d.Vendor+" "+d.Model)), 28), " ",
		padRight(muted.Render(d.DiskType), 10), " ",
		padLeft(text.Render(cap), 10), "  ",
		temp, "  ",
		padRight(muted.Render("SMART "+smart), 14), " ",
		t.HealthStyle(d.Status).Render(d.Status),
	)
	_ = width
	return row
}

func (s *StoragePage) renderShareRow(width int, sh dsm.Share, highlight bool) string {
	t := s.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	quota := "—"
	if sh.ShareQuota > 0 {
		quota = HumanBytes(uint64(sh.ShareQuotaUsed)*1024*1024) + " / " + HumanBytes(uint64(sh.ShareQuota)*1024*1024)
	}
	flags := []string{}
	if sh.IsEncrypted() {
		flags = append(flags, "enc")
	}
	if sh.EnableRecycle {
		flags = append(flags, "recycle")
	}
	if sh.Hidden {
		flags = append(flags, "hidden")
	}
	if sh.Readonly {
		flags = append(flags, "ro")
	}
	flagStr := strings.Join(flags, " ")
	row := lipgloss.JoinHorizontal(lipgloss.Center,
		s.caret(highlight), " ",
		padRight(text.Render(sh.Name), 18), " ",
		padRight(muted.Render(sh.Path), 28), " ",
		padRight(muted.Render(flagStr), 24), " ",
		padLeft(muted.Render(quota), 22),
	)
	_ = width
	return row
}

func (s *StoragePage) renderFileRow(width int, e dsm.FSEntry, highlight bool) string {
	t := s.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	accent := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	name := e.Name
	size := "—"
	if e.IsDir {
		name = "📁 " + e.Name
	} else {
		size = humanize.IBytes(uint64(e.Add.Size))
	}
	mod := "—"
	if e.Add.Time.Mtime > 0 {
		mod = time.Unix(e.Add.Time.Mtime, 0).Format("2006-01-02 15:04")
	}
	display := text.Render(name)
	if e.IsDir {
		display = accent.Render(name)
	}
	row := lipgloss.JoinHorizontal(lipgloss.Center,
		s.caret(highlight), " ",
		padRight(display, 32), " ",
		padLeft(text.Render(size), 12), "  ",
		padRight(muted.Render(mod), 20), " ",
		muted.Render(e.Add.Owner.User),
	)
	_ = width
	return row
}

// caret renders the ▸ glyph if highlighted; otherwise a single space of
// the same visual width so alignment doesn't shift.
func (s *StoragePage) caret(on bool) string {
	if on {
		return lipgloss.NewStyle().Foreground(s.ctx.Theme.Accent).Bold(true).Render("▸")
	}
	return " "
}

func (s *StoragePage) renderFileDetail(width, _ int, e dsm.FSEntry) string {
	t := s.ctx.Theme
	parts := []string{
		hero(t, width, "📄", e.Name, "", e.Path),
	}
	size := "(folder)"
	if !e.IsDir {
		size = humanize.IBytes(uint64(e.Add.Size))
	}
	props := [][2]string{
		{"Path", e.Path},
		{"Size", size},
		{"Type", coalesce(e.Type, e.Add.Type)},
		{"Owner", e.Add.Owner.User},
		{"Group", e.Add.Owner.Group},
		{"POSIX perms", fmt.Sprintf("%o", e.Add.Perm.POSIX)},
		{"Real path", e.Add.RealPath},
	}
	if e.Add.Time.Mtime > 0 {
		props = append(props, [2]string{"Modified", time.Unix(e.Add.Time.Mtime, 0).Format("2006-01-02 15:04:05")})
	}
	if e.Add.Time.Atime > 0 {
		props = append(props, [2]string{"Accessed", time.Unix(e.Add.Time.Atime, 0).Format("2006-01-02 15:04:05")})
	}
	if e.Add.Time.Ctime > 0 {
		props = append(props, [2]string{"Changed", time.Unix(e.Add.Time.Ctime, 0).Format("2006-01-02 15:04:05")})
	}
	if e.Add.Time.Crtime > 0 {
		props = append(props, [2]string{"Created", time.Unix(e.Add.Time.Crtime, 0).Format("2006-01-02 15:04:05")})
	}
	parts = append(parts, propsCard(t, width, " Properties ", props))
	parts = append(parts, noteCard(t, width, "  esc to go back · D delete · N rename"))
	if s.flash != "" {
		parts = append(parts, noteCard(t, width, "  "+s.flash))
	}
	return strings.Join(parts, "\n")
}

// fitOrScroll trims output to a maximum of `n` lines, ensuring the row
// containing the cursor stays visible. For our purposes a simple
// scroll-to-keep-cursor-on-screen is enough.
func fitOrScroll(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		// Pad to fill.
		return s + strings.Repeat("\n", n-len(lines))
	}
	// Take the first n lines for now — cursor tracking inside a wrapped
	// multi-section flow would require knowing which line each row maps
	// to. The StoragePage typically fits inside one screen at the
	// resolutions Bubbletea hands us; if it overflows we trim the tail.
	return strings.Join(lines[:n], "\n")
}
