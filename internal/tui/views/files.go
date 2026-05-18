package views

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dustin/go-humanize"

	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
)

// Files is the full-pane file browser. It boots into the share-list root
// (one row per FileStation share); enter on a share descends into it; the
// usual cd-style navigation works inside. Beyond browse + rename + delete
// the view exposes two download modes:
//
//   - `o` — download to a temp dir then hand off to the system opener
//     (open / xdg-open / start). Synaesthesia of "double-click in a file
//     manager" — good for previewing PDFs, images, video.
//   - `W` — download to a user-chosen path (prompted).
type Files struct {
	ctx Ctx

	roots    []dsm.FileShare // top-level shares (when path == "")
	rootsErr error

	files    []dsm.FSEntry
	filesErr error
	filePath string   // "" = at the share-list root
	stack    []string // breadcrumb history for esc/⌫

	base    listBase
	detail  *dsm.FSEntry
	confirm *Confirm
	prompt  *Prompt
	flash   string
}

// NewFiles constructs the file browser.
func NewFiles(c Ctx) tui.View {
	return &Files{
		ctx:     c,
		confirm: NewConfirm(c.Theme),
		prompt:  NewPrompt(c.Theme),
	}
}

func (f *Files) Name() string                   { return "files" }
func (f *Files) Title() string                  { return "Files" }
func (f *Files) Icon() string                   { return "🗁" }
func (f *Files) RefreshInterval() time.Duration { return 0 }
func (f *Files) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("backspace", "h"), key.WithHelp("⌫/h", "up one")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open with system app")),
		key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "download…")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete (confirm)")),
		key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "rename")),
	)
}

func (f *Files) Init() tea.Cmd { return f.fetchRoots() }

// IsTextEditing tells the shell to suppress global keybindings (q quit,
// a actions, …) while the user is filling in a prompt — otherwise typed
// runes would never reach the input.
func (f *Files) IsTextEditing() bool {
	return f.prompt.Open() || f.confirm.Open()
}

// — fetches —

func (f *Files) fetchRoots() tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.FileShare, error) { return c.FileShares(ctx) },
		func(s []dsm.FileShare, err error) tea.Msg { return filesRootsMsg{R: s, Err: err} },
	)
}

func (f *Files) fetchFiles(p string) tea.Cmd {
	c := f.ctx.Client
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

// — row model —

func (f *Files) atRoot() bool { return f.filePath == "" }

func (f *Files) rowCount() int {
	if f.atRoot() {
		return len(f.filterRoots())
	}
	return len(f.filterFiles())
}

func (f *Files) filterRoots() []dsm.FileShare {
	if f.base.FilterValue() == "" {
		return f.roots
	}
	out := make([]dsm.FileShare, 0, len(f.roots))
	for _, x := range f.roots {
		if MatchesAll(f.base.FilterValue(), x.Name, x.Path) {
			out = append(out, x)
		}
	}
	return out
}

func (f *Files) filterFiles() []dsm.FSEntry {
	if f.base.FilterValue() == "" {
		return f.files
	}
	out := make([]dsm.FSEntry, 0, len(f.files))
	for _, e := range f.files {
		if MatchesAll(f.base.FilterValue(), e.Name, e.Path, e.Type) {
			out = append(out, e)
		}
	}
	return out
}

// — async result messages —

type filesRootsMsg struct {
	R   []dsm.FileShare
	Err error
}
type filesDeleteMsg struct {
	Path string
	Err  error
}
type filesRenameMsg struct {
	Path, NewName string
	Err           error
}
type filesDownloadMsg struct {
	RemotePath string
	LocalPath  string
	Bytes      int64
	Err        error
}
type filesOpenMsg struct {
	RemotePath string
	LocalPath  string
	Err        error
}

// — update —

func (f *Files) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := f.confirm.Update(msg); handled {
		return f, cmd
	}
	if handled, cmd := f.prompt.Update(msg); handled {
		return f, cmd
	}

	switch m := msg.(type) {
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "delete:"); ok {
			f.flash = "deleting " + rest + "…"
			c := f.ctx.Client
			return f, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.FileDelete(ctx, rest, true) },
				func(_ struct{}, err error) tea.Msg { return filesDeleteMsg{Path: rest, Err: err} },
			)
		}
	case SubmittedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "rename:"); ok {
			if m.Value == "" {
				f.flash = "rename cancelled"
				return f, nil
			}
			c := f.ctx.Client
			pth, newName := rest, m.Value
			f.flash = "renaming " + pth + " → " + newName + "…"
			return f, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.FileRename(ctx, pth, newName) },
				func(_ struct{}, err error) tea.Msg { return filesRenameMsg{Path: pth, NewName: newName, Err: err} },
			)
		}
		if rest, ok := strings.CutPrefix(m.Token, "download:"); ok {
			if m.Value == "" {
				f.flash = "download cancelled"
				return f, nil
			}
			c := f.ctx.Client
			remote, local := rest, expandHome(m.Value)
			f.flash = "downloading " + remote + " → " + local + "…"
			return f, tui.Fetch(10*time.Minute,
				func(ctx context.Context) (int64, error) {
					return downloadToFile(ctx, c, remote, local)
				},
				func(n int64, err error) tea.Msg {
					return filesDownloadMsg{RemotePath: remote, LocalPath: local, Bytes: n, Err: err}
				},
			)
		}
	case CancelledMsg:
		f.flash = "cancelled"
		return f, nil
	case filesRootsMsg:
		f.roots, f.rootsErr = m.R, m.Err
		f.base.ClampCursor(f.rowCount())
		return f, nil
	case filesListMsg:
		f.files, f.filesErr = m.E, m.Err
		f.base.ClampCursor(f.rowCount())
		return f, nil
	case filesDeleteMsg:
		if m.Err != nil {
			f.flash = "delete failed: " + m.Err.Error()
		} else {
			f.flash = "deleted " + m.Path
		}
		if !f.atRoot() {
			return f, f.fetchFiles(f.filePath)
		}
	case filesRenameMsg:
		if m.Err != nil {
			f.flash = "rename failed: " + m.Err.Error()
		} else {
			f.flash = "renamed → " + m.NewName
		}
		if !f.atRoot() {
			return f, f.fetchFiles(f.filePath)
		}
	case filesDownloadMsg:
		if m.Err != nil {
			f.flash = "download failed: " + m.Err.Error()
		} else {
			f.flash = "downloaded " + m.RemotePath + " → " + m.LocalPath + " (" + humanize.IBytes(uint64(m.Bytes)) + ")"
		}
	case filesOpenMsg:
		if m.Err != nil {
			f.flash = "open failed: " + m.Err.Error()
		} else {
			f.flash = "opened " + m.RemotePath + " (cached at " + m.LocalPath + ")"
		}
	}

	if f.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				f.detail = nil
				return f, nil
			case "o":
				return f, f.openCurrent(*f.detail)
			case "W":
				return f, f.askDownload(*f.detail)
			case "D":
				p := f.detail.Path
				f.confirm.Ask("delete:"+p, "Delete "+p+"?",
					"This recursively removes the file or folder. There is no undo.")
				return f, nil
			case "N":
				p := f.detail.Path
				f.prompt.Ask("rename:"+p, "Rename "+f.detail.Name,
					"Enter a new name (path component only):", f.detail.Name)
				return f, nil
			}
		}
		return f, nil
	}

	if _, handled := f.base.HandleKey(msg, f.rowCount()); handled {
		return f, nil
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			return f, f.drillDown()
		case "backspace", "h":
			return f, f.upDir()
		case "esc":
			if !f.atRoot() {
				return f, f.upDir()
			}
		case "o":
			if f.atRoot() {
				return f, nil
			}
			rows := f.filterFiles()
			if f.base.Cursor() < len(rows) {
				e := rows[f.base.Cursor()]
				if e.IsDir {
					return f, f.drillDown()
				}
				return f, f.openCurrent(e)
			}
		case "W":
			if f.atRoot() {
				return f, nil
			}
			rows := f.filterFiles()
			if f.base.Cursor() < len(rows) {
				e := rows[f.base.Cursor()]
				return f, f.askDownload(e)
			}
		case "D":
			if f.atRoot() {
				return f, nil
			}
			rows := f.filterFiles()
			if f.base.Cursor() < len(rows) {
				e := rows[f.base.Cursor()]
				f.confirm.Ask("delete:"+e.Path, "Delete "+e.Path+"?",
					"This recursively removes the file or folder. There is no undo.")
			}
		case "N":
			if f.atRoot() {
				return f, nil
			}
			rows := f.filterFiles()
			if f.base.Cursor() < len(rows) {
				e := rows[f.base.Cursor()]
				f.prompt.Ask("rename:"+e.Path, "Rename "+e.Name,
					"Enter a new name (path component only):", e.Name)
			}
		}
	}
	return f, nil
}

func (f *Files) drillDown() tea.Cmd {
	if f.atRoot() {
		rows := f.filterRoots()
		if f.base.Cursor() >= len(rows) {
			return nil
		}
		sh := rows[f.base.Cursor()]
		f.stack = append(f.stack, f.filePath)
		f.filePath = sh.Path
		f.base.ResetCursor()
		return f.fetchFiles(f.filePath)
	}
	rows := f.filterFiles()
	if f.base.Cursor() >= len(rows) {
		return nil
	}
	e := rows[f.base.Cursor()]
	if e.IsDir {
		f.stack = append(f.stack, f.filePath)
		f.filePath = e.Path
		f.base.ResetCursor()
		return f.fetchFiles(e.Path)
	}
	f.detail = &e
	return nil
}

func (f *Files) upDir() tea.Cmd {
	if f.atRoot() {
		return nil
	}
	if len(f.stack) > 0 {
		prev := f.stack[len(f.stack)-1]
		f.stack = f.stack[:len(f.stack)-1]
		f.filePath = prev
		f.base.ResetCursor()
		if prev == "" {
			f.files = nil
			f.filesErr = nil
			return nil
		}
		return f.fetchFiles(prev)
	}
	parent := path.Dir(f.filePath)
	if parent == "." || parent == "/" {
		f.filePath = ""
		f.files = nil
		f.filesErr = nil
		f.base.ResetCursor()
		return nil
	}
	f.filePath = parent
	f.base.ResetCursor()
	return f.fetchFiles(parent)
}

func (f *Files) askDownload(e dsm.FSEntry) tea.Cmd {
	if e.IsDir {
		f.flash = "folder download not wired yet (chunked archive needs API work)"
		return nil
	}
	suggested := "~/Downloads/" + e.Name
	f.prompt.Ask("download:"+e.Path, "Download "+e.Name,
		"Save to (use ~ for $HOME):", suggested)
	return nil
}

// openCurrent downloads the file to a per-session temp dir then runs the
// platform opener. We re-use the same temp file on repeated opens of the
// same path so re-launching is instant.
func (f *Files) openCurrent(e dsm.FSEntry) tea.Cmd {
	if e.IsDir {
		return nil
	}
	c := f.ctx.Client
	remote := e.Path
	local := filepath.Join(os.TempDir(), "synoctl-open", strings.ReplaceAll(remote, "/", "_"))
	f.flash = "fetching " + remote + "…"
	return tui.Fetch(5*time.Minute,
		func(ctx context.Context) (string, error) {
			if _, err := os.Stat(local); err != nil {
				if _, err := downloadToFile(ctx, c, remote, local); err != nil {
					return "", err
				}
			}
			return local, openInDefault(local)
		},
		func(p string, err error) tea.Msg {
			return filesOpenMsg{RemotePath: remote, LocalPath: p, Err: err}
		},
	)
}

// — render —

func (f *Files) Render(width, height int) string {
	t := f.ctx.Theme
	if f.confirm.Open() {
		return f.confirm.Render(width, height)
	}
	if f.prompt.Open() {
		return f.prompt.Render(width, height)
	}
	if f.detail != nil {
		return f.renderDetail(width, height, *f.detail)
	}

	var parts []string
	parts = append(parts, f.renderBreadcrumb(width))

	if f.atRoot() {
		rows := f.filterRoots()
		parts = append(parts, sectionHeader(t, width, "Shares", len(rows), f.rootsErr))
		if f.roots == nil && f.rootsErr == nil {
			parts = append(parts, "  "+muted(t, "loading…"))
		} else if len(rows) == 0 {
			parts = append(parts, "  "+muted(t, "(none)"))
		}
		for i, sh := range rows {
			parts = append(parts, f.renderShareRoot(sh, i == f.base.Cursor()))
		}
	} else {
		rows := f.filterFiles()
		parts = append(parts, sectionHeader(t, width, "Contents", len(rows), f.filesErr))
		if f.files == nil && f.filesErr == nil {
			parts = append(parts, "  "+muted(t, "loading…"))
		} else if len(rows) == 0 {
			parts = append(parts, "  "+muted(t, "(empty)"))
		}
		for i, e := range rows {
			parts = append(parts, f.renderFileRow(e, i == f.base.Cursor()))
		}
	}

	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ⏎ open · ⌫ up · o system opener · W download · D delete · N rename · / filter"))
	if f.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+f.flash))
	}
	if v := f.base.FilterFooter(t); v != "" {
		parts = append(parts, v)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (f *Files) renderBreadcrumb(width int) string {
	t := f.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	accent := lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	if f.atRoot() {
		return accent.Render(" / ") + muted.Render("(file station root — pick a share)")
	}
	parts := []string{accent.Render(" / ")}
	segs := strings.Split(strings.TrimPrefix(f.filePath, "/"), "/")
	for i, s := range segs {
		if s == "" {
			continue
		}
		if i == len(segs)-1 {
			parts = append(parts, accent.Render(s))
		} else {
			parts = append(parts, muted.Render(s))
			parts = append(parts, muted.Render(" › "))
		}
	}
	_ = width
	return strings.Join(parts, "")
}

func (f *Files) renderShareRoot(sh dsm.FileShare, highlight bool) string {
	t := f.ctx.Theme
	text := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	totalLine := "—"
	if sh.Add.VolStatus.TotalSpace > 0 {
		free := sh.Add.VolStatus.FreeSpace
		total := sh.Add.VolStatus.TotalSpace
		used := total - free
		totalLine = fmt.Sprintf("%s used / %s", HumanBytes(uint64(used)), HumanBytes(uint64(total)))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		"📂 ", padRight(text.Render(sh.Name), 22), " ",
		padRight(muted.Render(sh.Path), 28), " ",
		padLeft(muted.Render(totalLine), 32),
	)
}

func (f *Files) renderFileRow(e dsm.FSEntry, highlight bool) string {
	t := f.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	accent := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	name := e.Name
	size := "—"
	icon := "  "
	if e.IsDir {
		icon = "📁"
	} else {
		size = humanize.IBytes(uint64(e.Add.Size))
		icon = fileIcon(e.Name)
	}
	mod := "—"
	if e.Add.Time.Mtime > 0 {
		mod = time.Unix(e.Add.Time.Mtime, 0).Format("2006-01-02 15:04")
	}
	display := text.Render(name)
	if e.IsDir {
		display = accent.Render(name)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		icon, " ",
		padRight(display, 36), " ",
		padLeft(text.Render(size), 10), "  ",
		padRight(muted.Render(mod), 20), " ",
		muted.Render(e.Add.Owner.User),
	)
}

func (f *Files) renderDetail(width, _ int, e dsm.FSEntry) string {
	t := f.ctx.Theme
	icon := fileIcon(e.Name)
	if e.IsDir {
		icon = "📁"
	}
	parts := []string{hero(t, width, icon, e.Name, "", e.Path)}
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
	parts = append(parts, noteCard(t, width, "  esc back · o open with system app · W download · D delete · N rename"))
	if f.flash != "" {
		parts = append(parts, noteCard(t, width, "  "+f.flash))
	}
	return strings.Join(parts, "\n")
}

// Inspect renders a compact preview of the currently-cursored entry in
// the right-pane inspector.
func (f *Files) Inspect(width, height int) string {
	if f.atRoot() {
		rows := f.filterRoots()
		if f.base.Cursor() >= len(rows) {
			return ""
		}
		t := f.ctx.Theme
		sh := rows[f.base.Cursor()]
		muted := lipgloss.NewStyle().Foreground(t.Muted)
		text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
		parts := []string{
			t.Title().Render(" " + sh.Name + " "),
			"",
			muted.Render(sh.Path),
		}
		if sh.Add.VolStatus.TotalSpace > 0 {
			free := sh.Add.VolStatus.FreeSpace
			total := sh.Add.VolStatus.TotalSpace
			used := total - free
			ratio := float64(used) / float64(total)
			parts = append(parts,
				"",
				Gauge(t, width-2, ratio),
				fmt.Sprintf("%s used of %s", HumanBytes(uint64(used)), HumanBytes(uint64(total))),
			)
		}
		if sh.Add.Owner.User != "" {
			parts = append(parts, "", muted.Render("Owner: ")+text.Render(sh.Add.Owner.User))
		}
		_ = height
		return strings.Join(parts, "\n")
	}
	rows := f.filterFiles()
	if f.base.Cursor() >= len(rows) {
		return ""
	}
	e := rows[f.base.Cursor()]
	t := f.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	size := "(folder)"
	if !e.IsDir {
		size = humanize.IBytes(uint64(e.Add.Size))
	}
	parts := []string{
		t.Title().Render(" " + e.Name + " "),
		"",
		muted.Render(e.Path),
		"",
		muted.Render("Size:    ") + text.Render(size),
	}
	if e.Add.Time.Mtime > 0 {
		parts = append(parts, muted.Render("Modified:")+" "+text.Render(time.Unix(e.Add.Time.Mtime, 0).Format("2006-01-02 15:04")))
	}
	if e.Add.Owner.User != "" {
		parts = append(parts, muted.Render("Owner:   ")+text.Render(e.Add.Owner.User))
	}
	if e.Add.Perm.POSIX != 0 {
		parts = append(parts, muted.Render("Perms:   ")+text.Render(fmt.Sprintf("%o", e.Add.Perm.POSIX)))
	}
	_ = height
	return strings.Join(parts, "\n")
}

// — helpers —

// fileIcon picks a tasteful unicode glyph based on the extension.
func fileIcon(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".heic", ".webp", ".bmp", ".tiff":
		return "🖼"
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".m4v":
		return "🎬"
	case ".mp3", ".flac", ".wav", ".ogg", ".aac", ".m4a":
		return "♪ "
	case ".pdf":
		return "📄"
	case ".zip", ".tar", ".gz", ".7z", ".bz2", ".rar", ".xz":
		return "📦"
	case ".doc", ".docx", ".odt", ".txt", ".md", ".rtf":
		return "📝"
	case ".xls", ".xlsx", ".ods", ".csv":
		return "📊"
	case ".ppt", ".pptx", ".odp":
		return "▦ "
	case ".go", ".js", ".ts", ".py", ".rs", ".c", ".cpp", ".sh", ".yaml", ".yml", ".json", ".toml":
		return "{}"
	default:
		return "·"
	}
}

// downloadToFile streams a FileStation download to a local file, creating
// parent directories as needed.
func downloadToFile(ctx context.Context, c *dsm.Client, remote, local string) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return 0, err
	}
	rc, _, err := c.FileDownload(ctx, remote)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	out, err := os.Create(local)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	return io.Copy(out, rc)
}

// expandHome turns a leading ~ into $HOME.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// openInDefault hands a local file off to the platform's "open with
// default" handler. Errors propagate — if there is no opener on this
// platform we surface the original os/exec error rather than swallowing
// it, so the user gets a real diagnostic instead of a confused UI.
func openInDefault(local string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", local)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", local)
	default:
		cmd = exec.Command("xdg-open", local)
	}
	return cmd.Start()
}
