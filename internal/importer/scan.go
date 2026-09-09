package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/dirs"
	"github.com/tonmoydeb404/gpm/internal/gitcmd"
	"github.com/tonmoydeb404/gpm/internal/gitprofile"
	"github.com/tonmoydeb404/gpm/internal/sshprofile"
)

// Report is everything a scan found on the machine (config-only).
type Report struct {
	Candidates []Candidate
	Warnings   []string
	Duration   time.Duration
}

// ScanOptions is reserved for future scan tuning. Currently unused
// (repo walk removed — scanner is config-only).
type ScanOptions struct{}

// dirMapping is an includeIf gitdir condition paired with the identity
// read from its target file (config-based username priority).
type dirMapping struct {
	dir   string
	name  string
	email string
}

// maxIncludeDepth caps the include/includeIf chain length so a
// self-referencing gitconfig cannot hang the scan.
const maxIncludeDepth = 10

// Scan inspects the machine's existing Git/SSH setup and returns a
// full report: ssh-config candidates enriched with gitconfig emails
// and includeIf directory mappings. Scan never writes anything and
// never walks repositories — it is config-only.
func Scan(existing *config.Config, opts ScanOptions) *Report {
	start := time.Now()
	_ = opts
	r := &Report{}

	cands, warnings, err := detectSSH()
	if err != nil {
		r.Warnings = append(r.Warnings, err.Error())
	}
	r.Candidates = cands
	r.Warnings = append(r.Warnings, warnings...)

	// Candidates matching already-imported profiles inherit their
	// identities so includeIf mappings can be adopted on a re-scan.
	if existing != nil {
		for i := range r.Candidates {
			if p, ok := existing.GitProfiles[r.Candidates[i].Username]; ok {
				if r.Candidates[i].Email == "" {
					r.Candidates[i].Email = p.Email
				}
				_ = p
			}
		}
	}

	// Global identity: even with multiple candidates, the global
	// SSH alias (HostAlias=="") should get the global git identity.
	if home, err := config.Home(); err == nil {
		globalEmail, _ := gitcmd.ConfigValue(home, "user.email")
		globalName, _ := gitcmd.ConfigValue(home, "user.name")
		for i := range r.Candidates {
			if r.Candidates[i].HostAlias != "" {
				continue
			}
			if r.Candidates[i].Email == "" && globalEmail != "" {
				r.Candidates[i].Email = globalEmail
			}
			if (r.Candidates[i].Username == "" || r.Candidates[i].Username == sanitizeName(r.Candidates[i].HostAlias)) && globalName != "" {
				if err := config.ValidateName(globalName); err == nil {
					r.Candidates[i].Username = globalName
				}
			}
			// Fill global username even if candidate already has email but
			// username still empty (e.g. bare github.com placeholder).
			if r.Candidates[i].Username == "" && globalName != "" {
				if err := config.ValidateName(globalName); err == nil {
					r.Candidates[i].Username = globalName
				}
			}
		}
		// Single non-global candidate fallback (original behavior).
		if len(r.Candidates) == 1 && r.Candidates[0].Email == "" && globalEmail != "" {
			r.Candidates[0].Email = globalEmail
			if (r.Candidates[0].Username == "" || r.Candidates[0].Username == sanitizeName(r.Candidates[0].HostAlias)) && globalName != "" {
				if err := config.ValidateName(globalName); err == nil {
					r.Candidates[0].Username = globalName
				}
			}
		}
	}

	mappings := scanGitConfig(&r.Warnings)

	// Pair scoped SSH candidates (HostAlias != "") that still lack an
	// email with git includeIf mappings using alias↔name heuristic.
	// Config-based priority: git user.name becomes Username.
	pairScopedCandidates(r.Candidates, mappings)

	// Config-based username: adopt git user.name for candidates whose
	// email now matches a mapping.
	applyGitNames(r.Candidates, mappings)

	attachMappings(r.Candidates, mappings)
	tidyDirectories(r.Candidates)

	// Global username fallback when global file lacked includeIf but
	// gitconfig still provides the name (e.g. live system global).
	if home, err := config.Home(); err == nil {
		if name, _ := gitcmd.ConfigValue(home, "user.name"); name != "" {
			if err := config.ValidateName(name); err == nil {
				for i, c := range r.Candidates {
					if c.HostAlias == "" && c.Username == "" {
						r.Candidates[i].Username = name
					}
				}
			}
		}
	}

	sort.Slice(r.Candidates, func(i, j int) bool { return r.Candidates[i].Username < r.Candidates[j].Username })
	r.Duration = time.Since(start)
	return r
}

// pairScopedCandidates assigns email+name to scoped candidates that
// still lack an email by matching alias suffix to git name/email local
// part. This bridges SSH-only and Git-only identities without a repo walk.
func pairScopedCandidates(cands []Candidate, mappings []dirMapping) {
	// Track which mapping emails are already claimed by a candidate.
	claimed := map[string]bool{}
	for _, c := range cands {
		if c.Email != "" {
			claimed[strings.ToLower(c.Email)] = true
		}
	}
	for i := range cands {
		if cands[i].HostAlias == "" || cands[i].Email != "" {
			continue
		}
		suffix := aliasSuffix(cands[i].HostAlias)
		best := -1
		bestScore := -1
		for j, m := range mappings {
			if m.email == "" || claimed[strings.ToLower(m.email)] {
				continue
			}
			score := aliasNameScore(suffix, m.name, m.email)
			if score > bestScore {
				bestScore = score
				best = j
			}
		}
		if best >= 0 && bestScore > 0 {
			m := mappings[best]
			if err := config.ValidateName(m.name); err == nil && m.name != "" {
				cands[i].Username = m.name
			}
			cands[i].Email = m.email
			claimed[strings.ToLower(m.email)] = true
		}
	}
}

// aliasSuffix extracts a heuristic token from an alias like
// "github.com-work" → "work", "gh-rajib" → "rajib".
func aliasSuffix(alias string) string {
	alias = strings.ToLower(alias)
	// Take after last of - . _ /
	idx := -1
	for _, sep := range []string{"-", ".", "_", "/"} {
		if p := strings.LastIndex(alias, sep); p > idx {
			idx = p
		}
	}
	if idx >= 0 && idx+1 < len(alias) {
		return alias[idx+1:]
	}
	return alias
}

// aliasNameScore returns a pairing score between an alias suffix and
// a git name/email. Higher is better; 0 means no match.
func aliasNameScore(suffix, gitName, email string) int {
	suffix = strings.ToLower(suffix)
	if suffix == "" {
		return 0
	}
	nameLower := strings.ToLower(gitName)
	emailLocal := strings.ToLower(email)
	if at := strings.Index(emailLocal, "@"); at != -1 {
		emailLocal = emailLocal[:at]
	}
	// Exact suffix == name token or email local part is strong.
	if nameLower == suffix || emailLocal == suffix {
		return 10
	}
	if strings.Contains(nameLower, suffix) || strings.Contains(emailLocal, suffix) {
		return 5
	}
	if strings.Contains(suffix, nameLower) && len(nameLower) >= 3 {
		return 3
	}
	return 0
}

// applyGitNames sets candidate.Username from the gitconfig target's
// user.name when the candidate's email matches that mapping's email.
// Config-based priority: git user.name wins over sanitized alias.
func applyGitNames(cands []Candidate, mappings []dirMapping) {
	for _, m := range mappings {
		if m.name == "" || m.email == "" {
			continue
		}
		if err := config.ValidateName(m.name); err != nil {
			continue
		}
		for i, c := range cands {
			if !strings.EqualFold(c.Email, m.email) {
				continue
			}
			if cands[i].Username == sanitizeName(cands[i].HostAlias) || cands[i].Username == "" {
				cands[i].Username = m.name
			}
			break
		}
	}
}

// Fresh returns the candidates whose ssh profile does not exist yet.
func (r *Report) Fresh(existing *config.Config) []Candidate {
	var fresh []Candidate
	for _, c := range r.Candidates {
		if _, exists := existing.SSHProfiles[c.Username]; exists {
			continue
		}
		fresh = append(fresh, c)
	}
	return fresh
}

// Apply imports one candidate into cfg: the SSH profile and — when
// the email is known — the Git profile with the candidate's
// directories. It returns a human-readable note ("" when nothing
// special happened) that callers may surface.
func Apply(cfg *config.Config, c Candidate) (note string, err error) {
	if _, err := sshprofile.Add(cfg, config.SSHProfile{
		Username:  c.Username,
		KeyPath:   c.KeyPath,
		HostAlias: c.HostAlias,
		Providers: c.Hosts,
	}, sshprofile.AddOpts{}); err != nil {
		return "", fmt.Errorf("ssh profile: %w", err)
	}
	if c.Email == "" {
		return fmt.Sprintf("no email known: only the ssh side of %q was imported; add it later with gpm profile add", c.Username), nil
	}
	directory := ""
	if len(c.Directories) > 0 {
		directory = c.Directories[0]
	}
	if _, err := gitprofile.Add(cfg, config.GitProfile{
		Username: c.Username,
		Email:    c.Email,
	}, directory); err != nil {
		return "", fmt.Errorf("git profile: %w", err)
	}
	if len(c.Directories) <= 1 {
		return "", nil
	}
	for _, d := range c.Directories[1:] {
		if _, err := dirs.Add(cfg, d, c.Username, false); err != nil {
			return "", fmt.Errorf("directory %s: %w", d, err)
		}
	}
	return "", nil
}

// attachMappings adds the directory of every mapping to the first
// candidate whose email matches, skipping duplicates. If candidate
// has no email yet but mapping is unambiguous (global), it can adopt.
func attachMappings(cands []Candidate, mappings []dirMapping) {
	for _, m := range mappings {
		for i, c := range cands {
			if c.Email == "" || !strings.EqualFold(c.Email, m.email) {
				continue
			}
			if !containsString(cands[i].Directories, m.dir) {
				cands[i].Directories = append(cands[i].Directories, m.dir)
			}
			break
		}
	}
}

// tidyDirectories drops candidate directories that another directory
// of the same candidate already covers, keeping the coarser mapping.
func tidyDirectories(cands []Candidate) {
	for i := range cands {
		dirs := cands[i].Directories
		kept := dirs[:0]
		for _, d := range dirs {
			covered := false
			for _, p := range dirs {
				if p != d && pathContains(p, d) {
					covered = true
					break
				}
			}
			if !covered {
				kept = append(kept, d)
			}
		}
		cands[i].Directories = kept
	}
}

// pathContains reports whether child is dir or a descendant of dir,
// respecting path boundaries.
func pathContains(dir, child string) bool {
	dir = filepath.Clean(dir)
	child = filepath.Clean(child)
	if dir == child {
		return true
	}
	return strings.HasPrefix(child, dir+string(filepath.Separator))
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// --- gitconfig walk ---------------------------------------------------

// scanGitConfig walks ~/.gitconfig and ~/.config/git/config (git's
// two global roots) following include and includeIf directives
// recursively, and returns the directory mappings it can derive.
func scanGitConfig(warnings *[]string) []dirMapping {
	var mappings []dirMapping
	seen := map[string]bool{}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		if path == "" || depth > maxIncludeDepth || seen[path] {
			return
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				*warnings = append(*warnings, fmt.Sprintf("read %s: %v", path, err))
			}
			return
		}
		for _, v := range parseGitConfigLines(string(data)) {
			switch {
			case v.section == "include" && v.key == "path":
				if target, ok := resolveIncludeTarget(path, v.value, warnings); ok {
					walk(target, depth+1)
				}
			case v.section == "includeif" && v.key == "path":
				dir, ok := gitdirDir(v.subsection)
				if !ok {
					*warnings = append(*warnings, fmt.Sprintf("includeIf %q: unsupported condition, skipped", v.subsection))
					continue
				}
				target, ok := resolveIncludeTarget(path, v.value, warnings)
				if !ok {
					continue
				}
				name, email := fileIdentity(target, map[string]bool{}, 0)
				if email == "" {
					*warnings = append(*warnings, fmt.Sprintf("includeIf %s: could not read an email from %s, skipped", dir, target))
					continue
				}
				mappings = append(mappings, dirMapping{dir: dir, name: name, email: email})
			}
		}
	}
	home, err := config.Home()
	if err != nil {
		return nil
	}
	walk(filepath.Join(home, ".gitconfig"), 0)
	walk(filepath.Join(home, ".config", "git", "config"), 0)
	return mappings
}

// gitVar is one config variable with its section context.
type gitVar struct {
	section    string // lower-case section name (e.g. "includeif")
	subsection string // raw subsection, if any (e.g. `gitdir:~/works/`)
	key        string // lower-case key
	value      string // unquoted value; "true" for bare boolean keys
}

// parseGitConfigLines parses enough of the gitconfig format to find
// include directives and [user] identities. It handles section and
// subsection headers, quoted values, escapes, and inline comments.
func parseGitConfigLines(content string) []gitVar {
	var (
		vars       []gitVar
		section    string
		subsection string
	)
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			end := strings.Index(line, "]")
			if end == -1 {
				continue
			}
			header := line[1:end]
			section = header
			subsection = ""
			if idx := strings.IndexAny(header, " \t"); idx != -1 {
				section = header[:idx]
				rest := strings.TrimSpace(header[idx:])
				subsection = strings.Trim(rest, "\"")
				subsection = strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(subsection)
			}
			section = strings.ToLower(section)
			continue
		}

		key, value := line, "true"
		if eq := strings.Index(line, "="); eq != -1 {
			key = strings.TrimSpace(line[:eq])
			value = stripGitValue(line[eq+1:])
		} else {
			key = stripGitValue(key)
		}
		if key == "" {
			continue
		}
		vars = append(vars, gitVar{
			section:    section,
			subsection: subsection,
			key:        strings.ToLower(key),
			value:      value,
		})
	}
	return vars
}

// stripGitValue trims whitespace, inline comments, and one level of
// surrounding quotes from a config value. Escape sequences are only
// processed inside double quotes — git treats backslashes outside
// quotes as literal characters, which matters for Windows paths
// (C:\Users\...) written unquoted.
func stripGitValue(v string) string {
	v = strings.TrimSpace(v)
	var (
		b   strings.Builder
		inQ bool
		end = len(v)
		i   = 0
	)
	for i < end {
		ch := v[i]
		switch {
		case ch == '\\' && inQ && i+1 < end:
			switch v[i+1] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'b':
				b.WriteByte('\b')
			default:
				b.WriteByte(v[i+1]) // \\ and \" and friends
			}
			i += 2
			continue
		case ch == '"':
			inQ = !inQ
			i++
			continue
		case (ch == '#' || ch == ';') && !inQ:
			return strings.TrimSpace(b.String())
		}
		b.WriteByte(ch)
		i++
	}
	return strings.TrimSpace(b.String())
}

// resolveIncludeTarget resolves an include path relative to the
// including file (git semantics), expanding ~ as needed.
func resolveIncludeTarget(includingFile, value string, warnings *[]string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	abs, err := config.ExpandPath(value)
	if err != nil {
		return "", false
	}
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(includingFile), abs)
	}
	abs = filepath.Clean(abs)
	if abs == includingFile {
		*warnings = append(*warnings, fmt.Sprintf("include in %s points at itself, skipped", includingFile))
		return "", false
	}
	return abs, true
}

// gitdirDir extracts the directory from an includeIf subsection like
// `gitdir:~/works/corp/` or `gitdir/i:~/works/corp/**`. Only absolute
// and ~-relative conditions are supported (git's `./` form depends on
// the config file location and is rare).
func gitdirDir(cond string) (string, bool) {
	lower := strings.ToLower(cond)
	var pattern string
	switch {
	case strings.HasPrefix(lower, "gitdir:"):
		pattern = cond[len("gitdir:"):]
	case strings.HasPrefix(lower, "gitdir/i:"):
		pattern = cond[len("gitdir/i:"):]
	default:
		return "", false
	}
	pattern = strings.TrimSuffix(strings.TrimSpace(pattern), "**")
	pattern = strings.TrimSuffix(pattern, "/")
	if pattern == "" {
		return "", false
	}
	if !strings.HasPrefix(pattern, "~/") && !filepath.IsAbs(pattern) {
		return "", false
	}
	abs, err := config.ExpandPath(pattern)
	if err != nil {
		return "", false
	}
	return filepath.Clean(abs), true
}

// fileIdentity returns the effective [user] name and email of a
// gitconfig file, following its include.path chain. Later values win,
// mirroring git. Config-based priority: name is primary.
func fileIdentity(path string, seen map[string]bool, depth int) (string, string) {
	if path == "" || depth > maxIncludeDepth || seen[path] {
		return "", ""
	}
	seen[path] = true
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	name, email := "", ""
	for _, v := range parseGitConfigLines(string(data)) {
		switch {
		case v.section == "include" && v.key == "path":
			if target, ok := resolveIncludeTarget(path, v.value, &[]string{}); ok {
				if n, e := fileIdentity(target, seen, depth+1); e != "" || n != "" {
					if e != "" {
						email = e
					}
					if n != "" {
						name = n
					}
				}
			}
		case v.section == "user" && v.key == "name":
			name = v.value
		case v.section == "user" && v.key == "email":
			email = v.value
		}
	}
	return name, email
}

// fileEmail is kept for tests — delegates to fileIdentity.
func fileEmail(path string, seen map[string]bool, depth int) string {
	_, e := fileIdentity(path, seen, depth)
	return e
}
