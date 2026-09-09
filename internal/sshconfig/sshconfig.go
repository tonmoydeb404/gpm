// Package sshconfig manages the gpm-owned section of the user's
// ~/.ssh/config. Everything between the managed markers is rewritten
// by gpm; everything outside is preserved byte-for-byte.
package sshconfig

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/managed"
	"github.com/tonmoydeb404/gpm/internal/sshprofile"
)

// Markers of the managed block, re-exported for callers and tests.
const (
	BeginMarker = managed.Begin
	EndMarker   = managed.End
)

// Host is a single Host stanza inside the managed block.
type Host struct {
	Alias        string
	HostName     string
	User         string
	IdentityFile string
}

// ManagedBlock renders the gpm-managed section for the given hosts,
// sorted by alias for deterministic output. Empty input yields "".
func ManagedBlock(hosts []Host) string {
	if len(hosts) == 0 {
		return ""
	}
	sorted := make([]Host, len(hosts))
	copy(sorted, hosts)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Alias < sorted[j].Alias })

	var b []byte
	b = append(b, BeginMarker...)
	b = append(b, '\n')
	for _, h := range sorted {
		b = append(b, fmt.Sprintf("Host %s\n", h.Alias)...)
		b = append(b, fmt.Sprintf("  HostName %s\n", h.HostName)...)
		if h.User != "" {
			b = append(b, fmt.Sprintf("  User %s\n", h.User)...)
		}
		if h.IdentityFile != "" {
			b = append(b, fmt.Sprintf("  IdentityFile %s\n", h.IdentityFile)...)
		}
		b = append(b, "  IdentitiesOnly yes\n\n"...)
	}
	b = append(b, EndMarker...)
	b = append(b, '\n')
	return string(b)
}

// Update rewrites the managed block of the SSH config at path,
// backing up the previous file first. When hosts is empty the block
// is removed entirely. Missing files are created.
func Update(path string, hosts []Host) error {
	return managed.Update(path, ManagedBlock(hosts), 0o600)
}

// Stanzas returns the Host stanzas for every SSH profile with a key,
// a host alias, and at least one provider. Global profiles (empty
// alias) are not wired into the ssh config. The profile's alias names
// its first provider's stanza; additional providers get a
// <alias>-<host> suffix so the stanzas stay distinguishable.
func Stanzas(cfg *config.Config) []Host {
	hosts := make([]Host, 0, len(cfg.SSHProfiles))
	for _, username := range sshprofile.SortedUsernames(cfg) {
		p := cfg.SSHProfiles[username]
		if p.KeyPath == "" || p.HostAlias == "" || len(p.Providers) == 0 {
			continue
		}
		for i, provider := range p.Providers {
			alias := p.HostAlias
			if i > 0 {
				alias = p.HostAlias + "-" + strings.ReplaceAll(provider, ".", "-")
			}
			hosts = append(hosts, Host{
				Alias:        alias,
				HostName:     provider,
				User:         "git",
				IdentityFile: p.KeyPath,
			})
		}
	}
	return hosts
}

// SyncFromConfig regenerates the managed block from gpm's config so
// that every SSH profile with a key gets a Host stanza per provider.
func SyncFromConfig(cfg *config.Config) error {
	path, err := config.SSHConfigPath()
	if err != nil {
		return err
	}
	return Update(path, Stanzas(cfg))
}

// HasManagedBlock reports whether the SSH config contains the managed
// markers.
func HasManagedBlock(path string) (bool, error) {
	return managed.HasBlock(path)
}
