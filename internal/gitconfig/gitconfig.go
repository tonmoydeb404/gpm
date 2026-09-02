// Package gitconfig manages folder-based Git identities: one
// generated per-profile config file plus includeIf rules inside the
// gpm-managed block of the global gitconfig.
package gitconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitprofile"
	"github.com/tonmoydeb/gpm/internal/managed"
)

// Markers of the managed block, re-exported for callers and tests.
const (
	BeginMarker = managed.Begin
	EndMarker   = managed.End
)

// Dir returns the directory holding per-profile gitconfig files.
func Dir() (string, error) {
	base, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "gitconfig"), nil
}

// ProfilePath returns the path of the generated gitconfig for a profile.
func ProfilePath(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// ProfileFile renders the [user] section for a profile. The username
// is the commit author name.
func ProfileFile(p config.GitProfile) string {
	var b strings.Builder
	b.WriteString("[user]\n")
	b.WriteString(fmt.Sprintf("\tname = %s\n", p.Username))
	b.WriteString(fmt.Sprintf("\temail = %s\n", p.Email))
	return b.String()
}

// SyncFromConfig regenerates the per-profile gitconfig files and the
// includeIf rules in the global gitconfig. Orphaned profile files are
// removed.
func SyncFromConfig(cfg *config.Config) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	// Write one gitconfig per profile; collect valid names for cleanup.
	valid := map[string]bool{}
	names := gitprofile.SortedUsernames(cfg)
	for _, name := range names {
		p := cfg.GitProfiles[name]
		if err := config.ValidateName(name); err != nil {
			return fmt.Errorf("profile %s: %w", name, err)
		}
		valid[name] = true
		path, err := ProfilePath(name)
		if err != nil {
			return err
		}
		if err := config.AtomicWrite(path, []byte(ProfileFile(p)), 0o600); err != nil {
			return err
		}
	}

	// Remove files of deleted profiles.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read %s: %w", dir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !valid[e.Name()] {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return fmt.Errorf("remove orphaned %s: %w", e.Name(), err)
			}
		}
	}

	// Render the includeIf rules and update the global gitconfig.
	block, err := IncludeBlock(cfg)
	if err != nil {
		return err
	}
	global, err := GlobalPath()
	if err != nil {
		return err
	}
	return managed.Update(global, block, 0o644)
}

// GlobalPath returns the path of the user's global gitconfig.
func GlobalPath() (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".gitconfig"), nil
}

// IncludeBlock renders the managed includeIf block for all directory
// mappings across Git profiles. Sorted by directory for deterministic
// output. No mappings yield "".
func IncludeBlock(cfg *config.Config) (string, error) {
	mappings := dirs.All(cfg)
	if len(mappings) == 0 {
		return "", nil
	}
	type rule struct {
		dir     string
		profile string
		include string
	}
	rules := make([]rule, 0, len(mappings))
	for _, m := range mappings {
		abs, err := config.ExpandPath(m.Path)
		if err != nil {
			return "", fmt.Errorf("mapping %s: %w", m.Path, err)
		}
		include, err := ProfilePath(m.Profile)
		if err != nil {
			return "", err
		}
		rules = append(rules, rule{dir: abs, profile: m.Profile, include: include})
	}

	var b strings.Builder
	b.WriteString(BeginMarker)
	b.WriteString("\n")
	for _, r := range rules {
		b.WriteString(fmt.Sprintf("[includeIf \"gitdir:%s/\"]\n", r.dir))
		b.WriteString(fmt.Sprintf("\tpath = %s\n", r.include))
		b.WriteString("\n")
	}
	b.WriteString(EndMarker)
	b.WriteString("\n")
	return b.String(), nil
}

// ProfileForGitconfigFile maps an origin file path (as reported by
// `git config --show-origin`) back to a profile name when the file is
// a gpm-generated profile gitconfig.
func ProfileForGitconfigFile(path string) (string, bool) {
	dir, err := Dir()
	if err != nil {
		return "", false
	}
	parent, base := filepath.Split(filepath.Clean(path))
	if filepath.Clean(parent) != filepath.Clean(dir+"/") {
		return "", false
	}
	if base == "" || strings.Contains(base, "/") {
		return "", false
	}
	return base, true
}

// HasManagedBlock reports whether the global gitconfig contains the
// managed markers.
func HasManagedBlock() (bool, error) {
	global, err := GlobalPath()
	if err != nil {
		return false, err
	}
	return managed.HasBlock(global)
}
