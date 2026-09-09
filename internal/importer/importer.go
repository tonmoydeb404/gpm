// Package importer detects existing Git/SSH multi-account setups and
// proposes gpm profiles for them. Detection reads ~/.ssh/config (with
// Include directives, verified via `ssh -G`) and walks the global
// gitconfig's include/includeIf chain (config-only, no repo walk).
package importer

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/managed"
	"github.com/tonmoydeb/gpm/internal/sshkey"
)

// Candidate is a proposed profile pair derived from the existing
// setup. Importing a candidate creates an SSH profile from the key,
// host alias, and provider hosts and — when the email is known — a
// Git profile with the same username. HostAlias=="" denotes the global
// SSH identity (no Host stanza alias).
type Candidate struct {
	Username    string   // proposed username (ssh + git) — config-based (git user.name)
	Email       string   // git user.email (may be empty)
	KeyPath     string   // expanded absolute path to the private key
	HostAlias   string   // ssh config alias of the source stanza ("" = global)
	Hosts       []string // provider hostnames from the Host stanzas
	Directories []string // directories mapped via includeIf
	Fingerprint string   // SHA256 fingerprint of the key ("" when unknown)
	RepoCount   int      // reserved (always 0 in config-only mode)
}

// Detection is everything import found on the machine.
type Detection struct {
	Candidates []Candidate
	Warnings   []string
}

var invalidNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// Detect scans ~/.ssh/config and the global gitconfig (config-only).
// See Scan for the full report.
func Detect(existing *config.Config) (*Detection, error) {
	r := Scan(existing, ScanOptions{})
	return &Detection{Candidates: r.Candidates, Warnings: r.Warnings}, nil
}

// isGlobalAlias reports whether the SSH alias should be treated as the
// global identity. Bare provider hostnames (e.g. "github.com") with no
// alias suffix are the user's global SSH identity.
func isGlobalAlias(alias, hostName string) bool {
	if alias == "" || hostName == "" {
		return false
	}
	return strings.EqualFold(alias, hostName)
}

// detectSSH extracts Host stanzas with identity files from
// ~/.ssh/config (following Include directives) as SSH (and, by name,
// Git) profile candidates. Effective key paths and hostnames are
// resolved with `ssh -G` when available. Bare provider hosts become
// global candidates (HostAlias="").
func detectSSH() ([]Candidate, []string, error) {
	path, err := config.SSHConfigPath()
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	stanzas := loadSSHStanzas(path, &warnings)

	var (
		cands       []Candidate
		seenName    = map[string]int{}
		seenGlobal  bool
	)
	for _, s := range stanzas {
		key := s.firstIdentityFile()
		if key == "" {
			continue
		}
		alias := s.primaryHost()
		if alias == "" {
			continue
		}
		hostName := strings.ToLower(s.get("hostname"))
		if hostName == "" {
			hostName = strings.ToLower(alias)
		}

		// Refine with what ssh itself would resolve for this alias
		// (Match blocks, token expansion, Include'd files). `ssh -G`
		// may not see the stanza at all — e.g. in sandbox mode its
		// Include lines point outside it — so it only refines the
		// parsed values when it clearly resolved them: an identity
		// file that exists, or a hostname that differs from the
		// bare alias (the unresolved default).
		if effKey, effHost, ok := resolveEffectiveSSH(path, alias); ok {
			if effKey != "" && sshkey.Exists(mustExpand(effKey)) {
				key = effKey
			}
			if effHost != "" && effHost != strings.ToLower(alias) {
				hostName = effHost
			}
		}

		isGlobal := isGlobalAlias(alias, hostName)
		if isGlobal {
			if seenGlobal {
				warnings = append(warnings, fmt.Sprintf("host %s: second global alias, treating as scoped %q", alias, alias))
				isGlobal = false
			} else {
				seenGlobal = true
			}
		}

		// Config-based username: will be replaced from gitconfig's
		// user.name in Scan/applyGitNames; for now use sanitized alias
		// as placeholder (empty for global — filled later).
		var name string
		if isGlobal {
			name = "" // placeholder, resolved from git config
		} else {
			name = sanitizeName(alias)
			if name == "" {
				warnings = append(warnings, fmt.Sprintf("host %s: unusable alias, skipped", alias))
				continue
			}
			if seenName[name] > 0 {
				name = fmt.Sprintf("%s-%d", name, seenName[name]+1)
			}
			seenName[name]++
		}

		keyPath, err := config.ExpandPath(key)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("host %s: bad key path %q, skipped", alias, key))
			continue
		}
		if !sshkey.Exists(keyPath) {
			warnings = append(warnings, fmt.Sprintf("host %s: key %s does not exist, skipped", alias, key))
			continue
		}
		c := Candidate{
			Username:  name,
			KeyPath:   keyPath,
			HostAlias: alias,
			Hosts:     []string{hostName},
		}
		if isGlobal {
			c.HostAlias = "" // global marker
		}
		if fp, err := sshkey.Fingerprint(sshkey.PubPath(keyPath)); err == nil {
			c.Fingerprint = fp
		}
		cands = append(cands, c)
	}
	return cands, warnings, nil
}

// mustExpand expands a key path, returning it unchanged on error.
func mustExpand(p string) string {
	abs, err := config.ExpandPath(p)
	if err != nil {
		return p
	}
	return abs
}

// sanitizeName turns an SSH alias into a valid profile name.
func sanitizeName(alias string) string {
	name := invalidNameChars.ReplaceAllString(strings.TrimSpace(alias), "-")
	name = strings.Trim(name, "-._")
	if name == "" {
		return ""
	}
	if name[0] < 'A' || (name[0] > 'Z' && name[0] < 'a') || name[0] > 'z' {
		if name[0] < '0' || name[0] > '9' {
			name = "gpm-" + name
		}
	}
	if err := config.ValidateName(name); err != nil {
		return ""
	}
	return name
}

// --- minimal ssh config parser -------------------------------------

type stanza struct {
	hosts   []string
	options map[string][]string // lower-case keys, repeated values kept
}

func (s *stanza) get(key string) string {
	if v, ok := s.options[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

func (s *stanza) firstIdentityFile() string {
	for _, v := range s.options["identityfile"] {
		return v
	}
	return ""
}

// primaryHost returns the first concrete (non-wildcard) host pattern.
func (s *stanza) primaryHost() string {
	for _, h := range s.hosts {
		if !strings.ContainsAny(h, "*?!") {
			return h
		}
	}
	return ""
}

// parseSSHConfig parses enough of the ssh_config format to find Host
// stanzas and the options gpm cares about. Handles both `Key Value`
// and `Key=Value` forms as well as quoted values containing spaces.
func parseSSHConfig(content string) []stanza {
	var (
		stanzas []stanza
		cur     *stanza
	)
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		tokens := tokenizeKeepQuotes(line)
		if len(tokens) == 0 {
			continue
		}

		key := tokens[0]
		var vals []string
		if idx := strings.Index(key, "="); idx > 0 && len(tokens) == 1 {
			vals = tokenizeKeepQuotes(key[idx+1:])
			key = key[:idx]
		} else {
			vals = tokens[1:]
			if len(vals) > 0 && vals[0] == "=" {
				vals = vals[1:]
			}
		}

		if strings.EqualFold(key, "Host") {
			patterns := make([]string, 0, len(vals))
			for _, v := range vals {
				if p := stripOuterQuotes(v); p != "" {
					patterns = append(patterns, p)
				}
			}
			if len(patterns) == 0 {
				continue
			}
			stanzas = append(stanzas, stanza{hosts: patterns, options: map[string][]string{}})
			cur = &stanzas[len(stanzas)-1]
			continue
		}
		if cur == nil || len(vals) == 0 {
			continue
		}
		k := strings.ToLower(key)
		cur.options[k] = append(cur.options[k], stripOuterQuotes(vals[0]))
	}
	return stanzas
}

// tokenizeKeepQuotes splits on whitespace outside quotes; quoted
// spans stay single tokens and keep their quote characters.
func tokenizeKeepQuotes(line string) []string {
	var (
		tokens []string
		cur    strings.Builder
		inQ    bool
	)
	for _, r := range line {
		switch {
		case r == '"':
			inQ = !inQ
			cur.WriteRune(r)
		case (r == ' ' || r == '\t') && !inQ:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// stripOuterQuotes removes one pair of surrounding double quotes.
func stripOuterQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// stripManaged removes the gpm-managed block so re-imports of an
// already-managed config do not produce duplicate candidates.
func stripManaged(content string) string {
	begin := strings.Index(content, managed.Begin)
	if begin == -1 {
		return content
	}
	end := strings.Index(content[begin:], managed.End)
	if end == -1 {
		return content
	}
	end = begin + end + len(managed.End)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	return content[:begin] + content[end:]
}
