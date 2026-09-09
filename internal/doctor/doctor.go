// Package doctor runs health checks over the gpm-managed setup:
// Git profiles, SSH profiles, keys and permissions, ssh config,
// gitconfig includeIf rules, directory mappings, the current repo
// identity, and optionally live provider authentication. Fixable
// problems are repaired when Options.Fix is set.
package doctor

import (
	"fmt"
	"os"

	"github.com/tonmoydeb404/gpm/internal/auth"
	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/dirs"
	"github.com/tonmoydeb404/gpm/internal/gitcmd"
	"github.com/tonmoydeb404/gpm/internal/gitconfig"
	"github.com/tonmoydeb404/gpm/internal/gitprofile"
	"github.com/tonmoydeb404/gpm/internal/managed"
	"github.com/tonmoydeb404/gpm/internal/resolve"
	"github.com/tonmoydeb404/gpm/internal/sshconfig"
	"github.com/tonmoydeb404/gpm/internal/sshkey"
	"github.com/tonmoydeb404/gpm/internal/sshprofile"
)

// Status is the outcome of a check.
type Status string

const (
	Pass Status = "pass"
	Warn Status = "warn"
	Fail Status = "fail"
)

// Result is one check outcome with an actionable hint.
type Result struct {
	Name   string
	Status Status
	Detail string
	Hint   string
}

// Options controls a doctor run.
type Options struct {
	Fix     bool   // auto-repair what can be repaired
	Network bool   // run live provider auth tests
	Dir     string // directory for the repo identity check ("" = skip)
}

// Run executes all checks and returns their results.
func Run(cfg *config.Config, opts Options) []Result {
	var results []Result
	results = append(results, checkGitProfiles(cfg)...)
	results = append(results, checkGlobal(cfg)...)
	results = append(results, checkSSHProfiles(cfg)...)
	results = append(results, checkKeys(cfg)...)
	results = append(results, checkPermissions(cfg, opts)...)
	results = append(results, checkSSHConfig(cfg, opts)...)
	results = append(results, checkGitconfig(cfg, opts)...)
	results = append(results, checkMappings(cfg)...)
	if opts.Dir != "" {
		results = append(results, checkRepoIdentity(cfg, opts.Dir))
	}
	if opts.Network {
		results = append(results, checkNetwork(cfg)...)
	}
	return results
}

func checkGitProfiles(cfg *config.Config) []Result {
	if len(cfg.GitProfiles) == 0 {
		return []Result{{Name: "Git profiles", Status: Warn, Detail: "none defined",
			Hint: "run: gpm profile add <username> --email ..."}}
	}
	var res Result
	res.Name = "Git profiles"
	for _, name := range gitprofile.SortedUsernames(cfg) {
		if err := gitprofile.Validate(cfg.GitProfiles[name]); err != nil {
			res.Status = Fail
			res.Detail += fmt.Sprintf("%s: %v; ", name, err)
		}
	}
	if res.Status == "" {
		res.Status = Pass
		res.Detail = fmt.Sprintf("%d profile(s) valid", len(cfg.GitProfiles))
	}
	return []Result{res}
}

func checkGlobal(cfg *config.Config) []Result {
	var globals []string
	for _, name := range gitprofile.SortedUsernames(cfg) {
		if len(cfg.GitProfiles[name].Directories) == 0 {
			globals = append(globals, name)
		}
	}
	switch len(globals) {
	case 0:
		return []Result{{Name: "Global profile", Status: Warn, Detail: "no global profile",
			Hint: "a profile without directories is global; remove a mapping with: gpm dir remove <path>"}}
	case 1:
		return []Result{{Name: "Global profile", Status: Pass, Detail: globals[0]}}
	default:
		return []Result{{Name: "Global profile", Status: Fail,
			Detail: fmt.Sprintf("multiple global profiles: %v — only one can be global", globals),
			Hint:   "map all but one to a directory: gpm dir add <path> <username>"}}
	}
}

func checkSSHProfiles(cfg *config.Config) []Result {
	if len(cfg.SSHProfiles) == 0 {
		return []Result{{Name: "SSH profiles", Status: Warn, Detail: "none defined",
			Hint: "run: gpm ssh add <name> --provider github.com"}}
	}
	var res Result
	res.Name = "SSH profiles"
	for _, name := range sshprofile.SortedUsernames(cfg) {
		if err := sshprofile.Validate(cfg.SSHProfiles[name]); err != nil {
			res.Status = Fail
			res.Detail += fmt.Sprintf("%s: %v; ", name, err)
			continue
		}
		if cfg.SSHProfiles[name].KeyPath == "" {
			res.Status = Fail
			res.Detail += fmt.Sprintf("%s: no key; ", name)
		}
	}
	if res.Status == "" {
		res.Status = Pass
		res.Detail = fmt.Sprintf("%d profile(s) valid", len(cfg.SSHProfiles))
	}
	return []Result{res}
}

func checkKeys(cfg *config.Config) []Result {
	var res Result
	res.Name = "SSH keys"
	total, missing := 0, 0
	for _, name := range sshprofile.SortedUsernames(cfg) {
		p := cfg.SSHProfiles[name]
		if p.KeyPath == "" {
			continue
		}
		total++
		if !sshkey.Exists(p.KeyPath) {
			missing++
			res.Detail += fmt.Sprintf("%s: %s missing; ", name, p.KeyPath)
			res.Hint = "run: gpm key generate <name>"
		}
	}
	if missing > 0 {
		res.Status = Fail
		return []Result{res}
	}
	res.Status = Pass
	res.Detail = fmt.Sprintf("%d key pair(s) present", total)
	return []Result{res}
}

func checkPermissions(cfg *config.Config, opts Options) []Result {
	var res Result
	res.Name = "Permissions"
	fixed := false

	if sshDir, err := config.SSHDir(); err == nil {
		if info, err := os.Stat(sshDir); err == nil && info.Mode().Perm() != 0o700 {
			if opts.Fix {
				if err := os.Chmod(sshDir, 0o700); err == nil {
					res.Detail += "~/.ssh -> 0700 (fixed); "
					fixed = true
				}
			} else {
				res.Status = Warn
				res.Detail += fmt.Sprintf("~/.ssh is %o, want 700; ", info.Mode().Perm())
				res.Hint = "run: gpm doctor --fix"
			}
		}
	}

	for _, name := range sshprofile.SortedUsernames(cfg) {
		p := cfg.SSHProfiles[name]
		if p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
			continue
		}
		if info, err := os.Stat(p.KeyPath); err == nil && info.Mode().Perm() != 0o600 {
			if opts.Fix {
				if err := os.Chmod(p.KeyPath, 0o600); err == nil {
					res.Detail += fmt.Sprintf("%s -> 0600 (fixed); ", name)
					fixed = true
				}
			} else {
				res.Status = Warn
				res.Detail += fmt.Sprintf("%s private key is %o, want 600; ", name, info.Mode().Perm())
				res.Hint = "run: gpm doctor --fix"
			}
		}
		pub := sshkey.PubPath(p.KeyPath)
		if info, err := os.Stat(pub); err == nil && info.Mode().Perm() != 0o644 {
			if opts.Fix {
				if err := os.Chmod(pub, 0o644); err == nil {
					res.Detail += fmt.Sprintf("%s.pub -> 0644 (fixed); ", name)
					fixed = true
				}
			} else {
				res.Status = Warn
				res.Detail += fmt.Sprintf("%s public key is %o, want 644; ", name, info.Mode().Perm())
				res.Hint = "run: gpm doctor --fix"
			}
		}
	}

	if res.Status == "" {
		res.Status = Pass
		if fixed {
			res.Detail += "permissions repaired"
		} else {
			res.Detail = "all permissions correct"
		}
	}
	return []Result{res}
}

func checkSSHConfig(cfg *config.Config, opts Options) []Result {
	path, err := config.SSHConfigPath()
	if err != nil {
		return []Result{{Name: "SSH config", Status: Fail, Detail: err.Error()}}
	}
	stanzas := sshconfig.Stanzas(cfg)
	needBlock := len(stanzas) > 0

	var res Result
	res.Name = "SSH config"

	has, _ := managed.HasBlock(path)
	if needBlock && !has {
		if opts.Fix {
			if err := sshconfig.SyncFromConfig(cfg); err != nil {
				res.Status = Fail
				res.Detail = "managed block missing and sync failed: " + err.Error()
				return []Result{res}
			}
			res.Status = Pass
			res.Detail = "managed block missing; regenerated"
			return []Result{res}
		}
		res.Status = Fail
		res.Detail = "managed block missing from ~/.ssh/config"
		res.Hint = "run: gpm sync (or gpm doctor --fix)"
		return []Result{res}
	}

	aliases := map[string]bool{}
	for _, h := range stanzas {
		if aliases[h.Alias] {
			res.Status = Fail
			res.Detail = "duplicate host alias: " + h.Alias
			res.Hint = "delete and re-create the conflicting ssh profile"
			return []Result{res}
		}
		aliases[h.Alias] = true
	}

	res.Status = Pass
	res.Detail = fmt.Sprintf("managed block present, %d host stanza(s)", len(stanzas))
	return []Result{res}
}

func checkGitconfig(cfg *config.Config, opts Options) []Result {
	var res Result
	res.Name = "Git config"

	// Per-profile files must exist.
	missing := 0
	for _, name := range gitprofile.SortedUsernames(cfg) {
		path, err := gitconfig.ProfilePath(name)
		if err != nil {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			missing++
		}
	}
	if missing > 0 {
		if opts.Fix {
			if err := gitconfig.SyncFromConfig(cfg); err != nil {
				res.Status = Fail
				res.Detail = "sync failed: " + err.Error()
				return []Result{res}
			}
			res.Detail = fmt.Sprintf("%d profile gitconfig file(s) regenerated; ", missing)
		} else {
			res.Status = Fail
			res.Detail = fmt.Sprintf("%d profile gitconfig file(s) missing; ", missing)
			res.Hint = "run: gpm sync (or gpm doctor --fix)"
			return []Result{res}
		}
	}

	// The includeIf block must match the mapping state.
	global, err := gitconfig.GlobalPath()
	if err != nil {
		return []Result{{Name: "Git config", Status: Fail, Detail: err.Error()}}
	}
	hasBlock, _ := managed.HasBlock(global)
	totalMappings := dirs.Count(cfg)
	switch {
	case totalMappings > 0 && !hasBlock:
		if opts.Fix {
			if err := gitconfig.SyncFromConfig(cfg); err != nil {
				res.Status = Fail
				res.Detail = "sync failed: " + err.Error()
				return []Result{res}
			}
			res.Status = Pass
			res.Detail += "includeIf block missing; regenerated"
			return []Result{res}
		}
		res.Status = Fail
		res.Detail = "directory mappings exist but the includeIf block is missing"
		res.Hint = "run: gpm sync (or gpm doctor --fix)"
		return []Result{res}
	case totalMappings == 0 && hasBlock:
		if opts.Fix {
			if err := gitconfig.SyncFromConfig(cfg); err != nil {
				res.Status = Fail
				res.Detail = "sync failed: " + err.Error()
				return []Result{res}
			}
			res.Status = Pass
			res.Detail += "stale includeIf block removed"
			return []Result{res}
		}
		res.Status = Warn
		res.Detail = "stale includeIf block with no directory mappings"
		res.Hint = "run: gpm sync (or gpm doctor --fix)"
		return []Result{res}
	}

	if res.Status == "" {
		res.Status = Pass
		if res.Detail == "" {
			res.Detail = "profile files and includeIf rules consistent"
		}
	}
	return []Result{res}
}

func checkMappings(cfg *config.Config) []Result {
	mappings := dirs.All(cfg)
	if len(mappings) == 0 {
		return []Result{{Name: "Directory mappings", Status: Pass, Detail: "none configured",
			Hint: "optional: gpm dir add <path> <username>"}}
	}
	var res Result
	res.Name = "Directory mappings"
	seen := map[string]bool{}
	for _, m := range mappings {
		if seen[m.Path] {
			res.Status = Fail
			res.Detail += fmt.Sprintf("duplicate mapping %s; ", m.Path)
			res.Hint = "run: gpm dir remove <path>"
			continue
		}
		seen[m.Path] = true
		if _, err := os.Stat(m.Path); err != nil {
			if res.Status != Fail {
				res.Status = Warn
			}
			res.Detail += fmt.Sprintf("%s does not exist; ", dirs.Shorten(m.Path))
		}
	}
	if res.Status == "" {
		res.Status = Pass
		res.Detail = fmt.Sprintf("%d mapping(s) valid", len(mappings))
	}
	return []Result{res}
}

func checkRepoIdentity(cfg *config.Config, dir string) Result {
	if !gitcmd.InsideWorkTree(dir) {
		return Result{Name: "Repo identity", Status: Pass, Detail: "not a git repository"}
	}
	name, how, err := resolve.Profile(cfg, dir)
	if err != nil {
		return Result{Name: "Repo identity", Status: Fail, Detail: err.Error()}
	}
	if name == "" {
		return Result{Name: "Repo identity", Status: Pass, Detail: "no profile mapped to this directory",
			Hint: "optional: gpm dir add . <username>"}
	}
	p := cfg.GitProfiles[name]
	email, err := gitcmd.ConfigValue(dir, "user.email")
	if err != nil {
		return Result{Name: "Repo identity", Status: Fail, Detail: err.Error()}
	}
	switch {
	case email == "":
		return Result{Name: "Repo identity", Status: Warn,
			Detail: fmt.Sprintf("profile %q (%s) applies but the repo has no identity yet", name, how),
			Hint:   fmt.Sprintf("run: gpm apply %s", name)}
	case email == p.Email:
		return Result{Name: "Repo identity", Status: Pass, Detail: fmt.Sprintf("%s via %s", name, how)}
	default:
		return Result{Name: "Repo identity", Status: Warn,
			Detail: fmt.Sprintf("repo identity %q does not match profile %q (%s)", email, name, how),
			Hint:   fmt.Sprintf("run: gpm apply %s", name)}
	}
}

func checkNetwork(cfg *config.Config) []Result {
	var results []Result
	for _, name := range sshprofile.SortedUsernames(cfg) {
		p := cfg.SSHProfiles[name]
		if p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
			continue
		}
		for _, host := range p.Providers {
			res, err := auth.Test("git@"+host, p.KeyPath)
			if err != nil {
				results = append(results, Result{Name: fmt.Sprintf("SSH auth (%s → %s)", name, host), Status: Fail,
					Detail: err.Error(), Hint: "add the public key: gpm key copy " + name})
				continue
			}
			results = append(results, Result{Name: fmt.Sprintf("SSH auth (%s → %s)", name, host), Status: Pass,
				Detail: "authenticated as " + res.Username})
		}
	}
	if len(results) == 0 {
		results = append(results, Result{Name: "SSH auth", Status: Warn,
			Detail: "no keys to test", Hint: "generate one: gpm key generate <name>"})
	}
	return results
}
