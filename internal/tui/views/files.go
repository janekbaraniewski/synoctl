package views

import (
	"context"
	"crypto/sha1"
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

// Files is the full-pane file browser. It boots into a share tree: enter on
// a share or folder expands/collapses it inline, while enter on a file opens
// its detail view. Beyond browse + rename + delete the view exposes two
// download modes:
//
//   - `o` — download to a temp dir then hand off to the system opener
//     (open / xdg-open / start). Synaesthesia of "double-click in a file
//     manager" — good for previewing PDFs, images, video.
//   - `W` — download to a user-chosen path (prompted).
type Files struct {
	ctx Ctx

	roots     []dsm.FileShare // top-level FileStation shares.
	rootsErr  error
	shares    []dsm.Share // Core.Share metadata keyed by shared-folder name.
	sharesErr error

	children map[string][]dsm.FSEntry
	childErr map[string]error
	expanded map[string]bool
	loading  map[string]bool
	sizes    map[string]fileSizeState
	sizeGen  int64

	base        listBase
	detail      *dsm.FSEntry
	shareDetail *dsm.Share
	confirm     *Confirm
	prompt      *Prompt
	otp         *OTPModal
	flash       string

	snapshotShare *dsm.Share
	snapshots     []dsm.Snapshot
	snapshotErr   error
	snapLoading   bool
	snapCursor    int
	pendingOp     pendingSnapshotOp
}

// NewFiles constructs the file browser.
func NewFiles(c Ctx) tui.View {
	return &Files{
		ctx:      c,
		children: map[string][]dsm.FSEntry{},
		childErr: map[string]error{},
		expanded: map[string]bool{},
		loading:  map[string]bool{},
		sizes:    map[string]fileSizeState{},
		confirm:  NewConfirm(c.Theme),
		prompt:   NewPrompt(c.Theme),
		otp:      NewOTPModal(c.Theme),
	}
}

type fileSizeState struct {
	Size   int64
	Sized  bool
	Sizing bool
	Err    error
	Gen    int64
}

type fileTreeRowKind int

const (
	fileTreeRoot fileTreeRowKind = iota
	fileTreeEntry
	fileTreeStatus
)

type fileTreeRow struct {
	Kind   fileTreeRowKind
	Level  int
	Root   dsm.FileShare
	Entry  dsm.FSEntry
	Status string
	Err    error
}

func (f *Files) Name() string                   { return "files" }
func (f *Files) Title() string                  { return "Shares & Files" }
func (f *Files) Icon() string                   { return "▦" }
func (f *Files) RefreshInterval() time.Duration { return 30 * time.Second }
func (f *Files) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("backspace", "h"), key.WithHelp("⌫/h", "collapse")),
		key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "open with system app")),
		key.NewBinding(key.WithKeys("W"), key.WithHelp("W", "download…")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete (confirm)")),
		key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "rename")),
		key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "snapshots")),
		key.NewBinding(key.WithKeys("I"), key.WithHelp("I", "share details")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "re-size usage")),
	)
}

func (f *Files) Hint() string {
	if f.snapshotShare != nil {
		return "↑/↓ move · c create · D delete · r refresh · esc back"
	}
	return "⏎ expand/open · S snapshots · I share details · o open · W download · D delete · N rename · R re-size · / filter"
}

func (f *Files) Init() tea.Cmd { return tea.Batch(f.fetchRoots(), f.fetchShares()) }

// IsTextEditing tells the shell to suppress global keybindings (q quit,
// a actions, …) while the user is filling in a prompt — otherwise typed
// runes would never reach the input.
func (f *Files) IsTextEditing() bool {
	return f.prompt.Open() || f.confirm.Open() || f.otp.Open() || f.base.filter.IsActive()
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

func (f *Files) fetchShares() tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Share, error) { return c.Shares(ctx) },
		func(v []dsm.Share, err error) tea.Msg { return sharesMsg{S: v, Err: err} },
	)
}

func (f *Files) fetchFiles(p string) tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	f.ensureTreeMaps()
	f.loading[p] = true
	delete(f.childErr, p)
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.FSEntry, error) {
			items, _, err := c.ListFiles(ctx, p, 0, 500)
			return items, err
		},
		func(e []dsm.FSEntry, err error) tea.Msg { return filesListMsg{Path: p, E: e, Err: err} },
	)
}

func (f *Files) dirSizeCmd(dirPath string) tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	f.sizeGen++
	gen := f.sizeGen
	return tui.Fetch(30*time.Minute,
		func(ctx context.Context) (dsm.DirSizeResult, error) { return c.DirSize(ctx, dirPath) },
		func(r dsm.DirSizeResult, err error) tea.Msg {
			return filesSizedMsg{
				Path:     dirPath,
				Size:     r.Total,
				NumDirs:  r.NumDirs,
				NumFiles: r.NumFiles,
				Gen:      gen,
				Err:      err,
			}
		},
	)
}

// — row model —

func (f *Files) rowCount() int {
	return len(f.visibleRows())
}

func (f *Files) ensureTreeMaps() {
	if f.children == nil {
		f.children = map[string][]dsm.FSEntry{}
	}
	if f.childErr == nil {
		f.childErr = map[string]error{}
	}
	if f.expanded == nil {
		f.expanded = map[string]bool{}
	}
	if f.loading == nil {
		f.loading = map[string]bool{}
	}
	if f.sizes == nil {
		f.sizes = map[string]fileSizeState{}
	}
}

func (f *Files) shareByName(name string) (dsm.Share, bool) {
	for _, sh := range f.shares {
		if sh.Name == name {
			return sh, true
		}
	}
	return dsm.Share{}, false
}

func (f *Files) shareForPath(p string) (dsm.Share, bool) {
	name := shareNameFromPath(p)
	if name == "" {
		return dsm.Share{}, false
	}
	return f.shareByName(name)
}

func shareNameFromPath(p string) string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return ""
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i]
	}
	return p
}

func (f *Files) visibleRows() []fileTreeRow {
	filter := f.base.FilterValue()
	out := make([]fileTreeRow, 0, len(f.roots))
	for _, sh := range f.roots {
		out = append(out, f.rootRows(sh, 0, filter)...)
	}
	return out
}

func (f *Files) rootRows(sh dsm.FileShare, level int, filter string) []fileTreeRow {
	row := fileTreeRow{Kind: fileTreeRoot, Level: level, Root: sh}
	childRows := f.childRows(sh.Path, level+1, filter)
	cells := []string{sh.Name, sh.Path}
	if meta, ok := f.shareByName(sh.Name); ok {
		cells = append(cells, meta.Path, meta.Desc)
	}
	selfMatch := filter == "" || MatchesAll(filter, cells...)
	if filter != "" && !selfMatch && len(childRows) == 0 {
		return nil
	}
	return append([]fileTreeRow{row}, childRows...)
}

func (f *Files) entryRows(e dsm.FSEntry, level int, filter string) []fileTreeRow {
	row := fileTreeRow{Kind: fileTreeEntry, Level: level, Entry: e}
	childRows := f.childRows(e.Path, level+1, filter)
	selfMatch := filter == "" || MatchesAll(filter, e.Name, e.Path, e.Type)
	if filter != "" && !selfMatch && len(childRows) == 0 {
		return nil
	}
	return append([]fileTreeRow{row}, childRows...)
}

func (f *Files) childRows(parent string, level int, filter string) []fileTreeRow {
	if !f.expanded[parent] {
		return nil
	}
	var out []fileTreeRow
	if filter == "" {
		if f.loading[parent] {
			out = append(out, fileTreeRow{Kind: fileTreeStatus, Level: level, Status: "loading..."})
		}
		if err := f.childErr[parent]; err != nil {
			out = append(out, fileTreeRow{Kind: fileTreeStatus, Level: level, Status: err.Error(), Err: err})
		}
	}
	children, ok := f.children[parent]
	if !ok {
		return out
	}
	if filter == "" && len(children) == 0 && !f.loading[parent] && f.childErr[parent] == nil {
		out = append(out, fileTreeRow{Kind: fileTreeStatus, Level: level, Status: "(empty)"})
		return out
	}
	for _, child := range children {
		out = append(out, f.entryRows(child, level, filter)...)
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
type filesSizedMsg struct {
	Path     string
	Size     int64
	NumDirs  int64
	NumFiles int64
	Gen      int64
	Err      error
}

// — update —

func (f *Files) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if handled, cmd := f.confirm.Update(msg); handled {
		return f, cmd
	}
	if handled, cmd := f.prompt.Update(msg); handled {
		return f, cmd
	}
	if handled, cmd := f.otp.Update(msg); handled {
		return f, cmd
	}

	switch m := msg.(type) {
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "snapshot.delete:"); ok {
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 2 {
				share, name := parts[0], parts[1]
				f.pendingOp = pendingSnapshotOp{kind: "delete", share: share, snapshot: name}
				f.otp.Ask("snapshot.delete:"+share+"/"+name,
					fmt.Sprintf("OTP needed to delete snapshot %s of %q.", name, share))
				return f, nil
			}
		}
		if rest, ok := strings.CutPrefix(m.Token, "delete:"); ok {
			f.flash = "deleting " + rest + "…"
			c := f.ctx.Client
			return f, tui.Fetch(30*time.Second,
				func(ctx context.Context) (struct{}, error) { return struct{}{}, c.FileDelete(ctx, rest, true) },
				func(_ struct{}, err error) tea.Msg { return filesDeleteMsg{Path: rest, Err: err} },
			)
		}
	case SubmittedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "snapshot.create.desc:"); ok {
			f.pendingOp = pendingSnapshotOp{kind: "create", share: rest, description: strings.TrimSpace(m.Value)}
			f.otp.Ask("snapshot.create:"+rest, fmt.Sprintf("OTP needed to create a snapshot of %q.", rest))
			return f, nil
		}
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
					return downloadToFile(ctx, c, remote, local, -1)
				},
				func(n int64, err error) tea.Msg {
					return filesDownloadMsg{RemotePath: remote, LocalPath: local, Bytes: n, Err: err}
				},
			)
		}
	case CancelledMsg:
		f.pendingOp = pendingSnapshotOp{}
		f.flash = "cancelled"
		return f, nil
	case OTPProvidedMsg:
		switch f.pendingOp.kind {
		case "create":
			share, desc := f.pendingOp.share, f.pendingOp.description
			f.flash = "creating snapshot of " + share + "…"
			f.pendingOp = pendingSnapshotOp{}
			return f, f.createSnapshotCmd(share, desc, m.Code)
		case "delete":
			share, name := f.pendingOp.share, f.pendingOp.snapshot
			f.flash = "deleting snapshot " + name + " of " + share + "…"
			f.pendingOp = pendingSnapshotOp{}
			return f, f.deleteSnapshotCmd(share, name, m.Code)
		}
	case OTPCancelledMsg:
		f.pendingOp = pendingSnapshotOp{}
		f.flash = "cancelled (OTP)"
		return f, nil
	case tui.TickMsg:
		return f, tea.Batch(f.fetchRoots(), f.fetchShares())
	case sharesMsg:
		f.shares, f.sharesErr = m.S, m.Err
		f.base.ClampCursor(f.rowCount())
		return f, nil
	case filesRootsMsg:
		f.roots, f.rootsErr = m.R, m.Err
		f.base.ClampCursor(f.rowCount())
		return f, f.kickPendingSizes()
	case filesListMsg:
		f.ensureTreeMaps()
		f.loading[m.Path] = false
		if m.Err != nil {
			f.childErr[m.Path] = m.Err
		} else {
			f.children[m.Path] = m.E
			delete(f.childErr, m.Path)
		}
		f.base.ClampCursor(f.rowCount())
		return f, f.kickPendingSizes()
	case filesSizedMsg:
		f.applySized(m)
		f.base.ClampCursor(f.rowCount())
		return f, f.kickPendingSizes()
	case snapshotsListedMsg:
		if f.snapshotShare == nil || m.Share != f.snapshotShare.Name {
			return f, nil
		}
		f.snapLoading = false
		f.snapshots, f.snapshotErr = m.Items, m.Err
		if f.snapCursor >= len(f.snapshots) {
			f.snapCursor = len(f.snapshots) - 1
		}
		if f.snapCursor < 0 {
			f.snapCursor = 0
		}
		return f, nil
	case snapshotActionMsg:
		if m.Err != nil {
			if dsm.IsOTPStepupRequired(m.Err) {
				f.flash = "OTP rejected, try again"
				switch m.Kind {
				case "create":
					f.otp.Ask("snapshot.create:"+m.Share, fmt.Sprintf("OTP rejected. Try again for %q.", m.Share))
					return f, nil
				case "delete":
					f.otp.Ask("snapshot.delete:"+m.Share+"/"+m.Snapshot, fmt.Sprintf("OTP rejected. Try again for %s.", m.Snapshot))
					return f, nil
				}
			}
			f.flash = m.Kind + " failed: " + m.Err.Error()
		} else {
			switch m.Kind {
			case "create":
				f.flash = "snapshot of " + m.Share + " created"
			case "delete":
				f.flash = "snapshot " + m.Snapshot + " deleted"
			}
		}
		if f.snapshotShare != nil {
			return f, f.fetchSnapshots(f.snapshotShare.Name)
		}
		return f, nil
	case filesDeleteMsg:
		if m.Err != nil {
			f.flash = "delete failed: " + m.Err.Error()
		} else {
			f.flash = "deleted " + m.Path
		}
		return f, f.refreshParent(m.Path)
	case filesRenameMsg:
		if m.Err != nil {
			f.flash = "rename failed: " + m.Err.Error()
		} else {
			f.flash = "renamed → " + m.NewName
		}
		return f, f.refreshParent(m.Path)
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
			f.flash = "sent " + m.RemotePath + " to the default app (cached at " + m.LocalPath + ")"
		}
	}

	if f.snapshotShare != nil {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				f.snapshotShare = nil
				f.snapshots = nil
				f.snapshotErr = nil
				f.snapCursor = 0
				return f, nil
			case "j", "down":
				if f.snapCursor < len(f.snapshots)-1 {
					f.snapCursor++
				}
				return f, nil
			case "k", "up":
				if f.snapCursor > 0 {
					f.snapCursor--
				}
				return f, nil
			case "g":
				f.snapCursor = 0
				return f, nil
			case "G":
				if len(f.snapshots) > 0 {
					f.snapCursor = len(f.snapshots) - 1
				}
				return f, nil
			case "c":
				share := f.snapshotShare.Name
				f.prompt.Ask("snapshot.create.desc:"+share,
					"Snapshot description",
					"Optional note attached to the snapshot. Press ⏎ to accept (blank ok).",
					"")
				return f, nil
			case "D":
				if f.snapCursor < len(f.snapshots) {
					share := f.snapshotShare.Name
					snap := f.snapshots[f.snapCursor]
					if snap.Locked {
						f.flash = snap.Name + " is locked — DSM blocks deletion until unlocked"
						return f, nil
					}
					f.confirm.Ask("snapshot.delete:"+share+"/"+snap.Name,
						"Delete snapshot "+snap.Name+"?",
						"This permanently removes the Btrfs snapshot. There is no undo.")
				}
				return f, nil
			case "r":
				return f, f.fetchSnapshots(f.snapshotShare.Name)
			}
		}
		return f, nil
	}

	if f.shareDetail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			f.shareDetail = nil
		}
		return f, nil
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
		return f, f.kickPendingSizes()
	}

	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			return f, f.drillDown()
		case "backspace", "h":
			return f, f.upDir()
		case "esc":
			return f, f.upDir()
		case "r":
			return f, tea.Batch(f.fetchRoots(), f.fetchShares())
		case "R":
			f.forgetVisibleSizes()
			return f, f.kickPendingSizes()
		case "S":
			return f, f.openSnapshotsForCursor()
		case "I":
			f.openShareDetailForCursor()
			return f, nil
		case "o":
			rows := f.visibleRows()
			if f.base.Cursor() < len(rows) {
				row := rows[f.base.Cursor()]
				if row.Kind == fileTreeRoot || (row.Kind == fileTreeEntry && row.Entry.IsDir) {
					return f, f.drillDown()
				}
				if row.Kind == fileTreeEntry {
					return f, f.openCurrent(row.Entry)
				}
			}
		case "W":
			rows := f.visibleRows()
			if f.base.Cursor() < len(rows) {
				row := rows[f.base.Cursor()]
				if row.Kind == fileTreeEntry {
					return f, f.askDownload(row.Entry)
				}
			}
		case "D":
			rows := f.visibleRows()
			if f.base.Cursor() < len(rows) {
				row := rows[f.base.Cursor()]
				if row.Kind == fileTreeEntry {
					e := row.Entry
					f.confirm.Ask("delete:"+e.Path, "Delete "+e.Path+"?",
						"This recursively removes the file or folder. There is no undo.")
				}
			}
		case "N":
			rows := f.visibleRows()
			if f.base.Cursor() < len(rows) {
				row := rows[f.base.Cursor()]
				if row.Kind == fileTreeEntry {
					e := row.Entry
					f.prompt.Ask("rename:"+e.Path, "Rename "+e.Name,
						"Enter a new name (path component only):", e.Name)
				}
			}
		}
	}
	return f, nil
}

func (f *Files) drillDown() tea.Cmd {
	rows := f.visibleRows()
	if f.base.Cursor() >= len(rows) {
		return nil
	}
	row := rows[f.base.Cursor()]
	switch row.Kind {
	case fileTreeRoot:
		return f.toggleDir(row.Root.Path)
	case fileTreeEntry:
		if row.Entry.IsDir {
			return f.toggleDir(row.Entry.Path)
		}
		e := row.Entry
		f.detail = &e
	}
	return nil
}

func (f *Files) upDir() tea.Cmd {
	rows := f.visibleRows()
	if f.base.Cursor() >= len(rows) {
		return nil
	}
	row := rows[f.base.Cursor()]
	if row.Kind == fileTreeRoot && f.expanded[row.Root.Path] {
		delete(f.expanded, row.Root.Path)
		f.base.ClampCursor(f.rowCount())
		return nil
	}
	if row.Kind == fileTreeEntry && row.Entry.IsDir && f.expanded[row.Entry.Path] {
		delete(f.expanded, row.Entry.Path)
		f.base.ClampCursor(f.rowCount())
		return nil
	}
	for i := f.base.Cursor() - 1; i >= 0; i-- {
		parent := rows[i]
		if parent.Level >= row.Level || !parent.isDir() {
			continue
		}
		delete(f.expanded, parent.path())
		f.base.cursor = i
		f.base.ClampCursor(f.rowCount())
		return nil
	}
	return nil
}

func (f *Files) toggleDir(p string) tea.Cmd {
	f.ensureTreeMaps()
	if f.expanded[p] {
		delete(f.expanded, p)
		f.base.ClampCursor(f.rowCount())
		return nil
	}
	f.expanded[p] = true
	if _, ok := f.children[p]; ok {
		return f.kickPendingSizes()
	}
	if f.loading[p] {
		return f.kickPendingSizes()
	}
	return tea.Batch(f.fetchFiles(p), f.kickPendingSizes())
}

func (f *Files) refreshParent(p string) tea.Cmd {
	parent := path.Dir(p)
	if parent == "." || parent == "/" {
		return nil
	}
	f.ensureTreeMaps()
	delete(f.children, p)
	delete(f.childErr, p)
	delete(f.expanded, p)
	for cur := p; cur != "." && cur != "/"; cur = path.Dir(cur) {
		delete(f.sizes, cur)
	}
	if f.expanded[parent] {
		return f.fetchFiles(parent)
	}
	return nil
}

func (r fileTreeRow) isDir() bool {
	return r.Kind == fileTreeRoot || (r.Kind == fileTreeEntry && r.Entry.IsDir)
}

func (r fileTreeRow) path() string {
	switch r.Kind {
	case fileTreeRoot:
		return r.Root.Path
	case fileTreeEntry:
		return r.Entry.Path
	default:
		return ""
	}
}

func (f *Files) kickPendingSizes() tea.Cmd {
	const maxInflight = 2
	if f.ctx.Client == nil {
		return nil
	}
	f.ensureTreeMaps()
	inflight := 0
	for _, st := range f.sizes {
		if st.Sizing {
			inflight++
		}
	}
	var cmds []tea.Cmd
	start := func(p string) {
		if inflight >= maxInflight || p == "" {
			return
		}
		st := f.sizes[p]
		if st.Sized || st.Sizing || st.Err != nil {
			return
		}
		st.Sizing = true
		st.Sized = false
		st.Gen = f.sizeGen + 1
		f.sizes[p] = st
		cmds = append(cmds, f.dirSizeCmd(p))
		inflight++
	}
	for _, p := range f.sizeCandidates() {
		start(p)
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (f *Files) sizeCandidates() []string {
	rows := f.visibleRows()
	if len(rows) == 0 {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(rows))
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		out = append(out, p)
	}

	cursor := f.base.Cursor()
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	if cursor >= 0 {
		row := rows[cursor]
		if row.isDir() {
			add(row.path())
		}
		for i := cursor - 1; i >= 0; i-- {
			parent := rows[i]
			if parent.Level < row.Level && parent.isDir() {
				add(parent.path())
				break
			}
		}
	}

	for i := 0; i < len(rows); i++ {
		left := cursor - i
		right := cursor + i
		if left >= 0 && rows[left].isDir() {
			add(rows[left].path())
		}
		if right != left && right < len(rows) && rows[right].isDir() {
			add(rows[right].path())
		}
	}
	return out
}

func (f *Files) applySized(m filesSizedMsg) {
	f.ensureTreeMaps()
	st := f.sizes[m.Path]
	if st.Gen != m.Gen {
		return
	}
	st.Sizing = false
	if m.Err != nil {
		st.Err = m.Err
	} else {
		st.Size = m.Size
		st.Sized = true
		st.Err = nil
	}
	f.sizes[m.Path] = st
}

func (f *Files) forgetVisibleSizes() {
	f.ensureTreeMaps()
	rows := f.visibleRows()
	for _, row := range rows {
		if row.isDir() {
			delete(f.sizes, row.path())
		}
	}
}

func (f *Files) sizeLabel(p string, isDir bool, fileSize int64) string {
	if !isDir {
		return humanize.IBytes(uint64(fileSize))
	}
	st := f.sizes[p]
	switch {
	case st.Err != nil:
		return "err"
	case st.Sizing:
		return "sizing..."
	case st.Sized:
		return HumanBytes(uint64(st.Size))
	default:
		return "pending"
	}
}

func (f *Files) sizeErr(p string) error {
	f.ensureTreeMaps()
	return f.sizes[p].Err
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

func (f *Files) fetchSnapshots(share string) tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	f.snapLoading = true
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.Snapshot, error) { return c.Snapshots(ctx, share) },
		func(items []dsm.Snapshot, err error) tea.Msg {
			return snapshotsListedMsg{Share: share, Items: items, Err: err}
		},
	)
}

func (f *Files) createSnapshotCmd(share, desc, otp string) tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.CreateSnapshot(ctx, share, desc, otp)
		},
		func(_ struct{}, err error) tea.Msg {
			return snapshotActionMsg{Kind: "create", Share: share, Err: err}
		},
	)
}

func (f *Files) deleteSnapshotCmd(share, name, otp string) tea.Cmd {
	c := f.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.DeleteSnapshot(ctx, share, name, otp)
		},
		func(_ struct{}, err error) tea.Msg {
			return snapshotActionMsg{Kind: "delete", Share: share, Snapshot: name, Err: err}
		},
	)
}

func (f *Files) openSnapshotsForCursor() tea.Cmd {
	rows := f.visibleRows()
	if f.base.Cursor() >= len(rows) {
		return nil
	}
	if sh, ok := f.shareForPath(rows[f.base.Cursor()].path()); ok {
		f.snapshotShare = &sh
		f.snapshots = nil
		f.snapshotErr = nil
		f.snapCursor = 0
		return f.fetchSnapshots(sh.Name)
	}
	f.flash = "share metadata is not loaded yet"
	return nil
}

func (f *Files) openShareDetailForCursor() {
	rows := f.visibleRows()
	if f.base.Cursor() >= len(rows) {
		return
	}
	if sh, ok := f.shareForPath(rows[f.base.Cursor()].path()); ok {
		f.shareDetail = &sh
		return
	}
	f.flash = "share metadata is not loaded yet"
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
	local := openCachePath(e)
	f.flash = "fetching " + remote + "…"
	return tui.Fetch(5*time.Minute,
		func(ctx context.Context) (string, error) {
			if !validCachedFile(local, e.Add.Size) {
				if _, err := downloadToFile(ctx, c, remote, local, e.Add.Size); err != nil {
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
	if f.otp.Open() {
		return f.otp.Render(width, height)
	}
	if f.confirm.Open() {
		return f.confirm.Render(width, height)
	}
	if f.prompt.Open() {
		return f.prompt.Render(width, height)
	}
	if f.snapshotShare != nil {
		return f.renderSnapshots(width, height)
	}
	if f.shareDetail != nil {
		return renderShareDetail(t, width, height, *f.shareDetail)
	}
	if f.detail != nil {
		return f.renderDetail(width, height, *f.detail)
	}

	var parts []string
	parts = append(parts, f.renderBreadcrumb(width))

	rows := f.visibleRows()
	parts = append(parts, sectionHeader(t, width, "Shared folders, files, usage", len(rows), f.loadErr()))
	if f.roots == nil && f.rootsErr == nil {
		parts = append(parts, "  "+muted(t, "loading..."))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for i, row := range rows {
		parts = append(parts, f.renderTreeRow(row, i == f.base.Cursor()))
	}

	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ⏎ expand/open · ⌫ collapse · S snapshots · I share details · o open · W download · D delete · N rename · R re-size · / filter"))
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
	_ = width
	return accent.Render(" storage / ") + muted.Render("(shares, files, usage)")
}

func (f *Files) loadErr() error {
	if f.rootsErr != nil {
		return f.rootsErr
	}
	return f.sharesErr
}

func (f *Files) renderTreeRow(row fileTreeRow, highlight bool) string {
	t := f.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text)
	accent := lipgloss.NewStyle().Foreground(t.Accent2).Bold(true)
	indent := strings.Repeat("  ", row.Level)
	if row.Kind == fileTreeStatus {
		style := muted
		if row.Err != nil {
			style = lipgloss.NewStyle().Foreground(t.Error)
		}
		return lipgloss.JoinHorizontal(lipgloss.Center,
			caretGlyph(t, highlight), " ",
			indent, "  ", style.Render(row.Status),
		)
	}

	nameWidth := max(36-row.Level*2, 14)
	expander := " "
	icon := "  "
	name := ""
	size := "—"
	info := "—"
	tail := ""
	if row.Kind == fileTreeRoot {
		sh := row.Root
		rowPath := sh.Path
		if f.expanded[sh.Path] {
			expander = "▾"
		} else {
			expander = "▸"
		}
		icon = "📂"
		name = accent.Render(sh.Name)
		size = f.sizeLabel(sh.Path, true, 0)
		if meta, ok := f.shareByName(sh.Name); ok {
			info = f.quotaLine(meta)
			tail = f.flagText(meta)
		} else {
			info = muted.Render("share metadata...")
		}
		if err := f.sizeErr(rowPath); err != nil {
			tail = strings.TrimSpace(strings.Join([]string{tail, clipTo("size: "+err.Error(), 42)}, " "))
		}
	} else {
		e := row.Entry
		rowPath := e.Path
		if e.IsDir {
			if f.expanded[e.Path] {
				expander = "▾"
			} else {
				expander = "▸"
			}
			icon = "📁"
			name = accent.Render(e.Name)
			size = f.sizeLabel(e.Path, true, 0)
		} else {
			icon = fileIcon(e.Name)
			name = text.Render(e.Name)
			size = f.sizeLabel(e.Path, false, e.Add.Size)
		}
		if e.Add.Time.Mtime > 0 {
			info = time.Unix(e.Add.Time.Mtime, 0).Format("2006-01-02 15:04")
		}
		tail = e.Add.Owner.User
		if err := f.sizeErr(rowPath); err != nil {
			tail = strings.TrimSpace(strings.Join([]string{tail, clipTo("size: "+err.Error(), 42)}, " "))
		}
	}
	sizeStyle := text
	if size == "err" {
		sizeStyle = lipgloss.NewStyle().Foreground(t.Error)
	} else if size == "sizing..." || size == "pending" || size == "—" {
		sizeStyle = muted
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		indent, expander, " ", icon, " ",
		padRight(name, nameWidth), " ",
		padLeft(sizeStyle.Render(size), 12), "  ",
		padRight(muted.Render(info), 24), " ",
		muted.Render(tail),
	)
}

func (f *Files) quotaLine(sh dsm.Share) string {
	if sh.ShareQuota <= 0 {
		return "quota —"
	}
	ratio := float64(sh.ShareQuotaUsed) / float64(sh.ShareQuota)
	return fmt.Sprintf("%5.1f%% %s / %s", ratio*100,
		HumanBytes(uint64(sh.ShareQuotaUsed)*1024*1024),
		HumanBytes(uint64(sh.ShareQuota)*1024*1024))
}

func (f *Files) flagText(sh dsm.Share) string {
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
	if sh.IsUsbShare {
		flags = append(flags, "usb")
	}
	if sh.IsSyncShare {
		flags = append(flags, "sync")
	}
	if sh.IsCloudSync {
		flags = append(flags, "cloud-sync")
	}
	return strings.Join(flags, " ")
}

func (f *Files) flagList(sh dsm.Share) []string {
	t := f.ctx.Theme
	chip := func(label string, on bool) string {
		st := t.HealthStyle("disabled")
		if on {
			st = t.HealthStyle("enabled")
		}
		return st.Render(" " + label + " ")
	}
	var out []string
	if sh.IsEncrypted() {
		out = append(out, chip("encrypted", true))
	}
	if sh.EnableRecycle {
		out = append(out, chip("recycle bin", true))
	}
	if sh.Hidden {
		out = append(out, chip("hidden", true))
	}
	if sh.Readonly {
		out = append(out, chip("read-only", true))
	}
	if sh.IsUsbShare {
		out = append(out, chip("usb", true))
	}
	if sh.IsSyncShare {
		out = append(out, chip("sync", true))
	}
	if sh.IsCloudSync {
		out = append(out, chip("cloud-sync", true))
	}
	return out
}

func (f *Files) renderSnapshots(width, height int) string {
	t := f.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	title := t.Title().Render(" Snapshots · " + f.snapshotShare.Name + " ")
	header := sectionHeader(t, width, "Snapshots · "+f.snapshotShare.Name, len(f.snapshots), f.snapshotErr)

	var parts []string
	parts = append(parts, title)
	parts = append(parts, header)

	if f.snapLoading && len(f.snapshots) == 0 {
		parts = append(parts, "  "+muted.Render("listing..."))
	} else if len(f.snapshots) == 0 && f.snapshotErr == nil {
		parts = append(parts, "  "+muted.Render("(no snapshots taken yet - press `c` to create one)"))
	}

	for i, sn := range f.snapshots {
		parts = append(parts, f.renderSnapshotRow(sn, i == f.snapCursor))
	}

	parts = append(parts, "")
	parts = append(parts, muted.Render(
		"  ↑/↓ move · c create · D delete · r refresh · esc back"))
	if f.flash != "" {
		parts = append(parts, muted.Render("  "+f.flash))
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (f *Files) renderSnapshotRow(sn dsm.Snapshot, highlight bool) string {
	t := f.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	accent := lipgloss.NewStyle().Foreground(t.Accent2)
	when := "—"
	if sn.Time > 0 {
		when = time.Unix(sn.Time, 0).Format("2006-01-02 15:04:05")
	}
	flags := []string{}
	if sn.Locked {
		flags = append(flags, accent.Render("locked"))
	}
	if sn.Schedule {
		flags = append(flags, muted.Render("scheduled"))
	}
	flagStr := strings.Join(flags, " ")
	desc := sn.Description
	if desc == "" {
		desc = muted.Render("(no description)")
	} else {
		desc = text.Render(desc)
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(sn.Name), 32), " ",
		padRight(muted.Render(when), 22), " ",
		padRight(flagStr, 16), " ",
		desc,
	)
}

func (f *Files) renderDetail(width, _ int, e dsm.FSEntry) string {
	t := f.ctx.Theme
	icon := fileIcon(e.Name)
	if e.IsDir {
		icon = "📁"
	}
	parts := []string{hero(t, width, icon, e.Name, "", e.Path)}
	size := f.sizeLabel(e.Path, e.IsDir, e.Add.Size)
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
	if err := f.sizeErr(e.Path); err != nil {
		props = append(props, [2]string{"Size error", err.Error()})
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
	if f.snapshotShare != nil {
		if len(f.snapshots) == 0 || f.snapCursor >= len(f.snapshots) {
			return ""
		}
		t := f.ctx.Theme
		sn := f.snapshots[f.snapCursor]
		muted := lipgloss.NewStyle().Foreground(t.Muted)
		text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
		when := "—"
		if sn.Time > 0 {
			when = time.Unix(sn.Time, 0).Format("2006-01-02 15:04:05")
		}
		parts := []string{
			t.Title().Render(" snapshot "),
			"",
			muted.Render(sn.Name),
			"",
			muted.Render("Taken:    ") + text.Render(when),
			muted.Render("Share:    ") + text.Render(f.snapshotShare.Name),
		}
		if sn.Locked {
			parts = append(parts, "", t.Chip(t.Warn).Render(" locked "))
		}
		if sn.Schedule {
			parts = append(parts, muted.Render("Source:   ")+text.Render("DSM scheduler"))
		}
		if sn.Description != "" {
			parts = append(parts, "", muted.Render("Description"))
			parts = append(parts, text.Render("  "+sn.Description))
		}
		_ = width
		_ = height
		return strings.Join(parts, "\n")
	}

	rows := f.visibleRows()
	if f.base.Cursor() >= len(rows) {
		return ""
	}
	row := rows[f.base.Cursor()]
	if row.Kind == fileTreeStatus {
		t := f.ctx.Theme
		return lipgloss.NewStyle().Foreground(t.Muted).Render(row.Status)
	}
	if row.Kind == fileTreeRoot {
		t := f.ctx.Theme
		sh := row.Root
		muted := lipgloss.NewStyle().Foreground(t.Muted)
		text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
		parts := []string{
			t.Title().Render(" " + sh.Name + " "),
			"",
			muted.Render(sh.Path),
		}
		if meta, ok := f.shareByName(sh.Name); ok {
			if meta.Path != "" {
				parts = append(parts, muted.Render("Volume:  ")+text.Render(meta.Path))
			}
			if meta.Desc != "" {
				parts = append(parts, "", text.Render(meta.Desc))
			}
			if meta.ShareQuota > 0 {
				ratio := float64(meta.ShareQuotaUsed) / float64(meta.ShareQuota)
				parts = append(parts,
					"",
					muted.Render("Quota"),
					Gauge(t, width-2, ratio),
					fmt.Sprintf("%s used of %s",
						HumanBytes(uint64(meta.ShareQuotaUsed)*1024*1024),
						HumanBytes(uint64(meta.ShareQuota)*1024*1024)),
				)
			}
			flags := f.flagList(meta)
			if len(flags) > 0 {
				parts = append(parts, "", muted.Render("Flags"))
				for _, flag := range flags {
					parts = append(parts, "  "+flag)
				}
			}
		}
		parts = append(parts, "", muted.Render("Usage:   ")+text.Render(f.sizeLabel(sh.Path, true, 0)))
		if err := f.sizeErr(sh.Path); err != nil {
			parts = append(parts, muted.Render("Size err: ")+lipgloss.NewStyle().Foreground(t.Error).Render(err.Error()))
		}
		if sh.Add.Owner.User != "" {
			parts = append(parts, muted.Render("Owner:   ")+text.Render(sh.Add.Owner.User))
		}
		_ = height
		return strings.Join(parts, "\n")
	}
	if row.Kind != fileTreeEntry {
		return ""
	}
	e := row.Entry
	t := f.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	size := "(folder)"
	size = f.sizeLabel(e.Path, e.IsDir, e.Add.Size)
	parts := []string{
		t.Title().Render(" " + e.Name + " "),
		"",
		muted.Render(e.Path),
		"",
		muted.Render("Size:    ") + text.Render(size),
	}
	if err := f.sizeErr(e.Path); err != nil {
		parts = append(parts, muted.Render("Size err: ")+lipgloss.NewStyle().Foreground(t.Error).Render(err.Error()))
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

func openCachePath(e dsm.FSEntry) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s\x00%d\x00%d", e.Path, e.Add.Size, e.Add.Time.Mtime)))
	name := filepath.Base(e.Name)
	if name == "." || name == "" {
		name = "download"
	}
	return filepath.Join(os.TempDir(), "synoctl-open", fmt.Sprintf("%x-%s", sum[:8], name))
}

func validCachedFile(local string, expected int64) bool {
	st, err := os.Stat(local)
	if err != nil || st.IsDir() {
		return false
	}
	if expected >= 0 && st.Size() != expected {
		_ = os.Remove(local)
		return false
	}
	return true
}

// downloadToFile streams a FileStation download to a local file atomically,
// creating parent directories as needed. When expected is non-negative the
// completed byte count must match before the file replaces the cache target.
func downloadToFile(ctx context.Context, c *dsm.Client, remote, local string, expected int64) (int64, error) {
	dir := filepath.Dir(local)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	rc, _, err := c.FileDownload(ctx, remote)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(local)+".*.tmp")
	if err != nil {
		return 0, err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	n, err := io.Copy(tmp, rc)
	if err != nil {
		_ = tmp.Close()
		return n, err
	}
	if expected >= 0 && n != expected {
		_ = tmp.Close()
		return n, fmt.Errorf("downloaded %s: got %s, expected %s", remote, humanize.IBytes(uint64(n)), humanize.IBytes(uint64(expected)))
	}
	if err := tmp.Close(); err != nil {
		return n, err
	}
	if err := os.Rename(tmpName, local); err != nil {
		return n, err
	}
	return n, nil
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
