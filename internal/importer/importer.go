// Package importer detects existing Git/SSH multi-account setups and
// proposes gpm profiles for them. Detection reads ~/.ssh/config Host
// stanzas and the global gitconfig (identity + includeIf rules) by
// shelling out to git.
package importer

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/gitcmd"
	"github.com/tonmoydeb/gpm/internal/managed"
	"github.com/tonmoydeb/gpm/internal/sshkey"
)

// Candidate is a proposed profile pair derived from the existing
// setup. Importing a candidate creates an SSH profile from the key,
// host alias, and provider hosts and — when the email is known — a
// Git profile with the same username.
type Candidate struct {
	Username    string   // proposed username (ssh + git)
	Email       string   // git user.email (may be empty)
	KeyPath     string   // expanded absolute path to the private key
	HostAlias   string   // ssh config alias of the source stanza
	Hosts       []string // provider hostnames from the Host stanzas
	Directories []string // includeIf directories whose email matches
}

// Detection is everything import found on the machine.
type Detection struct {
	Candidates []Candidate
	Warnings   []string
}

var invalidNameChars = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// Detect scans ~/.ssh/config and the global gitconfig. When existing
// is non-nil, candidates matching already-imported profiles inherit
// their emails so includeIf mappings can be adopted on a second run.
func Detect(existing *config.Config) (*Detection, error) {
	d := &Detection{}
	cands, warnings, err := detectSSH()
	if err != nil {
		return nil, err
	}
	d.Candidates = cands
	d.Warnings = append(d.Warnings, warnings...)

	if existing != nil {
		for i := range d.Candidates {
			if p, ok := existing.GitProfiles[d.Candidates[i].Username]; ok {
				if d.Candidates[i].Email == "" {
					d.Candidates[i].Email = p.Email
				}
			}
		}
	}

	// Attach the global git email when it is unambiguous.
	if len(d.Candidates) == 1 {
		home, err := config.Home()
		if err == nil {
			if email, _ := gitcmd.ConfigValue(home, "user.email"); email != "" {
				d.Candidates[0].Email = email
			}
		}
	}

	d.Warnings = detectIncludes(d.Candidates, d.Warnings)

	sort.Slice(d.Candidates, func(i, j int) bool { return d.Candidates[i].Username < d.Candidates[j].Username })
	return d, nil
}

// detectSSH extracts Host stanzas with identity files from
// ~/.ssh/config as SSH (and, by name, Git) profile candidates.
func detectSSH() ([]Candidate, []string, error) {
	path, err := config.SSHConfigPath()
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}
	stanzas := parseSSHConfig(stripManaged(string(data)))

	var (
		cands    []Candidate
		warnings []string
		seenName = map[string]int{}
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

		name := sanitizeName(alias)
		if name == "" {
			warnings = append(warnings, fmt.Sprintf("host %s: unusable alias, skipped", alias))
			continue
		}
		if seenName[name] > 0 {
			name = fmt.Sprintf("%s-%d", name, seenName[name]+1)
		}
		seenName[name]++

		keyPath, err := config.ExpandPath(key)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("host %s: bad key path %q, skipped", alias, key))
			continue
		}
		if !sshkey.Exists(keyPath) {
			warnings = append(warnings, fmt.Sprintf("host %s: key %s does not exist, skipped", alias, key))
			continue
		}
		cands = append(cands, Candidate{
			Username:  name,
			KeyPath:   keyPath,
			HostAlias: alias,
			Hosts:     []string{hostName},
		})
	}
	return cands, warnings, nil
}

// detectIncludes converts existing includeIf gitdir rules into
// directories on the candidate whose email matches the included file.
func detectIncludes(cands []Candidate, warnings []string) []string {
	home, err := config.Home()
	if err != nil {
		return warnings
	}
	rules, err := gitcmd.GlobalConfigRegexp(home, `^includeif\..*\.path$`)
	if err != nil || len(rules) == 0 {
		return warnings
	}
	for _, kv := range rules {
		key, value := kv[0], kv[1]
		gitdir, ok := extractGitdir(key)
		if !ok {
			continue
		}
		email := readEmailFromFile(value)
		if email == "" {
			warnings = append(warnings, fmt.Sprintf("includeIf %s: could not read an email from %s, skipped", gitdir, value))
			continue
		}
		matched := -1
		for i, c := range cands {
			if c.Email != "" && strings.EqualFold(c.Email, email) {
				matched = i
				break
			}
		}
		if matched == -1 {
			warnings = append(warnings, fmt.Sprintf("includeIf %s: email %s does not match any imported host, skipped", gitdir, email))
			continue
		}
		dir, err := config.ExpandPath(gitdir)
		if err != nil {
			continue
		}
		cands[matched].Directories = append(cands[matched].Directories, dir)
	}
	return warnings
}

// extractGitdir pulls the directory out of an includeIf config key
// like `includeif.gitdir:~/works/corp/.path`.
func extractGitdir(key string) (string, bool) {
	key = strings.ToLower(key)
	const prefix = "includeif.gitdir:"
	const suffix = ".path"
	if !strings.HasPrefix(key, prefix) || !strings.HasSuffix(key, suffix) {
		return "", false
	}
	dir := key[len(prefix) : len(key)-len(suffix)]
	dir = strings.TrimSuffix(dir, "/")
	if dir == "" {
		return "", false
	}
	return dir, true
}

// emailRe matches the email line of a gitconfig file.
var emailRe = regexp.MustCompile(`(?m)^\s*email\s*=\s*(.+?)\s*$`)

// readEmailFromFile reads the first `email = ...` line of a gitconfig
// file (used on includeIf targets, which may predate gpm).
func readEmailFromFile(path string) string {
	p, err := config.ExpandPath(path)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	m := emailRe.FindStringSubmatch(string(data))
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
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
