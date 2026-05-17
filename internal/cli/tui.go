package cli

import (
	"context"
	"errors"
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

// startTUI is the entry point invoked by `synoctl` (no subcommand). It
// loads the active profile, authenticates against DSM, registers every
// view, and runs the bubbletea program in alt-screen mode.
func startTUI(parentCtx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	profile, ok := cfg.Active()
	if !ok {
		// First run — drop straight into the onboarding flow. Returning
		// an error here would force the user to re-invoke `synoctl login`
		// manually, which is exactly the friction the new UX removes.
		p, err := Onboard(parentCtx, cfg)
		if err != nil {
			return err
		}
		profile = p
	}

	client, err := dsm.New(dsm.Options{
		Scheme:   profile.Scheme,
		Host:     profile.Host,
		Port:     profile.Port,
		Insecure: profile.Insecure,
		Timeout:  20 * time.Second,
	})
	if err != nil {
		return err
	}

	password, err := config.LoadPassword(profile.Host, profile.Username)
	if err != nil {
		return fmt.Errorf("read keychain: %w", err)
	}
	if password == "" {
		return errors.New("no password stored for this profile; run `synoctl login`")
	}

	authCtx, cancel := context.WithTimeout(parentCtx, 30*time.Second)
	defer cancel()
	if _, err := client.Login(authCtx, dsm.LoginRequest{
		Account:    profile.Username,
		Password:   password,
		DeviceID:   profile.DeviceID,
		DeviceName: "synoctl-" + hostnameOr("local"),
	}); err != nil {
		if dsm.IsOTPRequired(err) {
			return errors.New("OTP required; rerun `synoctl login` to refresh the device token")
		}
		return fmt.Errorf("login: %w", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Logout(ctx)
	}()

	// Build view context + registry.
	theme := tui.DefaultTheme()
	logger := log.NewWithOptions(os.Stderr, log.Options{ReportTimestamp: false, Prefix: "tui"})
	vctx := tui.ViewContext{Client: client, Theme: theme, Keys: tui.DefaultKeys(), Logger: logger}

	app := tui.NewApp(client, theme, logger,
		views.NewDashboard(vctx),
		views.NewVolumes(vctx),
		views.NewDisks(vctx),
		views.NewShares(vctx),
		views.NewFiles(vctx),
		views.NewUsers(vctx),
		views.NewPackages(vctx),
		views.NewServices(vctx),
		views.NewNetwork(vctx),
		views.NewSystem(vctx),
		views.NewLogs(vctx),
	)

	prog := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err = prog.Run()
	return err
}
