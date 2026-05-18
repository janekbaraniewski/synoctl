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

// CloudSyncView lists Cloud Sync connections. Refresh is 60s — sync
// state moves on a human cadence (Cloud Sync polls its providers
// every few minutes), so a tight loop here would just spam DSM for
// no UI benefit.

type cloudSyncMsg struct {
	T   []dsm.CloudSyncTask
	Err error
}

// CloudSyncView is the table-style list view for Cloud Sync tasks,
// matching the shape of HyperBackupView so behaviour (filter, detail
// overlay, refresh keys) is identical across the two backup-adjacent
// surfaces.
type CloudSyncView struct {
	ctx Ctx

	tasks    []dsm.CloudSyncTask
	tasksErr error

	cursor int
	filter Filter
	loaded bool

	detail *dsm.CloudSyncTask
}

// NewCloudSync constructs the Cloud Sync view. It is wired into the
// sidebar from internal/cli/tui.go.
func NewCloudSync(c Ctx) tui.View { return &CloudSyncView{ctx: c} }

func (v *CloudSyncView) Name() string                   { return "cloudsync" }
func (v *CloudSyncView) Title() string                  { return "Cloud Sync" }
func (v *CloudSyncView) Icon() string                   { return "☁" }
func (v *CloudSyncView) RefreshInterval() time.Duration { return 60 * time.Second }
func (v *CloudSyncView) Bindings() []key.Binding        { return BaseBindings() }
func (v *CloudSyncView) IsTextEditing() bool            { return v.filter.IsActive() }

// Hint feeds the global hint strip at the bottom of the screen. The
// keys advertised here mirror what Update() actually handles.
func (v *CloudSyncView) Hint() string {
	return "⏎ details · / filter · r refresh"
}

func (v *CloudSyncView) Init() tea.Cmd { return v.fetch() }

func (v *CloudSyncView) fetch() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.CloudSyncTask, error) { return c.CloudSyncTasks(ctx) },
		func(x []dsm.CloudSyncTask, err error) tea.Msg { return cloudSyncMsg{T: x, Err: err} },
	)
}

func (v *CloudSyncView) filtered() []dsm.CloudSyncTask {
	if v.filter.Value() == "" {
		return v.tasks
	}
	out := make([]dsm.CloudSyncTask, 0, len(v.tasks))
	for _, t := range v.tasks {
		provider := cloudSyncProvider(t)
		if MatchesAll(v.filter.Value(),
			t.Label(), provider, t.LinkStatus, t.CurrentStatus,
			t.LocalPath, t.LinkRemote, t.Username, t.AccountID) {
			out = append(out, t)
		}
	}
	return out
}

func (v *CloudSyncView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
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
		return v, v.fetch()
	case cloudSyncMsg:
		v.tasks, v.tasksErr = m.T, m.Err
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
			return v, v.fetch()
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				t := rows[v.cursor]
				v.detail = &t
			}
		}
	}
	return v, nil
}

func (v *CloudSyncView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *CloudSyncView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.detail != nil {
		return renderCloudSyncDetail(t, width, *v.detail)
	}

	if v.loaded && len(v.tasks) == 0 && v.tasksErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"☁  Cloud Sync",
			"Cloud Sync is not installed, or no cloud connections have been configured.",
			"Install Cloud Sync and add a connection (Dropbox / Google Drive / S3 / …) to see sync state here."), height)
	}

	tasks := v.filtered()
	var parts []string
	parts = append(parts, sectionHeader(t, width, "Cloud Sync connections", len(tasks), v.tasksErr))
	if !v.loaded {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(tasks) == 0 {
		parts = append(parts, "  "+muted(t, "(none matching)"))
	}
	for i, tk := range tasks {
		parts = append(parts, v.renderRow(tk, i == v.cursor))
	}
	parts = append(parts, "")
	parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render(
		"  ↑/↓ move · ⏎ details · / filter · esc clear · r refresh"))
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *CloudSyncView) renderRow(tk dsm.CloudSyncTask, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	lastSync := "—"
	if tk.LastSyncTime > 0 {
		lastSync = time.Unix(tk.LastSyncTime, 0).Format("2006-01-02 15:04")
	}
	provider := cloudSyncProvider(tk)
	direction := dsm.CloudSyncDirectionLabel(tk.Direction, tk.LegacyDirection)
	status := cloudSyncStatusKey(tk)
	statusLabel := tk.CurrentStatus
	if statusLabel == "" {
		statusLabel = tk.LinkStatus
	}
	if statusLabel == "" {
		statusLabel = status
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(clipTo(tk.Label(), 24)), 24), " ",
		padRight(mu.Render(clipTo(provider, 18)), 18), " ",
		padRight(mu.Render(direction), 16), " ",
		padRight(mu.Render(lastSync), 18), " ",
		padLeft(mu.Render(HumanBytes(uint64(tk.TotalSize))), 10), " ",
		t.HealthStyle(status).Render(statusLabel),
	)
}

// Inspect implements tui.Inspector — Cloud Sync rows carry enough
// connection metadata (provider, remote path, account, last sync) to
// benefit from the side preview pane while the user moves the cursor.
func (v *CloudSyncView) Inspect(width, height int) string {
	_ = height
	t := v.ctx.Theme
	tasks := v.filtered()
	if v.cursor < 0 || v.cursor >= len(tasks) {
		return muted(t, "  (no selection)")
	}
	return renderCloudSyncInspect(t, width, tasks[v.cursor])
}

func renderCloudSyncDetail(t tui.Theme, width int, tk dsm.CloudSyncTask) string {
	if width < 60 {
		width = 60
	}
	provider := cloudSyncProvider(tk)
	direction := dsm.CloudSyncDirectionLabel(tk.Direction, tk.LegacyDirection)
	lastSync := "—"
	if tk.LastSyncTime > 0 {
		lastSync = time.Unix(tk.LastSyncTime, 0).Format("2006-01-02 15:04")
	}
	status := tk.CurrentStatus
	if status == "" {
		status = tk.LinkStatus
	}
	if status == "" {
		status = "unknown"
	}
	parts := []string{
		hero(t, width, "☁", tk.Label(), status, provider),
		propsCard(t, width, " Properties ", [][2]string{
			{"ID", fmt.Sprintf("%d", tk.ID)},
			{"Name", tk.Label()},
			{"Provider", provider},
			{"Direction", direction},
			{"Link status", tk.LinkStatus},
			{"Current status", tk.CurrentStatus},
			{"Local path", tk.LocalPath},
			{"Remote path", tk.LinkRemote},
			{"Account", tk.Username},
			{"Account ID", tk.AccountID},
			{"Last sync", lastSync},
			{"Bytes synced", HumanBytes(uint64(tk.TotalSize))},
			{"Errors", fmt.Sprintf("%d", tk.ErrorCount)},
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
		chip("enabled", tk.Enabled.Bool()),
		chip("errors", tk.ErrorCount > 0),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · Cloud Sync write actions aren't wired up yet"))
	return strings.Join(parts, "\n")
}

func renderCloudSyncInspect(t tui.Theme, width int, tk dsm.CloudSyncTask) string {
	provider := cloudSyncProvider(tk)
	direction := dsm.CloudSyncDirectionLabel(tk.Direction, tk.LegacyDirection)
	lastSync := "—"
	if tk.LastSyncTime > 0 {
		lastSync = time.Unix(tk.LastSyncTime, 0).Format("2006-01-02 15:04")
	}
	status := tk.CurrentStatus
	if status == "" {
		status = tk.LinkStatus
	}
	if status == "" {
		status = "unknown"
	}
	statusKey := cloudSyncStatusKey(tk)
	remote := tk.LinkRemote
	if remote == "" {
		remote = "—"
	}
	local := tk.LocalPath
	if local == "" {
		local = "—"
	}
	account := tk.Username
	if account == "" {
		account = "—"
	}
	return strings.Join([]string{
		t.Title().Render(" Cloud Sync connection "),
		lipgloss.NewStyle().Foreground(t.Accent).Bold(true).Render("  " + clipTo(tk.Label(), width-4)),
		lipgloss.NewStyle().Foreground(t.Muted).Render("  " + provider),
		"  " + t.HealthStyle(statusKey).Render(status),
		"",
		muted(t, "  Direction   ") + direction,
		muted(t, "  Local       ") + clipTo(local, width-16),
		muted(t, "  Remote      ") + clipTo(remote, width-16),
		muted(t, "  Account     ") + clipTo(account, width-16),
		muted(t, "  Last sync   ") + lastSync,
		muted(t, "  Bytes       ") + HumanBytes(uint64(tk.TotalSize)),
		muted(t, "  Errors      ") + fmt.Sprintf("%d", tk.ErrorCount),
	}, "\n")
}

// cloudSyncProvider resolves the provider label. Modern Cloud Sync
// builds use the numeric link_type, legacy ones ship a string under
// link_type_s; we prefer the latter when present because it already
// matches Cloud Sync's own UI strings.
func cloudSyncProvider(tk dsm.CloudSyncTask) string {
	if tk.LinkTypeS != "" {
		return tk.LinkTypeS
	}
	return dsm.CloudSyncProviderName(tk.LinkType)
}

// cloudSyncStatusKey turns Cloud Sync's free-form status strings into
// the keys our Theme.HealthStyle() knows about (enabled/warning/error/
// stopped/disabled), so rows colour-code consistently with the rest of
// the TUI.
func cloudSyncStatusKey(tk dsm.CloudSyncTask) string {
	s := strings.ToLower(strings.TrimSpace(tk.CurrentStatus))
	if s == "" {
		s = strings.ToLower(strings.TrimSpace(tk.LinkStatus))
	}
	switch {
	case strings.Contains(s, "error"), strings.Contains(s, "fail"):
		return "error"
	case strings.Contains(s, "disconn"), strings.Contains(s, "offline"):
		return "stopped"
	case strings.Contains(s, "paus"):
		return "disabled"
	case strings.Contains(s, "sync"), strings.Contains(s, "running"), strings.Contains(s, "uploading"), strings.Contains(s, "downloading"):
		return "warning"
	case strings.Contains(s, "up to date"), strings.Contains(s, "connected"), strings.Contains(s, "idle"), strings.Contains(s, "complete"):
		return "enabled"
	}
	if tk.ErrorCount > 0 {
		return "warning"
	}
	if tk.Enabled.Bool() {
		return "enabled"
	}
	return "disabled"
}
