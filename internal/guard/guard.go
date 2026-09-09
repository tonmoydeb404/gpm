// Package guard installs a pre-commit hook that verifies the commit
// identity matches the profile mapped to the repository's directory.
// Unmapped directories are never blocked; gpm only guards what it
// manages.
package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tonmoydeb404/gpm/internal/gitcmd"
)

// hookMarker identifies hooks written by gpm.
const hookMarker = "# gpm pre-commit identity guard"

// hookScript fail-opens when gpm is missing so uninstalling gpm never
// bricks the user's repos.
const hookScript = `#!/bin/sh
` + hookMarker + `
command -v gpm >/dev/null 2>&1 || exit 0
exec gpm precommit
`

// hookPath returns the pre-commit hook path for the repo containing dir.
func hookPath(dir string) (string, error) {
	root, err := gitcmd.TopLevel(dir)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".git", "hooks", "pre-commit"), nil
}

// Install writes the gpm pre-commit hook into the repo containing
// dir. A foreign (non-gpm) hook is only replaced with force. Returns
// the hook path.
func Install(dir string, force bool) (string, error) {
	path, err := hookPath(dir)
	if err != nil {
		return "", err
	}
	if existing, err := os.ReadFile(path); err == nil {
		content := string(existing)
		if !strings.Contains(content, hookMarker) && !force {
			return "", fmt.Errorf("%s already exists and was not written by gpm (use --force to replace)", path)
		}
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create hooks dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(hookScript), 0o755); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o755); err != nil {
		return "", fmt.Errorf("chmod %s: %w", path, err)
	}
	return path, nil
}

// Remove deletes the gpm pre-commit hook if it exists. Foreign hooks
// are never touched.
func Remove(dir string) (bool, error) {
	path, err := hookPath(dir)
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	if !strings.Contains(string(existing), hookMarker) {
		return false, fmt.Errorf("%s was not written by gpm; remove it manually if desired", path)
	}
	if err := os.Remove(path); err != nil {
		return false, fmt.Errorf("remove %s: %w", path, err)
	}
	return true, nil
}

// IsInstalled reports whether the repo containing dir has the gpm hook.
func IsInstalled(dir string) (bool, error) {
	path, err := hookPath(dir)
	if err != nil {
		return false, err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	return strings.Contains(string(existing), hookMarker), nil
}
