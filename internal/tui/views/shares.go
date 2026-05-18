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

// Shares is the dedicated view for shared folders. One row per share with
// path, flag chips (enc/recycle/hidden/ro/usb/sync/cloudsync), and a quota
// gauge when one is set. Drill-down (`⏎`) opens the structured share
// detail; `S` opens a per-share Snapshots overlay where the user can list,
// create, and delete Btrfs snapshots (the create/delete paths go through
// the OTP modal because DSM demands fresh 2FA per call on this firmware).
type Shares struct {
	ctx Ctx

	shares []dsm.Share
	err    error

	base   listBase
	detail *dsm.Share

	// Snapshots overlay state. snapshotShare is the share we're showing
	// snapshots for (nil when the overlay is closed). snapCursor is a
	// separate cursor for the snapshot list — keeping it separate from
	// base.cursor means returning to the shares list with esc doesn't
	// reshuffle the user's place.
	snapshotShare *dsm.Share
	snapshots     []dsm.Snapshot
	snapshotErr   error
	snapLoading   bool
	snapCursor    int

	// Modals — all token-routed so the same overlay can host multiple
	// in-flight actions without keeping side state.
	otp       *OTPModal
	confirm   *Confirm
	prompt    *Prompt
	pendingOp pendingSnapshotOp
	flash     string
}

// pendingSnapshotOp tracks the in-flight create/delete action across the
// modal sequence (Prompt → OTP → API call). It's a tiny state machine so
// we can route the OTPProvidedMsg back to the right action without
// stuffing dozens of distinct tokens into the otp modal.
type pendingSnapshotOp struct {
	kind        string // "create" | "delete"
	share       string
	snapshot    string // populated for delete
	description string // populated for create after the prompt closes
}

// NewShares constructs the shares view.
func NewShares(c Ctx) tui.View {
	return &Shares{
		ctx:     c,
		otp:     NewOTPModal(c.Theme),
		confirm: NewConfirm(c.Theme),
		prompt:  NewPrompt(c.Theme),
	}
}

func (s *Shares) Name() string                   { return "shares" }
func (s *Shares) Title() string                  { return "Shares" }
func (s *Shares) Icon() string                   { return "▦" }
func (s *Shares) RefreshInterval() time.Duration { return 30 * time.Second }
func (s *Shares) Bindings() []key.Binding {
	return append(BaseBindings(),
		key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "snapshots")),
		key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "create snapshot (in overlay)")),
		key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete snapshot (in overlay)")),
	)
}

// IsTextEditing tells the shell to defer global keys when any of the
// snapshot-flow modals are open — typed runes (codes, descriptions,
// y/n confirms) must reach the input, not trigger quit/actions.
func (s *Shares) IsTextEditing() bool {
	return s.otp.Open() || s.confirm.Open() || s.prompt.Open()
}

// Hint returns mode-specific keys for the global hint strip. The strip
// changes when the user enters the snapshots overlay so the keys it
// advertises actually do something there.
func (s *Shares) Hint() string {
	if s.snapshotShare != nil {
		return "↑/↓ move · c create · D delete · r refresh · esc back"
	}
	return "⏎ details · S snapshots · / filter · r refresh"
}

func (s *Shares) Init() tea.Cmd { return s.fetch() }

func (s *Shares) fetch() tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	return tui.Fetch(8*time.Second,
		func(ctx context.Context) ([]dsm.Share, error) { return c.Shares(ctx) },
		func(v []dsm.Share, err error) tea.Msg { return sharesMsg{S: v, Err: err} },
	)
}

func (s *Shares) visible() []dsm.Share {
	if s.base.FilterValue() == "" {
		return s.shares
	}
	out := make([]dsm.Share, 0, len(s.shares))
	for _, x := range s.shares {
		if MatchesAll(s.base.FilterValue(), x.Name, x.Path, x.Desc) {
			out = append(out, x)
		}
	}
	return out
}

// — snapshot fetch/action messages —

type snapshotsListedMsg struct {
	Share string
	Items []dsm.Snapshot
	Err   error
}
type snapshotActionMsg struct {
	Kind     string // "create" | "delete"
	Share    string
	Snapshot string
	Err      error
}

func (s *Shares) fetchSnapshots(share string) tea.Cmd {
	c := s.ctx.Client
	if c == nil {
		return nil
	}
	s.snapLoading = true
	return tui.Fetch(15*time.Second,
		func(ctx context.Context) ([]dsm.Snapshot, error) { return c.Snapshots(ctx, share) },
		func(items []dsm.Snapshot, err error) tea.Msg {
			return snapshotsListedMsg{Share: share, Items: items, Err: err}
		},
	)
}

func (s *Shares) createSnapshotCmd(share, desc, otp string) tea.Cmd {
	c := s.ctx.Client
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

func (s *Shares) deleteSnapshotCmd(share, name, otp string) tea.Cmd {
	c := s.ctx.Client
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

func (s *Shares) Update(msg tea.Msg) (tui.View, tea.Cmd) {
	// Modal routing comes first — none of these care about the underlying
	// view state, and all three modals can be open at any time during the
	// snapshot flow.
	if handled, cmd := s.confirm.Update(msg); handled {
		return s, cmd
	}
	if handled, cmd := s.prompt.Update(msg); handled {
		return s, cmd
	}
	if handled, cmd := s.otp.Update(msg); handled {
		return s, cmd
	}

	switch m := msg.(type) {
	// — modal results route the in-flight snapshot operation forward —
	case SubmittedMsg:
		// The only Prompt we currently use is the "create snapshot
		// description" prompt; tokenise so a future second prompt
		// doesn't get caught in this branch.
		if rest, ok := strings.CutPrefix(m.Token, "snapshot.create.desc:"); ok {
			s.pendingOp = pendingSnapshotOp{kind: "create", share: rest, description: strings.TrimSpace(m.Value)}
			s.otp.Ask("snapshot.create:"+rest, fmt.Sprintf("OTP needed to create a snapshot of %q.", rest))
			return s, nil
		}
	case ConfirmedMsg:
		if rest, ok := strings.CutPrefix(m.Token, "snapshot.delete:"); ok {
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) == 2 {
				share, name := parts[0], parts[1]
				s.pendingOp = pendingSnapshotOp{kind: "delete", share: share, snapshot: name}
				s.otp.Ask("snapshot.delete:"+share+"/"+name,
					fmt.Sprintf("OTP needed to delete snapshot %s of %q.", name, share))
				return s, nil
			}
		}
	case CancelledMsg:
		s.pendingOp = pendingSnapshotOp{}
		s.flash = "cancelled"
		return s, nil
	case OTPProvidedMsg:
		switch s.pendingOp.kind {
		case "create":
			share, desc := s.pendingOp.share, s.pendingOp.description
			s.flash = "creating snapshot of " + share + "…"
			s.pendingOp = pendingSnapshotOp{}
			return s, s.createSnapshotCmd(share, desc, m.Code)
		case "delete":
			share, name := s.pendingOp.share, s.pendingOp.snapshot
			s.flash = "deleting snapshot " + name + " of " + share + "…"
			s.pendingOp = pendingSnapshotOp{}
			return s, s.deleteSnapshotCmd(share, name, m.Code)
		}
	case OTPCancelledMsg:
		s.pendingOp = pendingSnapshotOp{}
		s.flash = "cancelled (OTP)"
		return s, nil
	case snapshotsListedMsg:
		if s.snapshotShare == nil || m.Share != s.snapshotShare.Name {
			return s, nil // late arrival, user moved on
		}
		s.snapLoading = false
		s.snapshots, s.snapshotErr = m.Items, m.Err
		if s.snapCursor >= len(s.snapshots) {
			s.snapCursor = len(s.snapshots) - 1
		}
		if s.snapCursor < 0 {
			s.snapCursor = 0
		}
		return s, nil
	case snapshotActionMsg:
		if m.Err != nil {
			// DSM rejected the OTP — keep the modal expectation alive so
			// the user can retry without having to re-prompt for the
			// description / re-confirm.
			if dsm.IsOTPStepupRequired(m.Err) {
				s.flash = "OTP rejected, try again"
				switch m.Kind {
				case "create":
					s.otp.Ask("snapshot.create:"+m.Share, fmt.Sprintf("OTP rejected. Try again for %q.", m.Share))
					return s, nil
				case "delete":
					s.otp.Ask("snapshot.delete:"+m.Share+"/"+m.Snapshot, fmt.Sprintf("OTP rejected. Try again for %s.", m.Snapshot))
					return s, nil
				}
			}
			s.flash = m.Kind + " failed: " + m.Err.Error()
		} else {
			switch m.Kind {
			case "create":
				s.flash = "snapshot of " + m.Share + " created"
			case "delete":
				s.flash = "snapshot " + m.Snapshot + " deleted"
			}
		}
		// Refresh the snapshots list so the new state is visible.
		if s.snapshotShare != nil {
			return s, s.fetchSnapshots(s.snapshotShare.Name)
		}
		return s, nil
	}

	// — snapshots overlay owns input when open —
	if s.snapshotShare != nil {
		if km, ok := msg.(tea.KeyMsg); ok {
			switch km.String() {
			case "esc", "q":
				s.snapshotShare = nil
				s.snapshots = nil
				s.snapshotErr = nil
				s.snapCursor = 0
				return s, nil
			case "j", "down":
				if s.snapCursor < len(s.snapshots)-1 {
					s.snapCursor++
				}
				return s, nil
			case "k", "up":
				if s.snapCursor > 0 {
					s.snapCursor--
				}
				return s, nil
			case "g":
				s.snapCursor = 0
				return s, nil
			case "G":
				if len(s.snapshots) > 0 {
					s.snapCursor = len(s.snapshots) - 1
				}
				return s, nil
			case "c":
				share := s.snapshotShare.Name
				s.prompt.Ask("snapshot.create.desc:"+share,
					"Snapshot description",
					"Optional note attached to the snapshot. Press ⏎ to accept (blank ok).",
					"")
				return s, nil
			case "D":
				if s.snapCursor < len(s.snapshots) {
					share := s.snapshotShare.Name
					snap := s.snapshots[s.snapCursor]
					if snap.Locked {
						s.flash = snap.Name + " is locked — DSM blocks deletion until unlocked"
						return s, nil
					}
					s.confirm.Ask("snapshot.delete:"+share+"/"+snap.Name,
						"Delete snapshot "+snap.Name+"?",
						"This permanently removes the Btrfs snapshot. There is no undo.")
				}
				return s, nil
			case "r":
				return s, s.fetchSnapshots(s.snapshotShare.Name)
			}
		}
		return s, nil
	}

	// — share detail overlay (existing behaviour) —
	if s.detail != nil {
		if km, ok := msg.(tea.KeyMsg); ok && (km.String() == "esc" || km.String() == "q") {
			s.detail = nil
		}
		return s, nil
	}

	switch m := msg.(type) {
	case tui.TickMsg:
		return s, s.fetch()
	case sharesMsg:
		s.shares, s.err = m.S, m.Err
		s.base.ClampCursor(len(s.visible()))
		return s, nil
	}

	if _, handled := s.base.HandleKey(msg, len(s.visible())); handled {
		return s, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok {
		switch km.String() {
		case "enter":
			if rows := s.visible(); s.base.Cursor() < len(rows) {
				sh := rows[s.base.Cursor()]
				s.detail = &sh
			}
		case "S":
			if rows := s.visible(); s.base.Cursor() < len(rows) {
				sh := rows[s.base.Cursor()]
				s.snapshotShare = &sh
				s.snapshots = nil
				s.snapshotErr = nil
				s.snapCursor = 0
				return s, s.fetchSnapshots(sh.Name)
			}
		}
	}
	return s, nil
}

func (s *Shares) Render(width, height int) string {
	t := s.ctx.Theme
	if s.otp.Open() {
		return s.otp.Render(width, height)
	}
	if s.confirm.Open() {
		return s.confirm.Render(width, height)
	}
	if s.prompt.Open() {
		return s.prompt.Render(width, height)
	}
	if s.snapshotShare != nil {
		return s.renderSnapshots(width, height)
	}
	if s.detail != nil {
		return renderShareDetail(t, width, height, *s.detail)
	}
	rows := s.visible()
	parts := []string{sectionHeader(t, width, "Shared folders", len(rows), s.err)}
	if s.shares == nil && s.err == nil {
		parts = append(parts, "  "+muted(t, "loading…"))
	} else if len(rows) == 0 {
		parts = append(parts, "  "+muted(t, "(none)"))
	}
	for i, sh := range rows {
		parts = append(parts, s.renderRow(width, sh, i == s.base.Cursor()))
	}
	if s.flash != "" {
		parts = append(parts, "", lipgloss.NewStyle().Foreground(t.Muted).Render("  "+s.flash))
	}
	if f := s.base.FilterFooter(t); f != "" {
		parts = append(parts, f)
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

// renderSnapshots draws the per-share snapshots overlay. It replaces the
// main body when active (the overlay isn't a centered modal because the
// list can grow large — keeping it in the main pane lets us scroll).
func (s *Shares) renderSnapshots(width, height int) string {
	t := s.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	title := t.Title().Render(" Snapshots · " + s.snapshotShare.Name + " ")
	header := sectionHeader(t, width, "Snapshots · "+s.snapshotShare.Name, len(s.snapshots), s.snapshotErr)

	var parts []string
	parts = append(parts, title)
	parts = append(parts, header)

	if s.snapLoading && len(s.snapshots) == 0 {
		parts = append(parts, "  "+muted.Render("listing…"))
	} else if len(s.snapshots) == 0 && s.snapshotErr == nil {
		parts = append(parts, "  "+muted.Render("(no snapshots taken yet — press `c` to create one)"))
	}

	for i, sn := range s.snapshots {
		parts = append(parts, s.renderSnapshotRow(sn, i == s.snapCursor))
	}

	parts = append(parts, "")
	parts = append(parts, muted.Render(
		"  ↑/↓ move · c create · D delete · r refresh · esc back"))
	if s.flash != "" {
		parts = append(parts, muted.Render("  "+s.flash))
	}
	return fitOrScroll(strings.Join(parts, "\n"), height)
}

func (s *Shares) renderSnapshotRow(sn dsm.Snapshot, highlight bool) string {
	t := s.ctx.Theme
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

func (s *Shares) Inspect(width, height int) string {
	// When the snapshots overlay is open, the inspector previews the
	// cursor'd snapshot. Otherwise it previews the cursor'd share, as
	// before.
	if s.snapshotShare != nil {
		if len(s.snapshots) == 0 || s.snapCursor >= len(s.snapshots) {
			return ""
		}
		t := s.ctx.Theme
		sn := s.snapshots[s.snapCursor]
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
			muted.Render("Share:    ") + text.Render(s.snapshotShare.Name),
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

	rows := s.visible()
	if len(rows) == 0 || s.base.Cursor() >= len(rows) {
		return ""
	}
	t := s.ctx.Theme
	sh := rows[s.base.Cursor()]
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	parts := []string{
		t.Title().Render(" " + sh.Name + " "),
		"",
		muted.Render(sh.Path),
	}
	if sh.Desc != "" {
		parts = append(parts, "", text.Render(sh.Desc))
	}
	parts = append(parts, "")
	if sh.ShareQuota > 0 {
		ratio := float64(sh.ShareQuotaUsed) / float64(sh.ShareQuota)
		parts = append(parts,
			muted.Render("Quota"),
			Gauge(t, width-2, ratio),
			fmt.Sprintf("%s used of %s",
				HumanBytes(uint64(sh.ShareQuotaUsed)*1024*1024),
				HumanBytes(uint64(sh.ShareQuota)*1024*1024)),
			"",
		)
	}
	flags := s.flagList(sh)
	if len(flags) > 0 {
		parts = append(parts, muted.Render("Flags"))
		for _, f := range flags {
			parts = append(parts, "  "+f)
		}
	}
	_ = height
	return strings.Join(parts, "\n")
}

func (s *Shares) flagList(sh dsm.Share) []string {
	t := s.ctx.Theme
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

func (s *Shares) renderRow(width int, sh dsm.Share, highlight bool) string {
	t := s.ctx.Theme
	muted := lipgloss.NewStyle().Foreground(t.Muted)
	text := lipgloss.NewStyle().Foreground(t.Text).Bold(true)
	quota := "—"
	if sh.ShareQuota > 0 {
		ratio := float64(sh.ShareQuotaUsed) / float64(sh.ShareQuota)
		quota = fmt.Sprintf("%5.1f%%  %s / %s", ratio*100,
			HumanBytes(uint64(sh.ShareQuotaUsed)*1024*1024),
			HumanBytes(uint64(sh.ShareQuota)*1024*1024))
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
	_ = width
	return lipgloss.JoinHorizontal(lipgloss.Center,
		caretGlyph(t, highlight), " ",
		padRight(text.Render(sh.Name), 18), " ",
		padRight(muted.Render(sh.Path), 28), " ",
		padRight(muted.Render(flagStr), 24), " ",
		padLeft(muted.Render(quota), 30),
	)
}
