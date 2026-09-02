package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sandboxArgs isolates every command run in a test: the fake HOME
// keeps unit tests hermetic, GPM_HOME activates the sandbox code path
// end-to-end (config dir, ssh dir, gitconfig, git child env).
func sandboxArgs(t *testing.T) string {
	t.Helper()
	dev := filepath.Join(t.TempDir(), "sandbox")
	t.Setenv("HOME", dev)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GPM_HOME", dev)
	return dev
}

// runRoot executes the CLI in-process, as cobra intends commands to
// be tested, and returns what the command wrote to stdout.
func runRoot(t *testing.T, args ...string) string {
	t.Helper()

	// Commands print with fmt, so capture process stdout.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	stdout := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		stdout <- buf.String()
	}()

	defer func() { os.Stdout = old }()
	root := NewRootCmd()
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		w.Close()
		os.Stdout = old
		t.Fatalf("gpm %s: %v", strings.Join(args, " "), err)
	}
	w.Close()
	os.Stdout = old
	return <-stdout
}

func TestCLISandboxFlow(t *testing.T) {
	dev := sandboxArgs(t)

	out := runRoot(t, "profile", "add", "jane", "--email", "jane@corp.com")
	if !strings.Contains(out, `Git identity "jane" created`) {
		t.Errorf("add output missing creation message:\n%s", out)
	}

	out = runRoot(t, "ssh", "add", "work", "--provider", "github.com", "--alias", "gpm-work")
	if !strings.Contains(out, `SSH profile "work" created`) {
		t.Errorf("ssh add output:\n%s", out)
	}

	out = runRoot(t, "profile", "list")
	if !strings.Contains(out, "jane") || !strings.Contains(out, "jane@corp.com") {
		t.Errorf("list output missing profile:\n%s", out)
	}

	out = runRoot(t, "ssh", "list")
	if !strings.Contains(out, "gpm-work") || !strings.Contains(out, "work") {
		t.Errorf("ssh list output missing the alias:\n%s", out)
	}

	out = runRoot(t, "key", "show", "work")
	if !strings.HasPrefix(out, "ssh-ed25519 ") {
		t.Errorf("key show output = %q, want authorized key", out)
	}

	out = runRoot(t, "key", "list")
	if !strings.Contains(out, "ok") {
		t.Errorf("key list should report the key as ok:\n%s", out)
	}

	// Mapping a directory must produce the includeIf block.
	out = runRoot(t, "dir", "add", filepath.Join(dev, "works", "corp"), "jane")
	if !strings.Contains(out, `Mapped ~/works/corp to "jane"`) {
		t.Errorf("dir add output:\n%s", out)
	}
	gitconfigData, err := os.ReadFile(filepath.Join(dev, ".gitconfig"))
	if err != nil {
		t.Fatalf("sandbox .gitconfig missing after dir add: %v", err)
	}
	if !strings.Contains(string(gitconfigData), "includeIf") {
		t.Errorf("sandbox .gitconfig missing includeIf rules:\n%s", gitconfigData)
	}

	// All state must live inside the sandbox.
	for _, p := range []string{
		filepath.Join(dev, ".ssh", "id_ed25519_work"),
		filepath.Join(dev, ".ssh", "config"),
		filepath.Join(dev, ".config", "gpm", "config.toml"),
	} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected sandbox artifact %s: %v", p, err)
		}
	}
}

func TestCLISandboxCurrentAndDoctor(t *testing.T) {
	dev := sandboxArgs(t)

	runRoot(t, "profile", "add", "jane", "--email", "jane@corp.com")
	runRoot(t, "ssh", "add", "work", "--provider", "github.com", "--alias", "gpm-work")

	out := runRoot(t, "current", "--dir", filepath.Join(dev, "anywhere"), "--no-check")
	if !strings.Contains(out, "jane") || !strings.Contains(out, "global profile") {
		t.Errorf("current should fall back to the global profile:\n%s", out)
	}

	// Healthy sandbox: doctor must pass without warnings that would
	// trigger its os.Exit path.
	out = runRoot(t, "doctor", "--dir", filepath.Join(dev, "anywhere"))
	if !strings.Contains(out, "0 failures") {
		t.Errorf("doctor should be clean on a fresh profile:\n%s", out)
	}
}
