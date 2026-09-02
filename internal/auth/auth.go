// Package auth tests SSH authentication against providers. GitHub is
// asked for a shell we are never granted; its rejection message is
// the greeting that identifies which account the key belongs to.
package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Result captures the outcome of an auth test.
type Result struct {
	OK       bool
	Username string
	Output   string // raw ssh output for diagnostics
}

// greetingRe matches GitHub's "Hi <username>!" greeting.
var greetingRe = regexp.MustCompile(`Hi ([^!\s]+)!`)

// ParseGreeting extracts the authenticated username from a GitHub SSH
// greeting such as "Hi octocat! You've successfully authenticated...".
func ParseGreeting(out string) string {
	m := greetingRe.FindStringSubmatch(out)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

// runSSH executes ssh and reports stderr plus the exit code. GitHub
// exits 1 on a successful auth test (no shell is granted), so exit
// codes are data, not errors. Overridable in tests.
var runSSH = func(args []string) (stderr string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return out.String(), exitErr.ExitCode(), nil
	}
	if runErr != nil {
		return out.String(), -1, runErr
	}
	return out.String(), 0, nil
}

// TestGitHub authenticates to GitHub over SSH using keyPath and
// reports which account the key belongs to. Shelling out to the user's
// real ssh client reuses their known_hosts, agent, and proxy setup.
func TestGitHub(keyPath string) (Result, error) {
	return Test("git@github.com", keyPath)
}

// Test authenticates to host (e.g. git@github.com) using keyPath.
func Test(host, keyPath string) (Result, error) {
	args := []string{
		"-T",
		"-o", "IdentitiesOnly=yes",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
	}
	// Sandbox mode: keep ssh entirely inside GPM_HOME so the real
	// known_hosts and ssh config are never read or written.
	if dev := os.Getenv("GPM_HOME"); dev != "" {
		args = append(args,
			"-F", filepath.Join(dev, ".ssh", "config"),
			"-o", "UserKnownHostsFile="+filepath.Join(dev, ".ssh", "known_hosts"),
		)
	}
	args = append(args, "-i", keyPath, host)

	stderr, code, err := runSSH(args)
	if err != nil {
		return Result{Output: stderr}, fmt.Errorf("run ssh: %w", err)
	}
	if user := ParseGreeting(stderr); user != "" {
		return Result{OK: true, Username: user, Output: stderr}, nil
	}
	return Result{Output: stderr}, fmt.Errorf("authentication rejected (exit %d): %s", code, firstLine(stderr))
}

// firstLine returns the first non-empty line of s, trimmed.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return "(no output)"
}
