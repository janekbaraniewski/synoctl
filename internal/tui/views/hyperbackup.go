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

// HyperBackupView lists Hyper Backup tasks. Refresh is 60s — backup
// state moves on a human cadence, so spamming the API would be silly.
//
// Beyond listing, the view exposes three write actions routed through a
// confirm modal: `X` runs the task immediately, `p` suspends an
// in-flight task, and `R` resumes a previously suspended task. All
// three are token-gated through the same Confirm so a single yes/no
// pair handles every mutation.

type backupTasksMsg struct {
	T   []dsm.BackupTask
	Err error
}

// backupActionMsg carries the outcome of a Hyper Backup write call back
// to Update(). Kind is "run" / "suspend" / "resume" and matches the
// confirm-modal token prefix so the flash text can describe what just
// happened (or failed).
type backupActionMsg struct {
	Kind   string
	TaskID int
	Name   string
	Err    error
}

type HyperBackupView struct {
	ctx Ctx

	tasks    []dsm.BackupTask
	tasksErr error

	cursor int
	filter Filter
	loaded bool

	detail *dsm.BackupTask

	confirm *Confirm
	flash   string
}

func NewHyperBackup(c Ctx) tui.View {
	return &HyperBackupView{ctx: c, confirm: NewConfirm(c.Theme)}
}

func (v *HyperBackupView) Name() string                   { return "backup" }
func (v *HyperBackupView) Title() string                  { return "Hyper Backup" }
func (v *HyperBackupView) Icon() string                   { return "⏏" }
func (v *HyperBackupView) RefreshInterval() time.Duration { return 60 * time.Second }
func (v *HyperBackupView) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("X"), key.WithHelp("X", "run now")),
		key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "suspend")),
		key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "resume")),
	)
}

// Hint feeds the global hint strip at the bottom of the screen. The
// keys advertised here mirror what Update() actually handles.
func (v *HyperBackupView) Hint() string {
	return "⏎ details · X run · p suspend · R resume · / filter · r refresh"
}

// IsTextEditing defers global keys (q quit, /, etc.) to the confirm
// modal while it's owning input. Without this, "y" or "n" would also
// trigger the global filter or quit handlers.
func (v *HyperBackupView) IsTextEditing() bool { return v.confirm.Open() }

func (v *HyperBackupView) Init() tea.Cmd { return v.fetchTasks() }

func (v *HyperBackupView) fetchTasks() tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.BackupTask, error) { return c.BackupTasks(ctx) },
		func(x []dsm.BackupTask, err error) tea.Msg { return backupTasksMsg{T: x, Err: err} },
	)
}

// runCmd / suspendCmd / resumeCmd wrap the three DSM mutations in a
// uniform tea.Cmd shape so the Update loop can dispatch them by
// confirm-token without each branch duplicating the Fetch boilerplate.

func (v *HyperBackupView) runCmd(taskID int, name string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.RunBackupTask(ctx, taskID)
		},
		func(_ struct{}, err error) tea.Msg {
			return backupActionMsg{Kind: "run", TaskID: taskID, Name: name, Err: err}
		},
	)
}

func (v *HyperBackupView) suspendCmd(taskID int, name string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.SuspendBackupTask(ctx, taskID)
		},
		func(_ struct{}, err error) tea.Msg {
			return backupActionMsg{Kind: "suspend", TaskID: taskID, Name: name, Err: err}
		},
	)
}

func (v *HyperBackupView) resumeCmd(taskID int, name string) tea.Cmd {
	c := v.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(30*time.Second,
		func(ctx context.Context) (struct{}, error) {
			return struct{}{}, c.ResumeBackupTask(ctx, taskID)
		},
		func(_ struct{}, err error) tea.Msg {
			return backupActionMsg{Kind: "resume", TaskID: taskID, Name: name, Err: err}
		},
	)
}

func (v *HyperBackupView) filtered() []dsm.BackupTask {
	if v.filter.Value() == "" {
		return v.tasks
	}
	out := make([]dsm.BackupTask, 0, len(v.tasks))
	for _, t := range v.tasks {
		if MatchesAll(v.filter.Value(), t.Name, t.Type, t.RepoTarget, t.RepoHost, t.RepoPath, t.Status, t.LastStatus) {
			out = append(out, t)
		}
	}
	return out
}

func (v *HyperBackupView) currentTask() (dsm.BackupTask, bool) {
	rows := v.filtered()
	if v.cursor < 0 || v.cursor >= len(rows) {
		return dsm.BackupTask{}, false
	}
	return rows[v.cursor], true
}

func (v *HyperBackupView) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Confirm modal claims input first — same pattern as Apps / Shares.
	if handled, cmd := v.confirm.Update(msg); handled {
		return v, cmd
	}
	switch m := msg.(type) {
	case ConfirmedMsg:
		// Token shape: "<kind>:<taskID>:<name>". Name is best-effort —
		// the API only needs the ID, but we capture the name at the
		// confirm moment so flash messages survive a list refresh.
		if kind, rest, ok := splitToken(m.Token, ':'); ok {
			id, name := splitTaskToken(rest)
			switch kind {
			case "run":
				v.flash = fmt.Sprintf("running %s…", name)
				return v, v.runCmd(id, name)
			case "suspend":
				v.flash = fmt.Sprintf("suspending %s…", name)
				return v, v.suspendCmd(id, name)
			case "resume":
				v.flash = fmt.Sprintf("resuming %s…", name)
				return v, v.resumeCmd(id, name)
			}
		}
	case CancelledMsg:
		v.flash = "cancelled"
		return v, nil
	case backupActionMsg:
		if m.Err != nil {
			v.flash = fmt.Sprintf("%s failed: %s", m.Kind, m.Err.Error())
		} else {
			switch m.Kind {
			case "run":
				v.flash = fmt.Sprintf("%s started", m.Name)
			case "suspend":
				v.flash = fmt.Sprintf("%s suspended", m.Name)
			case "resume":
				v.flash = fmt.Sprintf("%s resumed", m.Name)
			}
		}
		// Pull the list so the user sees the new state without having
		// to mash `r`.
		return v, v.fetchTasks()
	}

	if v.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			v.detail = nil
		}
		return v, nil
	}
	if v.filter.IsActive() {
		if v.filter.Update(msg) {
			return v, nil
		}
	}
	switch m := msg.(type) {
	case tui.TickMsg:
		return v, v.fetchTasks()
	case backupTasksMsg:
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
		case "esc":
			if v.filter.Value() != "" {
				v.filter.Clear()
				v.cursor = 0
			}
		case "r":
			return v, v.fetchTasks()
		case "enter":
			rows := v.filtered()
			if v.cursor >= 0 && v.cursor < len(rows) {
				t := rows[v.cursor]
				v.detail = &t
			}
		case "X":
			if tk, ok := v.currentTask(); ok {
				v.confirm.Ask(
					fmt.Sprintf("run:%d:%s", tk.TaskID, tk.Name),
					fmt.Sprintf("Run backup task %s now?", tk.Name),
					"It may use significant bandwidth and load the repository for the duration of the backup.")
			}
		case "p":
			if tk, ok := v.currentTask(); ok {
				v.confirm.Ask(
					fmt.Sprintf("suspend:%d:%s", tk.TaskID, tk.Name),
					fmt.Sprintf("Suspend backup task %s?", tk.Name),
					"This pauses the task. It stays paused until you resume it.")
			}
		case "R":
			if tk, ok := v.currentTask(); ok {
				v.confirm.Ask(
					fmt.Sprintf("resume:%d:%s", tk.TaskID, tk.Name),
					fmt.Sprintf("Resume backup task %s?", tk.Name),
					"The task picks up from where it was suspended.")
			}
		}
	}
	return v, nil
}

func (v *HyperBackupView) clampCursor() {
	n := len(v.filtered())
	if v.cursor >= n {
		v.cursor = n - 1
	}
	if v.cursor < 0 {
		v.cursor = 0
	}
}

func (v *HyperBackupView) Render(width, height int) string {
	t := v.ctx.Theme
	if v.confirm.Open() {
		return v.confirm.Render(width, height)
	}
	if v.detail != nil {
		return renderBackupTaskDetail(t, width, *v.detail)
	}

	if v.loaded && len(v.tasks) == 0 && v.tasksErr == nil {
		return fitOrScroll(emptyStateCard(t, width,
			"⏏  Hyper Backup",
			"Hyper Backup is not installed, or no backup tasks have been configured.",
			"Install Hyper Backup and add a task to see backup state, repo targets, and last-run results here."), height)
	}

	tasks := v.filtered()
	var parts []string
	parts = append(parts, sectionHeader(t, width, "Backup tasks", len(tasks), v.tasksErr))
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
		"  ↑/↓ move · ⏎ details · X run · p suspend · R resume · / filter · esc clear · r refresh"))
	if v.flash != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Muted).Render("  "+v.flash))
	}
	if fr := v.filter.Render(t); fr != "" {
		parts = append(parts, fr)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (v *HyperBackupView) renderRow(tk dsm.BackupTask, highlight bool) string {
	t := v.ctx.Theme
	mu := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	lastRun := "—"
	if tk.LastRun > 0 {
		lastRun = time.Unix(tk.LastRun, 0).Format("2006-01-02 15:04")
	}
	repo := tk.RepoTarget
	if tk.RepoHost != "" {
		repo += " · " + tk.RepoHost
	}
	status := tk.LastStatus
	if status == "" {
		status = tk.Status
	}
	if status == "" {
		status = tk.State
	}
	if status == "" {
		status = "—"
	}
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(tk.Name), 22), " ",
		padRight(mu.Render(clipTo(repo, 24)), 24), " ",
		padRight(mu.Render(lastRun), 18), " ",
		padLeft(mu.Render(HumanBytes(uint64(tk.TotalSize))), 10), " ",
		t.HealthStyle(status).Render(status),
	)
}

func renderBackupTaskDetail(t tui.Theme, width int, tk dsm.BackupTask) string {
	if width < 60 {
		width = 60
	}
	status := tk.LastStatus
	if status == "" {
		status = tk.Status
	}
	if status == "" {
		status = "unknown"
	}
	lastRun, nextRun := "—", "—"
	if tk.LastRun > 0 {
		lastRun = time.Unix(tk.LastRun, 0).Format("2006-01-02 15:04")
	}
	if tk.NextRun > 0 {
		nextRun = time.Unix(tk.NextRun, 0).Format("2006-01-02 15:04")
	}
	duration := "—"
	if tk.LastDuration > 0 {
		duration = (time.Duration(tk.LastDuration) * time.Second).String()
	}
	parts := []string{
		hero(t, width, "⏏", tk.Name, status, tk.Type),
		propsCard(t, width, " Properties ", [][2]string{
			{"Task ID", fmt.Sprintf("%d", tk.TaskID)},
			{"Name", tk.Name},
			{"Type", tk.Type},
			{"Repository", tk.RepoTarget},
			{"Repo host", tk.RepoHost},
			{"Repo path", tk.RepoPath},
			{"Schedule", tk.Schedule},
			{"Last run", lastRun},
			{"Last status", tk.LastStatus},
			{"Last duration", duration},
			{"Next run", nextRun},
			{"Total size", HumanBytes(uint64(tk.TotalSize))},
			{"Used size", HumanBytes(uint64(tk.UsedSize))},
			{"Versions", fmt.Sprintf("%d", tk.Versions)},
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
		chip("enabled", tk.Enable.Bool()),
		chip("encrypted", tk.Encrypted.Bool()),
	}))
	parts = append(parts, noteCard(t, width, "  esc to go back · X run · p suspend · R resume"))
	return strings.Join(parts, "\n")
}

// splitToken returns the prefix before sep, the rest, and ok if sep is
// present. Used to peel "<kind>:<rest>" tokens routed through the
// confirm modal.
func splitToken(s string, sep byte) (string, string, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return s[:i], s[i+1:], true
		}
	}
	return "", "", false
}

// splitTaskToken parses "<taskID>:<name>" suffixes. Returns 0/"" on
// malformed input; callers should treat that as "do nothing".
func splitTaskToken(s string) (int, string) {
	id := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			fmt.Sscanf(s[:i], "%d", &id)
			return id, s[i+1:]
		}
	}
	fmt.Sscanf(s, "%d", &id)
	return id, ""
}
