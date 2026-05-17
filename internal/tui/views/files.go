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

// Files is a File Station browser — the user's escape hatch into the
// data living on the NAS. Enter on a folder navigates in, Backspace or
// Esc steps up, `/` filters the current directory.
//
// The view tracks one path at a time and re-fetches on navigation. It
// auto-loads SYNO.FileStation.List.list_share when started at the root.
type Files struct {
	listBase
	ctx Ctx

	// currentPath is empty when we're showing the roots (shares).
	currentPath string
	stack       []string // navigation history (for back)

	shares  []dsm.FileShare
	entries []dsm.FSEntry
	total   int
	err     error

	detail  *dsm.FSEntry
	confirm *Confirm
	prompt  *Prompt
	flash   string
}

type filesSharesMsg struct {
	S   []dsm.FileShare
	Err error
}
type filesListMsg struct {
	E     []dsm.FSEntry
	Total int
	Err   error
}

func NewFiles(c Ctx) tui.View {
	f := &Files{ctx: c, confirm: NewConfirm(c.Theme), prompt: NewPrompt(c.Theme)}
	f.initBase(c)
	return f
}

func (f *Files) Name() string                   { return "files" }
func (f *Files) Title() string                  { return "Files" }
func (f *Files) Icon() string                   { return "🗁" }
func (f *Files) RefreshInterval() time.Duration { return 0 }
func (f *Files) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("backspace"), key.WithHelp("⌫", "up")),
		key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "up a directory")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete (with confirm)")),
		key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "rename")),
	)
}

func (f *Files) Init() tea.Cmd { return f.fetchRoots() }

func (f *Files) fetchRoots() tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.FileShare, error) { return c.FileShares(ctx) },
		func(s []dsm.FileShare, err error) tea.Msg { return filesSharesMsg{S: s, Err: err} },
	)
}

func (f *Files) fetchDir(p string) tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.FSEntry, error) {
			items, total, err := c.ListFiles(ctx, p, 0, 1000)
			f.total = total
			return items, err
		},
		func(e []dsm.FSEntry, err error) tea.Msg { return filesListMsg{E: e, Total: f.total, Err: err} },
	)
}

// NavigateTo replaces the current path and fetches its contents.
func (f *Files) NavigateTo(p string) tea.Cmd {
	if p != "" {
		f.stack = append(f.stack, f.currentPath)
	}
	f.currentPath = p
	f.ResetCursor()
	if p == "" {
		return f.fetchRoots()
	}
	return f.fetchDir(p)
}

func (f *Files) up() tea.Cmd {
	if f.currentPath == "" {
		return nil
	}
	if len(f.stack) > 0 {
		prev := f.stack[len(f.stack)-1]
		f.stack = f.stack[:len(f.stack)-1]
		f.currentPath = prev
	} else {
		parent := path.Dir(f.currentPath)
		if parent == "." || parent == "/" {
			f.currentPath = ""
		} else {
			f.currentPath = parent
		}
	}
	f.ResetCursor()
	if f.currentPath == "" {
		return f.fetchRoots()
	}
	return f.fetchDir(f.currentPath)
}

func (f *Files) visibleShares() []dsm.FileShare {
	if f.FilterValue() == "" {
		return f.shares
	}
	out := make([]dsm.FileShare, 0)
	for _, s := range f.shares {
		if f.FilterMatch(s.Name, s.Path) {
			out = append(out, s)
		}
	}
	return out
}

func (f *Files) visibleEntries() []dsm.FSEntry {
	if f.FilterValue() == "" {
		return f.entries
	}
	out := make([]dsm.FSEntry, 0)
	for _, e := range f.entries {
		if f.FilterMatch(e.Name, e.Path, e.Type) {
			out = append(out, e)
		}
	}
	return out
}

func (f *Files) rowCount() int {
	if f.currentPath == "" {
		return len(f.visibleShares())
	}
	return len(f.visibleEntries())
}

type filesDeleteMsg struct {
	Path string
	Err  error
}
type filesRenameMsg struct {
	Path, NewName string
	Err           error
}

func (f *Files) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := f.confirm.Update(msg); handled {
		return f, cmd
	}
	if handled, cmd := f.prompt.Update(msg); handled {
		return f, cmd
	}
	switch m := msg.(type) {
	case ConfirmedMsg:
		if strings.HasPrefix(m.Token, "delete:") {
			pth := strings.TrimPrefix(m.Token, "delete:")
			f.flash = "deleting " + pth + "…"
			c := f.ctx.Client
			return f, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.FileDelete(ctx, pth, true) },
				func(_ struct{}, err error) tea.Msg { return filesDeleteMsg{Path: pth, Err: err} },
			)
		}
	case SubmittedMsg:
		if strings.HasPrefix(m.Token, "rename:") {
			pth := strings.TrimPrefix(m.Token, "rename:")
			if m.Value == "" {
				f.flash = "rename cancelled (empty name)"
				return f, nil
			}
			f.flash = "renaming " + pth + " → " + m.Value + "…"
			c := f.ctx.Client
			newName := m.Value
			return f, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.FileRename(ctx, pth, newName) },
				func(_ struct{}, err error) tea.Msg { return filesRenameMsg{Path: pth, NewName: newName, Err: err} },
			)
		}
	case CancelledMsg:
		f.flash = "cancelled"
		return f, nil
	case filesDeleteMsg:
		if m.Err != nil {
			f.flash = "delete failed: " + m.Err.Error()
		} else {
			f.flash = "deleted " + m.Path
		}
		if f.currentPath == "" {
			return f, f.fetchRoots()
		}
		return f, f.fetchDir(f.currentPath)
	case filesRenameMsg:
		if m.Err != nil {
			f.flash = "rename failed: " + m.Err.Error()
		} else {
			f.flash = "renamed to " + m.NewName
		}
		if f.currentPath == "" {
			return f, f.fetchRoots()
		}
		return f, f.fetchDir(f.currentPath)
	}
	// Detail screen handling — esc closes.
	if f.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				f.detail = nil
				return f, nil
			case "D":
				p := f.detail.Path
				f.confirm.Ask("delete:"+p, "Delete "+p+"?",
					"This recursively removes the file or folder. There is no undo.")
				return f, nil
			}
		}
		return f, nil
	}
	if cmd, handled := f.HandleKey(msg, f.rowCount()); handled {
		return f, cmd
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "backspace", "h":
			return f, f.up()
		case "esc":
			if f.currentPath != "" {
				return f, f.up()
			}
		case "r":
			if f.currentPath == "" {
				return f, f.fetchRoots()
			}
			return f, f.fetchDir(f.currentPath)
		}
	}
	if f.IsEnter(msg) {
		if f.currentPath == "" {
			shares := f.visibleShares()
			if f.Cursor() < len(shares) {
				return f, f.NavigateTo(shares[f.Cursor()].Path)
			}
		} else {
			entries := f.visibleEntries()
			if f.Cursor() < len(entries) {
				e := entries[f.Cursor()]
				if e.IsDir {
					return f, f.NavigateTo(e.Path)
				}
				// File: open inspector.
				f.detail = &e
				return f, nil
			}
		}
	}
	if km, ok := msg.(tea.KeyMsg); ok && f.currentPath != "" {
		entries := f.visibleEntries()
		switch km.String() {
		case "D":
			if f.Cursor() < len(entries) {
				e := entries[f.Cursor()]
				f.confirm.Ask("delete:"+e.Path, "Delete "+e.Path+"?",
					"This recursively removes the file or folder. There is no undo.")
				return f, nil
			}
		case "N":
			if f.Cursor() < len(entries) {
				e := entries[f.Cursor()]
				f.prompt.Ask("rename:"+e.Path, "Rename "+e.Name,
					"Enter a new name (path component only, no slashes):", e.Name)
				return f, nil
			}
		}
	}
	switch m := msg.(type) {
	case filesSharesMsg:
		f.shares, f.err = m.S, m.Err
		f.ClampCursor(f.rowCount())
	case filesListMsg:
		f.entries, f.err = m.E, m.Err
		f.total = m.Total
		f.ClampCursor(f.rowCount())
	}
	return f, nil
}

func (f *Files) Render(width, height int) string {
	t := f.ctx.Theme
	if f.confirm.Open() {
		return f.confirm.Render(width, height)
	}
	if f.prompt.Open() {
		return f.prompt.Render(width, height)
	}
	if f.detail != nil {
		return f.renderFileDetail(width, height, *f.detail)
	}
	title := f.titleString()
	if f.err != nil {
		return Card(t, width, title, "\n"+errLine(t, f.err)+"\n", true)
	}
	if f.currentPath == "" {
		return f.renderShares(width, height, title)
	}
	return f.renderEntries(width, height, title)
}

// renderFileDetail draws the per-file inspector with size + ownership +
// timestamps + permissions.
func (f *Files) renderFileDetail(width, _ int, e dsm.FSEntry) string {
	t := f.ctx.Theme
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
	parts = append(parts,
		noteCard(t, width, "  esc to go back · D to delete (asks for confirmation)"))
	if f.flash != "" {
		parts = append(parts, noteCard(t, width, "  "+f.flash))
	}
	return strings.Join(parts, "\n")
}

func (f *Files) titleString() string {
	if f.currentPath == "" {
		return " 🗁  Files — pick a shared folder · ⏎ open · / filter "
	}
	return " 🗁  " + f.currentPath + " — ⏎ open · ⌫ up · / filter · [N]ame [D]elete "
}

func (f *Files) renderShares(width, height int, title string) string {
	t := f.ctx.Theme
	if f.shares == nil {
		return Card(t, width, title, "\n  Loading…\n", true)
	}
	cols := []Column{
		{Header: "NAME", Width: 24},
		{Header: "OWNER", Width: 14},
		{Header: "FREE", Width: 12, Align: lipgloss.Right},
		{Header: "TOTAL", Width: 12, Align: lipgloss.Right},
		{Header: "PATH", Width: 0},
		{Header: "ACCESS", Width: 10, Align: lipgloss.Right},
	}
	rows := make([][]Cell, 0)
	for _, s := range f.visibleShares() {
		owner := s.Add.Owner.User
		if owner == "" && s.Add.Owner.Group != "" {
			owner = "(group) " + s.Add.Owner.Group
		}
		access := "rw"
		if s.Add.VolStatus.ReadOnly {
			access = "ro"
		}
		free := "—"
		total := "—"
		if s.Add.VolStatus.TotalSpace > 0 {
			free = humanize.IBytes(uint64(s.Add.VolStatus.FreeSpace))
			total = humanize.IBytes(uint64(s.Add.VolStatus.TotalSpace))
		}
		rows = append(rows, []Cell{
			Plain(s.Name),
			Plain(owner),
			Plain(free),
			Plain(total),
			Plain(s.Path),
			Styled(access, t.HealthStyle("ok")),
		})
	}
	footerH := 1
	if v := f.FilterFooter(t); v != "" {
		footerH = 2
	}
	body := "\n" + Table(t, width-4, height-3-footerH, cols, rows, f.Cursor()) + "\n"
	if v := f.FilterFooter(t); v != "" {
		body += v + "\n"
	}
	return Card(t, width, title, body, true)
}

func (f *Files) renderEntries(width, height int, title string) string {
	t := f.ctx.Theme
	cols := []Column{
		{Header: "NAME", Width: 0},
		{Header: "SIZE", Width: 12, Align: lipgloss.Right},
		{Header: "MODIFIED", Width: 20},
		{Header: "OWNER", Width: 14},
	}
	rows := make([][]Cell, 0)
	for _, e := range f.visibleEntries() {
		size := "—"
		nameDecor := e.Name
		if e.IsDir {
			nameDecor = lipgloss.NewStyle().Foreground(t.Accent2).Bold(true).Render("📁 " + e.Name)
		} else {
			size = humanize.IBytes(uint64(e.Add.Size))
		}
		mod := "—"
		if e.Add.Time.Mtime > 0 {
			mod = time.Unix(e.Add.Time.Mtime, 0).Format("2006-01-02 15:04")
		}
		owner := e.Add.Owner.User
		rows = append(rows, []Cell{
			Plain(nameDecor),
			Plain(size),
			Plain(mod),
			Plain(owner),
		})
	}
	footerH := 2
	if v := f.FilterFooter(t); v != "" {
		footerH = 3
	}
	body := "\n" + Table(t, width-4, height-3-footerH, cols, rows, f.Cursor()) + "\n"
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	dirs, files := countKinds(f.entries)
	body += muted.Render(fmt.Sprintf("  %d folders · %d files · %s total visible",
		dirs, files, humanize.IBytes(uint64(totalSize(f.entries))))) + "\n"
	if v := f.FilterFooter(t); v != "" {
		body += v + "\n"
	}
	return Card(t, width, title, body, true)
}

func countKinds(es []dsm.FSEntry) (dirs, files int) {
	for _, e := range es {
		if e.IsDir {
			dirs++
		} else {
			files++
		}
	}
	return
}

func totalSize(es []dsm.FSEntry) int64 {
	var s int64
	for _, e := range es {
		if !e.IsDir {
			s += e.Add.Size
		}
	}
	return s
}

// trim trailing slash if any (for prettier breadcrumb display).
var _ = strings.TrimRight
