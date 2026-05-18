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

// DriveView shows top-line Synology Drive stats and the file/folder
// listing at the Drive root. Both endpoints are optional: when Drive
// Server isn't installed we render an empty state, otherwise we surface
// whatever DSM gives us — even if just the stats card with an empty
// list.

type driveStatsMsg struct {
	S   *dsm.DriveStats
	Err error
}
type driveFilesMsg struct {
	F   []dsm.DriveFile
	Err error
}

type DriveView struct {
	ctx Ctx

	stats    *dsm.DriveStats
	statsErr error
	files    []dsm.DriveFile
	filesErr error

	cursor int
	filter Filter
	loaded bool

	detail *dsm.DriveFile
}

func NewDrive(c Ctx) tui.View { return &DriveView{ctx: c} }

func (v *DriveView) Name() string                   { return "drive" }
func (v *DriveView) Title() string                  { return "Drive" }
func (v *DriveView) Icon() string                   { return "⌘" }
func (v *DriveView) RefreshInterval() time.Duration { return 60 * time.Second }
func (v *DriveView) Bindings() []key.Binding        { return BaseBindings() }
func (v *DriveView) IsTextEditing() bool            { return v.filter.IsActive() }

func (v *DriveView) Init() tea.Cmd { return tea.Batch(v.fetchStats(), v.fetchFiles()) }

func (v *DriveView) fetchStats() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(10*time.Second,
		func(ctx context.Context) (*dsm.DriveStats, error) { return c.DriveStats(ctx) },
		func(s *dsm.DriveStats, err error) tea.Msg { return driveStatsMsg{S: s, Err: err} },
	)
}

func (v *DriveView) fetchFiles() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(12*time.Second,
		func(ctx context.Context) ([]dsm.DriveFile, error) { return c.DriveFiles(ctx, "/") },
		func(f []dsm.DriveFile, err error) tea.Msg { return driveFilesMsg{F: f, Err: err} },
	)
}

func (v *DriveView) filtered() []dsm.DriveFile {
	if v.filter.Value() == "" {
		return v.files
	}
	out := make([]dsm.DriveFile, 0, len(v.files))
	for _, f := range v.files {
		if MatchesAll(v.filter.Value(), f.Name, f.Path, f.Owner, f.Type, f.MimeType) {
			out = append(out, f)
		}
	}
	return out
}

func (v *DriveView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	if v.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detail = nil
		}
		return v, nil
	}
	if v.filter.IsActive() {
		before := v.filter.Value()
		if v.filter.Update(msg) {
			if v.filter.Value() != before {
				v.cursor = 0
			}
			return v, nil
		}
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, tea.Batch(v.fetchStats(), v.fetchFiles())
	case driveStatsMsg:
		v.stats, v.statsErr = m.S, m.Err
		v.loaded = true
	case driveFilesMsg:
		v.files, v.filesErr = m.F, m.Err
		v.loaded = true
		v.clampCursor()
	case tea.KeyMsg:
		switch m.String() {
		case "j", "down":
			rows := v.filtered()
			if v.cursor < len(rows)-1 {
				v.cursor++
			}
		case "k", "up":
			if v.cursor > 0 {
				v.cursor--
			}
		case "g":
			v.cursor = 0
		case "G":
			v.cursor = max(len(v.filtered())-1, 0)
		case "/":
			v.filter.Open()
			v.cursor = 0
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, tea.Batch(v.fetchStats(), v.fetchFiles())
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				f := rows[v.cursor]
				v.detail = &f
			}
		}
	}
	return v, nil
}

func (v *DriveView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *DriveView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detail != nil {
		return renderDriveFileDetail(t, width, *v.detail)
	}

	// Both endpoints empty + no error → Drive isn't installed (the
	// Supports() short-circuit). Show one card and stop.
	if v.loaded && v.stats == nil && len(v.files) == 0 && v.statsErr == nil && v.filesErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⌘  Synology Drive Server",
			"Synology Drive Server is not installed, or the API isn't reachable yet.",
			"Install Synology Drive Server from Package Center to see Drive stats and root contents here."), height)
	}

	files := v.filtered()
	var parts []string
	parts = append(parts, v.renderStatsCard(width))
	parts = append(parts, "")
	parts = append(parts, sectionHeader(t, width, "Root", len(files), v.filesErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(files) == 0 {
		parts = append(parts, "  "+muted(t, "(no files)"))
	}
	for i, f := range files {
		parts = append(parts, v.renderRow(f, i == v.cursor))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · esc clear · r refresh"))
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *DriveView) renderStatsCard(width int) string {
	t := v.ctx.Theme
	if v.stats == nil {
		return t.Card(false).Width(width - 2).Render(
			t.Title().Render(" Drive ") + "\n  " + muted(t, "stats unavailable on this build"))
	}
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	pair := func(k, val string) string {
		if val == "" {
			val = "—"
		}
		return mu.Render(k+":") + " " + text.Render(val)
	}
	row1 := strings.Join([]string{
		pair("Users", fmt.Sprintf("%d (%d active)", v.stats.TotalUsers, v.stats.ActiveUsers)),
		pair("Files", humanCount(uint64(v.stats.TotalFiles))),
		pair("Folders", humanCount(uint64(v.stats.TotalFolders))),
		pair("Team folders", fmt.Sprintf("%d", v.stats.TeamFolders)),
	}, "   ")
	row2 := strings.Join([]string{
		pair("Storage used", HumanBytes(uint64(v.stats.StorageUsed))),
		pair("Quota", HumanBytes(uint64(v.stats.StorageQuota))),
		pair("Versions", HumanBytes(uint64(v.stats.VersionUsed))),
		pair("Trash", HumanBytes(uint64(v.stats.TrashUsedSize))),
	}, "   ")
	body := t.Title().Render(" Drive ") + "\n" + row1 + "\n" + row2
	return t.Card(false).Width(width - 2).Render(body)
}

func (v *DriveView) renderRow(f dsm.DriveFile, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	icon := "📄"
	if f.Type == "dir" {
		icon = "📁"
	}
	modified := "—"
	if f.Modified > 0 {
		modified = time.Unix(f.Modified, 0).Format("2006-01-02 15:04")
	}
	size := "—"
	if f.Type != "dir" {
		size = HumanBytes(uint64(f.Size))
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		icon, " ",
		padRight(text.Render(clipTo(f.Name, 32)), 32), " ",
		padRight(mu.Render(f.Owner), 14), " ",
		padLeft(mu.Render(size), 10), " ",
		padRight(mu.Render(modified), 18),
	)
}

func renderDriveFileDetail(t tui.Theme, width int, f dsm.DriveFile) string {
	if width < 60 {
		width = 60
	}
	icon := "📄"
	if f.Type == "dir" {
		icon = "📁"
	}
	stamp := func(unix int64) string {
		if unix <= 0 {
			return "—"
		}
		return time.Unix(unix, 0).Format("2006-01-02 15:04")
	}
	parts := []string{
		hero(t, width, icon, f.Name, f.Type, f.Path),
		propsCard(t, width, " Properties ", [][2]string{
			{"File ID", f.FileID},
			{"Name", f.Name},
			{"Path", f.Path},
			{"Type", f.Type},
			{"Size", HumanBytes(uint64(f.Size))},
			{"Owner", f.Owner},
			{"Created", stamp(f.Created)},
			{"Modified", stamp(f.Modified)},
			{"Accessed", stamp(f.Accessed)},
			{"Versions", fmt.Sprintf("%d", f.Versions)},
			{"MIME", f.MimeType},
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
		chip("starred", f.Starred.Bool()),
		chip("shared", f.Shared.Bool()),
		chip("team folder", f.TeamFolder.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · Drive write actions aren't wired up yet"))
	return strings.Join(parts, "\n")
}
