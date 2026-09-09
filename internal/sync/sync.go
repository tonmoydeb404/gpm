// Package sync persists the gpm config and regenerates every managed
// artifact derived from it: the managed block in ~/.ssh/config, the
// per-profile gitconfig files, and the includeIf block in the global
// gitconfig. Both the CLI and the TUI call SyncAll after mutating
// profiles or directory mappings so the two frontends cannot drift.
package sync

import (
	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/gitconfig"
	"github.com/tonmoydeb404/gpm/internal/sshconfig"
)

// All saves the config and regenerates every managed artifact.
func All(cfg *config.Config) error {
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := sshconfig.SyncFromConfig(cfg); err != nil {
		return err
	}
	return gitconfig.SyncFromConfig(cfg)
}
