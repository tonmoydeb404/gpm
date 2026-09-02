// Package config manages gpm's own configuration file, safe atomic
// writes, and backups of foreign config files (e.g. ~/.ssh/config).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// nameRe restricts profile names to characters that are safe in file
// names, TOML table keys, and SSH host aliases.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidateName checks that a profile name is usable everywhere it
// appears (file names, TOML keys, SSH aliases).
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name must not be empty")
	}
	if len(name) > 64 {
		return fmt.Errorf("profile name must be at most 64 characters")
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: use letters, digits, '.', '_' or '-' (must start alphanumeric)", name)
	}
	return nil
}

// SSHProfile describes one SSH identity managed by gpm: a named
// ED25519 key pair plus the provider hostnames it authenticates
// against. It is independent of any Git identity. An empty host
// alias marks the global SSH identity; at most one profile may be
// global at a time.
type SSHProfile struct {
	Username  string   `toml:"username"`             // map key
	KeyPath   string   `toml:"key_path,omitempty"`   // path of the private key
	HostAlias string   `toml:"host_alias,omitempty"` // ssh config alias; empty = global
	Providers []string `toml:"providers,omitempty"`  // provider hostnames, e.g. github.com
}

// GitProfile describes one Git identity managed by gpm: the username
// (also the commit author name) and email it commits with, plus the
// directories mapped to it. A profile with no directories is the
// global identity; at most one profile may be global at a time.
type GitProfile struct {
	Username    string   `toml:"username"`              // map key; also git user.name
	Email       string   `toml:"email"`                 // git user.email
	Directories []string `toml:"directories,omitempty"` // absolute directories mapped to this identity
}

// Config is the root of gpm's own configuration. SSH and Git
// profiles are independent entities.
type Config struct {
	GitProfiles map[string]GitProfile `toml:"git_profiles"`
	SSHProfiles map[string]SSHProfile `toml:"ssh_profiles"`
}

// New returns an empty, ready-to-use config.
func New() *Config {
	return &Config{
		GitProfiles: map[string]GitProfile{},
		SSHProfiles: map[string]SSHProfile{},
	}
}

// GlobalGitProfile returns the profile with no directories — the
// identity that applies where no directory mapping matches. At most
// one profile can be global; the pick is deterministic even if a
// hand-edited config contains several.
func (c *Config) GlobalGitProfile() (GitProfile, bool) {
	names := make([]string, 0, len(c.GitProfiles))
	for name := range c.GitProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if len(c.GitProfiles[name].Directories) == 0 {
			return c.GitProfiles[name], true
		}
	}
	return GitProfile{}, false
}

// GlobalSSHProfile returns the ssh profile with no host alias — the
// global SSH identity. At most one profile can be global; the pick
// is deterministic even if a hand-edited config contains several.
func (c *Config) GlobalSSHProfile() (SSHProfile, bool) {
	names := make([]string, 0, len(c.SSHProfiles))
	for name := range c.SSHProfiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if c.SSHProfiles[name].HostAlias == "" {
			return c.SSHProfiles[name], true
		}
	}
	return SSHProfile{}, false
}

// Home returns the directory gpm treats as the user's home. Setting
// GPM_HOME (absolute or relative path) redirects everything gpm reads
// or writes — its own config, ~/.ssh, ~/.gitconfig, ~ expansion —
// into that sandbox, leaving the real home untouched.
func Home() (string, error) {
	if dev := os.Getenv("GPM_HOME"); dev != "" {
		abs, err := filepath.Abs(dev)
		if err != nil {
			return "", fmt.Errorf("resolve GPM_HOME %q: %w", dev, err)
		}
		return filepath.Clean(abs), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

// Dir returns the gpm configuration directory. XDG_CONFIG_HOME is
// respected in normal operation; in sandbox mode (GPM_HOME) all state
// lives under the sandbox regardless of XDG.
func Dir() (string, error) {
	if os.Getenv("GPM_HOME") != "" {
		home, err := Home()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "gpm"), nil
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "gpm"), nil
	}
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "gpm"), nil
}

// Path returns the path of gpm's config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// SSHDir returns the user's ~/.ssh directory.
func SSHDir() (string, error) {
	home, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh"), nil
}

// SSHConfigPath returns the path of the user's ~/.ssh/config.
func SSHConfigPath() (string, error) {
	dir, err := SSHDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config"), nil
}

// Load reads gpm's config. A missing file yields an empty config, not an error.
func Load() (*Config, error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return New(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	cfg := New()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.GitProfiles == nil {
		cfg.GitProfiles = map[string]GitProfile{}
	}
	if cfg.SSHProfiles == nil {
		cfg.SSHProfiles = map[string]SSHProfile{}
	}
	cfg.normalize()
	return cfg, nil
}

// normalize repairs profiles whose username field is missing but
// whose TOML table key carries it (e.g. configs written before the
// username field existed or edited by hand).
func (c *Config) normalize() {
	for key, p := range c.GitProfiles {
		if p.Username == "" {
			p.Username = key
			c.GitProfiles[key] = p
		}
	}
	for key, p := range c.SSHProfiles {
		if p.Username == "" {
			p.Username = key
			c.SSHProfiles[key] = p
		}
	}
}

// Save atomically writes the config to disk with 0600 permissions.
func (c *Config) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	data, err := toml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return AtomicWrite(path, data, 0o600)
}

// ExpandPath resolves a leading ~ to the home directory (GPM_HOME
// aware) and cleans the result.
func ExpandPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := Home()
		if err != nil {
			return "", err
		}
		return filepath.Clean(filepath.Join(home, strings.TrimPrefix(p, "~"))), nil
	}
	return filepath.Clean(p), nil
}

// AtomicWrite writes data to path via a temp file and rename so that a
// crash never leaves a truncated file behind. Parent directories are
// created with 0700.
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".gpm-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName) // no-op after successful rename
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	return nil
}

// backupKeep is the number of backups retained per source file.
const backupKeep = 10

// BackupFile copies path into ~/.config/gpm/backups before it is
// modified. Missing files are silently skipped. Only the most recent
// backupKeep backups per file name are retained.
func BackupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	backupDir := filepath.Join(dir, "backups")
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", backupDir, err)
	}
	base := filepath.Base(path)
	stamp := time.Now().Format("20060102-150405.000000000")
	dst := filepath.Join(backupDir, base+"."+stamp)
	if err := os.WriteFile(dst, data, 0o600); err != nil {
		return fmt.Errorf("write backup %s: %w", dst, err)
	}
	return pruneBackups(backupDir, base, backupKeep)
}

// pruneBackups removes the oldest backups of base beyond keep.
func pruneBackups(backupDir, base string, keep int) error {
	matches, err := filepath.Glob(filepath.Join(backupDir, base+".*"))
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	if len(matches) <= keep {
		return nil
	}
	// Names embed a zero-padded timestamp, so lexical order is chronological.
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-keep] {
		if err := os.Remove(old); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove old backup %s: %w", old, err)
		}
	}
	return nil
}
