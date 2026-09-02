// Package resolve determines which Git identity governs a directory.
// The same rules back `gpm current`, `gpm apply`, `gpm precommit`,
// and `gpm doctor` so every surface agrees.
package resolve

import (
	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitcmd"
	"github.com/tonmoydeb/gpm/internal/gitconfig"
)

// Profile returns the git profile username governing dir (""
// when none) and how it was determined:
//  1. deepest directory mapping
//  2. includeIf gitconfig attribution (which gpm file supplied the identity)
//  3. unambiguous git identity email match
//  4. the global profile (the one with no directories)
func Profile(cfg *config.Config, dir string) (name, how string, err error) {
	if mapped, ok := dirs.Resolve(cfg, dir); ok {
		return mapped, "directory mapping", nil
	}
	if gitcmd.InsideWorkTree(dir) {
		_, origin, err := gitcmd.ConfigOriginValue(dir, "user.email")
		if err != nil {
			return "", "", err
		}
		if origin != "" {
			if candidate, ok := gitconfig.ProfileForGitconfigFile(origin); ok {
				if _, exists := cfg.GitProfiles[candidate]; exists {
					return candidate, "includeIf gitconfig", nil
				}
			}
		}
	}
	if email, err := gitcmd.ConfigValue(dir, "user.email"); err == nil && email != "" {
		var matches []string
		for n, p := range cfg.GitProfiles {
			if p.Email == email {
				matches = append(matches, n)
			}
		}
		if len(matches) == 1 {
			return matches[0], "matching git identity", nil
		}
	}
	if global, ok := cfg.GlobalGitProfile(); ok {
		return global.Username, "global profile", nil
	}
	return "", "", nil
}
