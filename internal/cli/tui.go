package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/log"

	"github.com/janbaraniewski/synology-ctl/internal/config"
	"github.com/janbaraniewski/synology-ctl/internal/dsm"
	"github.com/janbaraniewski/synology-ctl/internal/tui"
	"github.com/janbaraniewski/synology-ctl/internal/tui/views"
)

// startTUI is the entry point invoked by `synoctl` (no subcommand). On
// any state-related miss — no profile, no Keychain password, expired
// device token — it transparently runs Onboard() instead of telling
// the user to invoke another subcommand.
func startTUI(parentCtx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	profile, password, err := ensureProfileAndPassword(parentCtx, cfg)
	if err != nil {
		return err
	}

	client, err := dsm.New(dsm.Options{
		Scheme:   profile.Scheme,
		Host:     profile.Host,
		Port:     profile.Port,
		Insecure: profile.Insecure,
		// 3 minutes is generous, but it has to cover the very slowest
		// call we make: SYNO.Core.Package.Server.list, which walks
		// Synology's upstream package index over the public internet.
		// On a DS220j on a residential line that has been observed to
		// take 60–90s. The 45s cap we used before caused the Available
		// tab to error out before the call ever completed. Per-view
		// fetches still apply their own (shorter) context deadlines on
		// top of this — this is just the HTTP-client ceiling.
		Timeout: 3 * time.Minute,
	})
	if err != nil {
		return err
	}

	authCtx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()
	if _, err := client.Login(authCtx, dsm.LoginRequest{
		Account:    profile.Username,
		Password:   password,
		DeviceID:   profile.DeviceID,
		DeviceName: "synoctl-" + hostnameOr("local"),
	}); err != nil {
		// OTP required, password rotated, device token revoked — all of
		// these mean the stored creds are stale. Re-onboard rather than
		// kicking the user out.
		if needsReonboard(err) {
			profile, password, err = onboardFresh(parentCtx, cfg)
			if err != nil {
				return err
			}
			client, err = dsm.New(dsm.Options{
				Scheme: profile.Scheme, Host: profile.Host, Port: profile.Port,
				Insecure: profile.Insecure, Timeout: 20 * time.Second,
			})
			if err != nil {
				return err
			}
			if _, err := client.Login(authCtx, dsm.LoginRequest{
				Account: profile.Username, Password: password,
				DeviceID: profile.DeviceID, DeviceName: "synoctl-" + hostnameOr("local"),
			}); err != nil {
				return fmt.Errorf("login after re-onboard: %w", err)
			}
		} else {
			return fmt.Errorf("login: %w", err)
		}
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Logout(ctx)
	}()

	theme := tui.DefaultTheme()
	logger := log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false, Prefix: "tui"})
	vctx := tui.ViewContext{Client: client, Theme: theme, Keys: tui.DefaultKeys(), Logger: logger}

	sections := []tui.NavSection{
		{Name: "Overview", Views: []tui.View{
			views.NewDashboard(vctx),
			views.NewResourceMonitor(vctx),
		}},
		{Name: "Storage", Views: []tui.View{
			views.NewVolumes(vctx),
			views.NewDisks(vctx),
			views.NewShares(vctx),
			views.NewFiles(vctx),
			views.NewUsage(vctx),
		}},
		{Name: "Apps", Views: []tui.View{
			views.NewApps(vctx),
			views.NewContainers(vctx),
		}},
		{Name: "Backup", Views: []tui.View{
			views.NewHyperBackup(vctx),
			views.NewActiveBackup(vctx),
			views.NewCloudSync(vctx),
		}},
		{Name: "Services", Views: []tui.View{
			views.NewDrive(vctx),
			views.NewSurveillance(vctx),
		}},
		{Name: "Security", Views: []tui.View{
			views.NewCerts(vctx),
			views.NewSecurityAdvisor(vctx),
			views.NewFirewall(vctx),
		}},
		{Name: "System", Views: []tui.View{
			views.NewAdminPage(vctx),
			views.NewSchedTasks(vctx),
			views.NewDDNS(vctx),
		}},
		{Name: "Tools", Views: []tui.View{
			views.NewExplorer(vctx),
		}},
	}
	app := tui.NewApp(client, theme, logger, sections)

	prog := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = prog.Run()
	return err
}

// ensureProfileAndPassword returns a usable profile + password, running
// Onboard() if either is missing. This is the "auto-login on first run
// (and on partial state)" promise.
func ensureProfileAndPassword(ctx context.Context, cfg *config.Config) (*config.Profile, string, error) {
	profile, ok := cfg.Active()
	if !ok {
		return onboardFresh(ctx, cfg)
	}
	password, err := config.LoadPassword(profile.Host, profile.Username)
	if err != nil {
		return nil, "", fmt.Errorf("read keychain: %w", err)
	}
	if password == "" {
		return onboardFresh(ctx, cfg)
	}
	return profile, password, nil
}

func onboardFresh(ctx context.Context, cfg *config.Config) (*config.Profile, string, error) {
	p, err := Onboard(ctx, cfg)
	if err != nil {
		return nil, "", err
	}
	pw, err := config.LoadPassword(p.Host, p.Username)
	if err != nil {
		return nil, "", fmt.Errorf("read keychain after onboard: %w", err)
	}
	if pw == "" {
		return nil, "", fmt.Errorf("onboarding finished but no password landed in keychain")
	}
	return p, pw, nil
}

// needsReonboard reports whether a login error reflects stale stored
// state (rather than e.g. a network blip).
func needsReonboard(err error) bool {
	if err == nil {
		return false
	}
	if dsm.IsOTPRequired(err) {
		return true
	}
	if dsm.IsAuthFailure(err) {
		return true
	}
	return false
}
