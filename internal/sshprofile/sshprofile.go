// Package sshprofile implements operations on gpm SSH identities:
// validation, creation (with default ED25519 key generation), provider
// management, and removal. The CLI and TUI are thin wrappers around
// this package. SSH profiles are independent of Git profiles; a
// profile with an empty host alias is the global SSH identity and at
// most one profile can be global at a time.
package sshprofile

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/sshkey"
)

// ValidateName checks that a username is usable.
func ValidateName(username string) error {
	return config.ValidateName(username)
}

// ValidateAlias checks a host alias. An empty alias is allowed: the
// profile is then the global SSH identity.
func ValidateAlias(alias string) error {
	if alias == "" {
		return nil
	}
	return config.ValidateName(alias)
}

// ValidateHost checks that a provider hostname is well-formed.
func ValidateHost(host string) error {
	if host == "" {
		return fmt.Errorf("provider hostname must not be empty")
	}
	if len(host) > 253 {
		return fmt.Errorf("provider hostname is too long")
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
		default:
			return fmt.Errorf("invalid hostname %q: use letters, digits, dots and dashes", host)
		}
	}
	return nil
}

// NormalizeHost trims and lowercases a provider hostname.
func NormalizeHost(host string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if err := ValidateHost(host); err != nil {
		return "", err
	}
	return host, nil
}

// NormalizeProviders trims, lowercases, de-duplicates, and validates
// provider hostnames while preserving order.
func NormalizeProviders(providers []string) ([]string, error) {
	out := make([]string, 0, len(providers))
	seen := map[string]bool{}
	for _, h := range providers {
		h, err := NormalizeHost(h)
		if err != nil {
			return nil, err
		}
		if seen[h] {
			continue
		}
		seen[h] = true
		out = append(out, h)
	}
	return out, nil
}

// Validate checks a profile's fields.
func Validate(p config.SSHProfile) error {
	if err := ValidateName(p.Username); err != nil {
		return fmt.Errorf("username: %w", err)
	}
	if err := ValidateAlias(p.HostAlias); err != nil {
		return fmt.Errorf("host alias: %w", err)
	}
	if len(p.Providers) == 0 {
		return fmt.Errorf("at least one provider hostname is required")
	}
	for _, h := range p.Providers {
		if err := ValidateHost(h); err != nil {
			return fmt.Errorf("provider: %w", err)
		}
	}
	return nil
}

// checkAliasRules enforces the host-alias invariants: at most one
// profile may be global (empty alias) and non-empty aliases must be
// unique. The profile's own entry is exempt so edits are idempotent.
func checkAliasRules(cfg *config.Config, username, alias string) error {
	if alias == "" {
		if global, ok := cfg.GlobalSSHProfile(); ok && global.Username != username {
			return fmt.Errorf("%q is already the global ssh profile — only one profile can be global; set a host alias instead", global.Username)
		}
		return nil
	}
	for name, p := range cfg.SSHProfiles {
		if name != username && p.HostAlias == alias {
			return fmt.Errorf("host alias %q is already used by %q", alias, name)
		}
	}
	return nil
}

// DefaultKeyPath is the conventional private key path for a profile.
func DefaultKeyPath(username string) (string, error) {
	home, err := config.Home()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".ssh", "id_ed25519_"+username), nil
}

// AddOpts controls Add.
type AddOpts struct {
	GenerateKey bool // generate a fresh ED25519 key pair (the default flow)
}

// AddResult reports what Add did.
type AddResult struct {
	GeneratedKey bool   // whether a new key pair was created
	PublicKey    string // authorized_keys line when a key was generated
	KeyPath      string
}

// Add validates and inserts a new SSH profile. With GenerateKey a
// fresh key pair is created at the conventional path; the username is
// embedded as the key comment. Existing keys must be present on disk.
// The caller is responsible for saving cfg and syncing
// ~/.ssh/config afterwards.
func Add(cfg *config.Config, p config.SSHProfile, opts AddOpts) (AddResult, error) {
	var res AddResult
	p.Username = strings.TrimSpace(p.Username)
	p.HostAlias = strings.TrimSpace(p.HostAlias)
	if err := Validate(p); err != nil {
		return res, err
	}
	if _, exists := cfg.SSHProfiles[p.Username]; exists {
		return res, fmt.Errorf("ssh profile %q already exists", p.Username)
	}
	if err := checkAliasRules(cfg, p.Username, p.HostAlias); err != nil {
		return res, err
	}
	providers, err := NormalizeProviders(p.Providers)
	if err != nil {
		return res, err
	}
	p.Providers = providers

	switch {
	case p.KeyPath != "":
		expanded, err := config.ExpandPath(p.KeyPath)
		if err != nil {
			return res, err
		}
		if !sshkey.Exists(expanded) {
			return res, fmt.Errorf("key %s does not exist", expanded)
		}
		p.KeyPath = expanded
		res.KeyPath = expanded
	case opts.GenerateKey:
		path, err := DefaultKeyPath(p.Username)
		if err != nil {
			return res, err
		}
		pub, err := sshkey.Generate(path, p.Username)
		if err != nil {
			return res, err
		}
		p.KeyPath = path
		res.GeneratedKey = true
		res.PublicKey = pub
		res.KeyPath = path
	default:
		return res, fmt.Errorf("no key: generate one or pass an existing key path")
	}

	cfg.SSHProfiles[p.Username] = p
	return res, nil
}

// Edit applies fn to an existing profile. The username is the
// profile's identity and cannot be changed; delete and re-create to
// rename. Host-alias invariants (one global, unique aliases) are
// re-checked after the edit.
func Edit(cfg *config.Config, username string, fn func(p *config.SSHProfile)) error {
	p, ok := cfg.SSHProfiles[username]
	if !ok {
		return fmt.Errorf("ssh profile %q does not exist", username)
	}
	fn(&p)
	p.Username = username
	p.HostAlias = strings.TrimSpace(p.HostAlias)
	if err := Validate(p); err != nil {
		return err
	}
	if err := checkAliasRules(cfg, username, p.HostAlias); err != nil {
		return err
	}
	providers, err := NormalizeProviders(p.Providers)
	if err != nil {
		return err
	}
	if len(providers) == 0 {
		return fmt.Errorf("at least one provider hostname is required")
	}
	p.Providers = providers
	cfg.SSHProfiles[username] = p
	return nil
}

// AddProvider attaches another provider hostname to a profile.
func AddProvider(cfg *config.Config, username, host string) error {
	host, err := NormalizeHost(host)
	if err != nil {
		return err
	}
	p, _, err := Get(cfg, username)
	if err != nil {
		return err
	}
	for _, h := range p.Providers {
		if h == host {
			return fmt.Errorf("%s is already a provider of %q", host, username)
		}
	}
	return Edit(cfg, username, func(p *config.SSHProfile) {
		p.Providers = append(p.Providers, host)
	})
}

// RemoveProvider detaches a provider hostname. The last provider
// cannot be removed; delete the profile instead.
func RemoveProvider(cfg *config.Config, username, host string) error {
	host, err := NormalizeHost(host)
	if err != nil {
		return err
	}
	p, _, err := Get(cfg, username)
	if err != nil {
		return err
	}
	found := false
	for _, h := range p.Providers {
		if h == host {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%s is not a provider of %q", host, username)
	}
	if len(p.Providers) == 1 {
		return fmt.Errorf("cannot remove the last provider of %q — delete the profile instead", username)
	}
	return Edit(cfg, username, func(p *config.SSHProfile) {
		kept := p.Providers[:0]
		for _, h := range p.Providers {
			if h != host {
				kept = append(kept, h)
			}
		}
		p.Providers = kept
	})
}

// RemoveOpts controls Remove.
type RemoveOpts struct {
	RemoveKey bool // also delete the key pair from disk
}

// RemoveResult reports what Remove did.
type RemoveResult struct {
	RemovedKeyPath string
}

// Remove deletes an SSH profile and optionally its key pair.
func Remove(cfg *config.Config, username string, opts RemoveOpts) (RemoveResult, error) {
	var res RemoveResult
	p, ok := cfg.SSHProfiles[username]
	if !ok {
		return res, fmt.Errorf("ssh profile %q does not exist", username)
	}
	if opts.RemoveKey && p.KeyPath != "" {
		if err := sshkey.Remove(p.KeyPath); err != nil {
			return res, err
		}
		res.RemovedKeyPath = p.KeyPath
	}
	delete(cfg.SSHProfiles, username)
	return res, nil
}

// Get returns a profile by username.
func Get(cfg *config.Config, username string) (config.SSHProfile, bool, error) {
	if err := ValidateName(username); err != nil {
		return config.SSHProfile{}, false, err
	}
	p, ok := cfg.SSHProfiles[username]
	return p, ok, nil
}

// SortedUsernames returns all SSH profile usernames in deterministic
// order.
func SortedUsernames(cfg *config.Config) []string {
	names := make([]string, 0, len(cfg.SSHProfiles))
	for name := range cfg.SSHProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
