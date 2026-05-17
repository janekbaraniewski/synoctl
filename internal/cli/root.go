// Package cli wires the Cobra command tree for synoctl. The root command
// launches the TUI; subcommands handle one-shot tasks (discover, login,
// logout, version) that don't need the full-screen interface.
package cli

import (
	"context"
	"os"

	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// NewRoot constructs the root command. Calling .Execute() blocks until the
// command finishes.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "synoctl",
		Short: "A polished TUI for managing Synology NAS devices",
		Long: `synoctl is a TUI-first management tool for Synology DSM.

It auto-discovers devices on the local network via mDNS, stores credentials
in the macOS Keychain, and renders a live dashboard with drill-down views
for volumes, disks, shares, users, packages, services, and more.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          runTUI,
	}
	root.AddCommand(
		newDiscoverCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newAPIsCmd(),
		newRawCmd(),
		newVersionCmd(),
	)
	return root
}

// Execute is the binary entry point.
func Execute() {
	logger := log.NewWithOptions(os.Stderr, log.Options{
		ReportTimestamp: false,
		Prefix:          "synoctl",
	})
	cmd := NewRoot()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := cmd.ExecuteContext(ctx); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}

// runTUI is the default action when the user runs `synoctl` with no
// subcommand. It loads config, authenticates, and starts the bubbletea
// application.
func runTUI(cmd *cobra.Command, args []string) error {
	// The full TUI wiring lives in cli/tui.go so this file stays focused
	// on the command tree.
	return startTUI(cmd.Context())
}
