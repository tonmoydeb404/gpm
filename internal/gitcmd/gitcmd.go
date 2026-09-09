// Package gitcmd wraps the git commands gpm shells out to. Shelling
// out keeps gpm compatible with every git version and respects the
// user's environment.
package gitcmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepo is returned when the directory is not inside a git work tree.
var ErrNotRepo = errors.New("not inside a git work tree")

// ErrKeyUnset is returned when a config key is not defined.
var ErrKeyUnset = errors.New("config key not defined")

// Available reports whether a git binary can be found.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// sandboxEnv returns the environment for git child processes. In
// sandbox mode (GPM_HOME) git must read the sandbox's global config,
// not the real one.
func sandboxEnv() []string {
	dev := os.Getenv("GPM_HOME")
	if dev == "" {
		return nil
	}
	return append(os.Environ(), "GIT_CONFIG_GLOBAL="+filepath.Join(dev, ".gitconfig"))
}

// Run executes git in dir and returns trimmed stdout.
func Run(dir string, args ...string) (string, error) {
	if !Available() {
		return "", fmt.Errorf("git binary not found in PATH")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env := sandboxEnv(); env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// TopLevel returns the work tree root for dir.
func TopLevel(dir string) (string, error) {
	out, err := Run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotRepo, err)
	}
	return out, nil
}

// InsideWorkTree reports whether dir is inside a git work tree.
func InsideWorkTree(dir string) bool {
	_, err := TopLevel(dir)
	return err == nil
}

// runWithStatus executes git and maps "key unset" (exit 1) to
// ErrKeyUnset so callers can distinguish it from real failures.
func runWithStatus(dir string, args ...string) (string, error) {
	if !Available() {
		return "", fmt.Errorf("git binary not found in PATH")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if env := sandboxEnv(); env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 && stderr.Len() == 0 {
			return "", ErrKeyUnset
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// ConfigValue returns the effective value of key in dir ("" when unset).
func ConfigValue(dir, key string) (string, error) {
	out, err := runWithStatus(dir, "config", "--get", key)
	if errors.Is(err, ErrKeyUnset) {
		return "", nil
	}
	return out, err
}

// ConfigOriginValue returns the effective value of key in dir plus
// the config file it came from, resolved to an absolute path.
// origin is empty when the key is unset.
func ConfigOriginValue(dir, key string) (value, origin string, err error) {
	out, err := runWithStatus(dir, "config", "--show-origin", "--get", key)
	if errors.Is(err, ErrKeyUnset) {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(out, "\t", 2)
	if len(parts) != 2 {
		return "", "", nil
	}
	origin = strings.TrimSpace(parts[0])
	value = strings.TrimSpace(parts[1])
	origin = strings.TrimPrefix(origin, "file:")
	if origin != "" && !filepath.IsAbs(origin) {
		origin = filepath.Join(dir, origin)
	}
	return value, origin, nil
}

// GlobalConfigRegexp returns key/value pairs from the user's global
// config whose keys match the Go-flavoured pattern.
func GlobalConfigRegexp(dir, pattern string) ([][2]string, error) {
	out, err := runWithStatus(dir, "config", "--global", "--get-regexp", pattern)
	if errors.Is(err, ErrKeyUnset) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var pairs [][2]string
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		pairs = append(pairs, [2]string{parts[0], strings.TrimSpace(parts[1])})
	}
	return pairs, nil
}

// SetIdentity writes repo-local user.name and user.email.
func SetIdentity(dir, name, email string) error {
	if _, err := Run(dir, "config", "user.name", name); err != nil {
		return err
	}
	if _, err := Run(dir, "config", "user.email", email); err != nil {
		return err
	}
	return nil
}

// UnsetIdentity removes repo-local user.name and user.email.
func UnsetIdentity(dir string) error {
	for _, key := range []string{"user.name", "user.email"} {
		if _, err := runWithStatus(dir, "config", "--unset", key); err != nil && !errors.Is(err, ErrKeyUnset) {
			return err
		}
	}
	return nil
}

// RepoLocalConfig returns the repo-local (not global) value of key.
// Missing values are returned as "".
func RepoLocalConfig(dir, key string) (string, error) {
	out, err := runWithStatus(dir, "config", "--local", "--get", key)
	if errors.Is(err, ErrKeyUnset) {
		return "", nil
	}
	return out, err
}

// GetRepoLocalIdentity reads repo-local (not global) user.name/email.
// Missing values are returned as "".
func GetRepoLocalIdentity(dir string) (name, email string, err error) {
	name, err = runWithStatus(dir, "config", "--local", "--get", "user.name")
	if errors.Is(err, ErrKeyUnset) {
		name = ""
	} else if err != nil {
		return "", "", err
	}
	email, err = runWithStatus(dir, "config", "--local", "--get", "user.email")
	if errors.Is(err, ErrKeyUnset) {
		email = ""
	} else if err != nil {
		return "", "", err
	}
	return name, email, nil
}
