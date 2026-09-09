package importer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tonmoydeb404/gpm/internal/config"
)

// maxIncludeDepthSSH caps ssh config Include chains.
const maxIncludeDepthSSH = 8

// loadSSHStanzas reads path, splices Include'd files in at their
// include point, strips the gpm-managed block, and parses the result.
func loadSSHStanzas(path string, warnings *[]string) []stanza {
	content := readSSHWithIncludes(path, map[string]bool{}, 0, warnings)
	return parseSSHConfig(stripManaged(content))
}

// readSSHWithIncludes reads an ssh config file and concatenates the
// content of Include'd files where the directive appears, mirroring
// OpenSSH's first-value-wins ordering. Relative include paths resolve
// against the directory of the including file; missing files are
// silently skipped, cycles and runaway depth are guarded.
func readSSHWithIncludes(path string, seen map[string]bool, depth int, warnings *[]string) string {
	if path == "" || depth > maxIncludeDepthSSH || seen[path] {
		return ""
	}
	seen[path] = true
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			*warnings = append(*warnings, fmt.Sprintf("read %s: %v", path, err))
		}
		return ""
	}
	var b strings.Builder
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if !strings.HasPrefix(line, "#") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && strings.EqualFold(fields[0], "include") {
				for _, pattern := range fields[1:] {
					for _, inc := range globInclude(path, pattern) {
						b.WriteString(readSSHWithIncludes(inc, seen, depth+1, warnings))
						b.WriteString("\n")
					}
				}
				continue
			}
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return b.String()
}

// globInclude expands one Include pattern. Absolute and ~ paths are
// used as-is; relative patterns resolve against the including file's
// directory (which for ~/.ssh/config is ~/.ssh, matching OpenSSH).
func globInclude(includingFile, pattern string) []string {
	abs, err := config.ExpandPath(pattern)
	if err != nil {
		return nil
	}
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(filepath.Dir(includingFile), abs)
	}
	matches, err := filepath.Glob(filepath.Clean(abs))
	if err != nil || len(matches) == 0 {
		return nil
	}
	sort.Strings(matches)
	return matches
}

// resolveEffectiveSSH asks `ssh -G -F <config> <alias>` for the
// effective identityfile and hostname of a stanza, which resolves
// Include directives, Match blocks, and token expansion the way ssh
// itself would. ok is false when ssh is unavailable or fails.
func resolveEffectiveSSH(cfgPath, alias string) (keyPath, hostname string, ok bool) {
	cmd := exec.Command("ssh", "-G", "-F", cfgPath, "--", alias)
	out, err := cmd.Output()
	if err != nil {
		return "", "", false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "identityfile":
			if keyPath == "" {
				keyPath = fields[1]
			}
		case "hostname":
			if hostname == "" {
				hostname = strings.ToLower(fields[1])
			}
		}
	}
	return keyPath, hostname, keyPath != "" || hostname != ""
}
