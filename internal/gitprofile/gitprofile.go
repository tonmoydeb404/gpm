// Package gitprofile implements operations on gpm Git identities:
// validation, creation, editing, and removal. The CLI and TUI are
// thin wrappers around this package. Git profiles are independent of
// SSH profiles; a profile without directories is the global identity
// and at most one profile can be global at a time.
package gitprofile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
)

// Validate checks a git profile's fields.
func Validate(p config.GitProfile) error {
	if err := config.ValidateName(p.Username); err != nil {
		return fmt.Errorf("github username: %w", err)
	}
	if !strings.Contains(p.Email, "@") {
		return fmt.Errorf("git user.email %q does not look like an email address", p.Email)
	}
	return nil
}

// AddResult reports what Add did.
type AddResult struct {
	IsGlobal bool // created without a directory: this is the global identity
}

// Add validates and inserts a new git profile keyed by its username.
// directory may be empty to create the global identity, which is
// refused while another global profile exists. The caller is
// responsible for saving cfg and syncing the managed artifacts.
func Add(cfg *config.Config, p config.GitProfile, directory string) (AddResult, error) {
	var res AddResult
	p.Username = strings.TrimSpace(p.Username)
	if err := Validate(p); err != nil {
		return res, err
	}
	if _, exists := cfg.GitProfiles[p.Username]; exists {
		return res, fmt.Errorf("git profile %q already exists", p.Username)
	}
	if directory == "" {
		if global, is := cfg.GlobalGitProfile(); is {
			return res, fmt.Errorf("%q is already the global profile — only one profile can be global; map this profile to a directory instead", global.Username)
		}
		res.IsGlobal = true
	} else {
		abs, err := dirs.Normalize(directory)
		if err != nil {
			return res, err
		}
		if owner, ok := dirs.Owner(cfg, abs); ok {
			return res, fmt.Errorf("%s is already mapped to %q", dirs.Shorten(abs), owner)
		}
		p.Directories = []string{abs}
	}
	cfg.GitProfiles[p.Username] = p
	return res, nil
}

// Edit applies fn to an existing profile. The username is the
// profile's identity and cannot be changed; delete and re-create to
// rename.
func Edit(cfg *config.Config, username string, fn func(p *config.GitProfile)) error {
	p, ok := cfg.GitProfiles[username]
	if !ok {
		return fmt.Errorf("git profile %q does not exist", username)
	}
	fn(&p)
	p.Username = username
	if err := Validate(p); err != nil {
		return err
	}
	cfg.GitProfiles[username] = p
	return nil
}

// RemoveResult reports what Remove did.
type RemoveResult struct {
	RemovedDirs int  // directory mappings that died with the profile
	WasGlobal   bool // the profile had no directories
}

// Remove deletes a git profile together with its directory mappings.
func Remove(cfg *config.Config, username string) (RemoveResult, error) {
	var res RemoveResult
	p, ok := cfg.GitProfiles[username]
	if !ok {
		return res, fmt.Errorf("git profile %q does not exist", username)
	}
	res.RemovedDirs = len(p.Directories)
	res.WasGlobal = res.RemovedDirs == 0
	delete(cfg.GitProfiles, username)
	return res, nil
}

// Get returns a profile by username.
func Get(cfg *config.Config, username string) (config.GitProfile, bool, error) {
	if err := config.ValidateName(username); err != nil {
		return config.GitProfile{}, false, err
	}
	p, ok := cfg.GitProfiles[username]
	return p, ok, nil
}

// SortedUsernames returns all git profile usernames in deterministic order.
func SortedUsernames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.GitProfiles))
	for name := range cfg.GitProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
