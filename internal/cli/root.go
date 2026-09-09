// Package cli implements the gpm command line interface. All command
// logic delegates to the internal packages so the TUI can reuse it.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/tui"
)

// version is overridden at build time via -ldflags.
var version = "0.1.0"

// NewRootCmd assembles the gpm command tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "gpm",
		Short:         "Git Profile Manager",
		Long:          "GPM manages multiple Git identities and SSH keys on a single machine.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Bare `gpm` opens the interactive TUI. Without a TTY
			// (scripts, CI) fall back to printing help instead of
			// hanging on a raw-mode program.
			if !interactive() {
				return cmd.Help()
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			return tui.Run(cfg)
		},
	}
	root.AddCommand(
		newProfileCmd(),
		newSSHCmd(),
		newKeyCmd(),
		newDirCmd(),
		newApplyCmd(),
		newCurrentCmd(),
		newSyncCmd(),
		newDoctorCmd(),
		newScanCmd(),
		newImportCmd(),
		newGuardCmd(),
		newPrecommitCmd(),
	)
	return root
}

// Execute runs the root command and maps errors to exit codes.
func Execute() {
	if dev := os.Getenv("GPM_HOME"); dev != "" {
		fmt.Fprintf(os.Stderr, "gpm: sandbox mode — all state under %s (GPM_HOME)\n", dev)
	}
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "gpm:", err)
		os.Exit(1)
	}
}

// loadConfig loads gpm's config or fails with a friendly message.
func loadConfig() (*config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// interactive reports whether we can prompt the user.
func interactive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}
