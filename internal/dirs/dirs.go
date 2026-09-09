// Package dirs manages directory → Git identity mappings. Each
// directory belongs to exactly one Git profile (directories are
// stored on the profile itself); nested mappings are supported and
// the deepest match wins on resolve. Removing the last directory of
// a profile makes it the global identity, so at most one profile can
// be directory-free.
package dirs

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonmoydeb404/gpm/internal/config"
)

// Mapping is a flattened directory → username view used for display,
// includeIf rendering, and resolution.
type Mapping struct {
	Path    string // absolute directory
	Profile string // git profile username
}

// Normalize expands ~ and cleans a directory path to an absolute form.
func Normalize(p string) (string, error) {
	abs, err := config.ExpandPath(p)
	if err != nil {
		return "", err
	}
	if abs == "" {
		return "", fmt.Errorf("directory path must not be empty")
	}
	if !filepath.IsAbs(abs) {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		abs = filepath.Join(wd, abs)
	}
	return abs, nil
}

// Shorten renders a path for display, collapsing the home directory
// prefix to ~. Symlink-resolved homes (e.g. /tmp -> /private/tmp on
// macOS) are handled too.
func Shorten(p string) string {
	home, err := config.Home()
	if err != nil || home == "" {
		return p
	}
	home = filepath.Clean(home)
	for _, prefix := range homePrefixes(home) {
		if p == prefix {
			return "~"
		}
		if strings.HasPrefix(p, prefix+string(filepath.Separator)) {
			return "~" + strings.TrimPrefix(p, prefix)
		}
	}
	return p
}

// homePrefixes returns the home directory and its symlink-resolved
// form (when different).
func homePrefixes(home string) []string {
	prefixes := []string{home}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		prefixes = append(prefixes, resolved)
	}
	return prefixes
}

// Owner returns the username of the profile that owns the exact
// directory path.
func Owner(cfg *config.Config, path string) (string, bool) {
	for username, p := range cfg.GitProfiles {
		for _, d := range p.Directories {
			if d == path {
				return username, true
			}
		}
	}
	return "", false
}

// All returns every mapping across all Git profiles, sorted by path.
func All(cfg *config.Config) []Mapping {
	out := make([]Mapping, 0, 8)
	for username, p := range cfg.GitProfiles {
		for _, d := range p.Directories {
			out = append(out, Mapping{Path: d, Profile: username})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Count returns the total number of mapped directories.
func Count(cfg *config.Config) int {
	n := 0
	for _, p := range cfg.GitProfiles {
		n += len(p.Directories)
	}
	return n
}

// AddResult reports what Add did.
type AddResult struct {
	Path     string
	Replaced bool // an existing mapping for the same path was overwritten
	Moved    bool // the path was taken away from another profile
	Notes    []string
}

// Add maps dir to the git profile username. The directory must not
// belong to another profile unless force is set. Adding the first
// directory to the global profile silently ends its global reign and
// says so in the notes.
func Add(cfg *config.Config, path, username string, force bool) (AddResult, error) {
	var res AddResult
	p, ok := cfg.GitProfiles[username]
	if !ok {
		return res, fmt.Errorf("git profile %q does not exist", username)
	}
	abs, err := Normalize(path)
	if err != nil {
		return res, err
	}
	res.Path = abs

	owner, owned := Owner(cfg, abs)
	if owned && owner != username && !force {
		return res, fmt.Errorf("%s is already mapped to %q (use --force to remap)", Shorten(abs), owner)
	}
	if owned && owner != username {
		if err := detach(cfg, owner, abs); err != nil {
			return res, err
		}
		res.Moved = true
	}

	// Replace same-path entry if present, then append.
	directories := make([]string, 0, len(p.Directories)+1)
	replaced := false
	for _, d := range p.Directories {
		if d == abs {
			if replaced {
				continue // defensive: drop duplicates
			}
			replaced = true
			continue
		}
		directories = append(directories, d)
	}
	directories = append(directories, abs)
	p.Directories = directories
	cfg.GitProfiles[username] = p
	res.Replaced = replaced

	// Adding a first directory retires the profile as the global identity.
	if len(directories) == 1 {
		res.Notes = append(res.Notes,
			fmt.Sprintf("note: %q is no longer the global identity (it is now mapped to %s)", username, Shorten(abs)))
	}

	// Informational notes about nesting with different profiles.
	for _, m := range All(cfg) {
		if m.Path == abs || m.Profile == username {
			continue
		}
		switch {
		case isParent(m.Path, abs):
			res.Notes = append(res.Notes,
				fmt.Sprintf("note: %s maps to %q; %s now shadows it for deeper paths", Shorten(m.Path), m.Profile, Shorten(abs)))
		case isParent(abs, m.Path):
			res.Notes = append(res.Notes,
				fmt.Sprintf("note: %s maps to %q and shadows %s for deeper paths", Shorten(abs), username, Shorten(m.Path)))
		}
	}
	return res, nil
}

// Remove deletes the mapping for an exact path. Removing a profile's
// last directory turns it into the global identity, which is refused
// while another profile is already global.
func Remove(cfg *config.Config, path string) (Mapping, error) {
	abs, err := Normalize(path)
	if err != nil {
		return Mapping{}, err
	}
	username, ok := Owner(cfg, abs)
	if !ok {
		return Mapping{}, fmt.Errorf("no mapping for %s", Shorten(abs))
	}
	if global, is := cfg.GlobalGitProfile(); is && global.Username != username {
		p := cfg.GitProfiles[username]
		if len(p.Directories) == 1 {
			return Mapping{}, fmt.Errorf("removing the last directory of %q would make it the global identity, but %q is already global — only one profile can be global", username, global.Username)
		}
	}
	if err := detach(cfg, username, abs); err != nil {
		return Mapping{}, err
	}
	return Mapping{Path: abs, Profile: username}, nil
}

// detach drops path from the profile's directory list.
func detach(cfg *config.Config, username, path string) error {
	p, ok := cfg.GitProfiles[username]
	if !ok {
		return fmt.Errorf("git profile %q does not exist", username)
	}
	kept := p.Directories[:0]
	for _, d := range p.Directories {
		if d == path {
			continue
		}
		kept = append(kept, d)
	}
	p.Directories = kept
	cfg.GitProfiles[username] = p
	return nil
}

// Resolve returns the username governing dir using the deepest
// matching mapping. Boundary-aware: a mapping for /a/b does not match
// /a/bc.
func Resolve(cfg *config.Config, dir string) (string, bool) {
	abs, err := Normalize(dir)
	if err != nil {
		return "", false
	}
	bestLen := -1
	best := ""
	for _, m := range All(cfg) {
		if !isParent(m.Path, abs) {
			continue
		}
		if len(m.Path) > bestLen {
			bestLen = len(m.Path)
			best = m.Profile
		}
	}
	return best, bestLen >= 0
}

// isParent reports whether child is dir or a descendant of parent,
// respecting path boundaries.
func isParent(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	if parent == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}
