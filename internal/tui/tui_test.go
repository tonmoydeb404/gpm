package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/gitprofile"
	"github.com/tonmoydeb404/gpm/internal/sshprofile"
	"github.com/tonmoydeb404/gpm/internal/sync"
)

// sandbox isolates every TUI test the same way the CLI tests do:
// a fake HOME plus GPM_HOME so all state lands under the sandbox.
func sandbox(t *testing.T) (string, *config.Config) {
	t.Helper()
	dev := filepath.Join(t.TempDir(), "sandbox")
	if err := os.MkdirAll(dev, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", dev)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GPM_HOME", dev)
	return dev, config.New()
}

// seedSSH creates a real ssh profile with a generated key pair.
func seedSSH(t *testing.T, cfg *config.Config, username string) {
	t.Helper()
	if _, err := sshprofile.Add(cfg, config.SSHProfile{
		Username:  username,
		Providers: []string{"github.com"},
	}, sshprofile.AddOpts{GenerateKey: true}); err != nil {
		t.Fatalf("seed ssh profile %s: %v", username, err)
	}
	if err := sync.All(cfg); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
}

// seedGit creates a real git profile mapped to a directory; an empty
// dir seeds the global identity.
func seedGit(t *testing.T, cfg *config.Config, username, email, dir string) {
	t.Helper()
	if _, err := gitprofile.Add(cfg, config.GitProfile{
		Username: username,
		Email:    email,
	}, dir); err != nil {
		t.Fatalf("seed git profile %s: %v", username, err)
	}
	if err := sync.All(cfg); err != nil {
		t.Fatalf("seed sync: %v", err)
	}
}

func startProgram(t *testing.T, cfg *config.Config) *teatest.TestModel {
	t.Helper()
	return teatest.NewTestModel(t, NewModel(cfg), teatest.WithInitialTermSize(120, 40))
}

// waitTimeout is how long waitOutput / waitAll poll for content.
const waitTimeout = 5 * time.Second

// waitOutput blocks until substr appears in the program output. It
// polls the output pipe directly instead of teatest.WaitFor, whose
// accumulator misbehaves across consecutive waits. Note that bubbletea
// only writes diffs, so each wait must target content that appears
// strictly after whatever earlier waits already consumed.
func waitOutput(t *testing.T, tm *teatest.TestModel, substr string) {
	t.Helper()
	waitFor(t, tm, func(s string) bool { return strings.Contains(s, substr) }, substr)
}

// waitAll blocks until every string in want appears in the output. Use
// it to assert several substrings that render in the same frame.
func waitAll(t *testing.T, tm *teatest.TestModel, want ...string) {
	t.Helper()
	set := make(map[string]bool, len(want))
	waitFor(t, tm, func(s string) bool {
		for _, w := range want {
			if !set[w] {
				set[w] = strings.Contains(s, w)
			}
		}
		for _, w := range want {
			if !set[w] {
				return false
			}
		}
		return true
	}, strings.Join(want, ", "))
}

// waitFor polls the model output until pred matches the accumulated
// bytes or the deadline expires.
func waitFor(t *testing.T, tm *teatest.TestModel, pred func(string) bool, desc string) {
	t.Helper()
	var buf bytes.Buffer
	tmp := make([]byte, 64*1024)
	deadline := time.Now().Add(waitTimeout)
	for {
		n, err := tm.Output().Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
			if pred(buf.String()) {
				return
			}
		}
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("read output: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q; last output:\n%s", desc, buf.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// keyDelay paces input like a human typist; huh drops keystrokes
// that arrive faster than it processes field updates.
const keyDelay = 60 * time.Millisecond

func press(tm *teatest.TestModel, key tea.KeyType) {
	tm.Send(tea.KeyMsg{Type: key})
	time.Sleep(keyDelay)
}

func fieldType(tm *teatest.TestModel, s string) {
	tm.Type(s)
	time.Sleep(keyDelay)
}

// menuDown moves the menu cursor down n times; the leading settle
// gives the program time to attach the next screen before the first
// key lands.
func menuDown(tm *teatest.TestModel, n int) {
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < n; i++ {
		press(tm, tea.KeyDown)
	}
}

func TestTUIHomeMenu(t *testing.T) {
	sandbox(t)
	tm := startProgram(t, config.New())
	defer tm.Quit()

	waitAll(t, tm, "GPM", "SSH Profiles", "Git Profiles", "Create", "Run Doctor", "Exit")
}

func TestTUISSHCreateFlow(t *testing.T) {
	_, cfg := sandbox(t)

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter) // SSH Profiles

	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEnter) // Create New

	waitOutput(t, tm, "Username")
	fieldType(tm, "work")
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // provider: keep the github.com default
	press(tm, tea.KeyEnter) // host alias: empty → global identity

	waitAll(t, tm, "SSH profile created", "ssh-ed25519",
		"Add this public key to your provider", "Copy Public Key", "Test Connection")

	p, ok := cfg.SSHProfiles["work"]
	if !ok {
		t.Fatalf("ssh profile work not in config; have %v", cfg.SSHProfiles)
	}
	if len(p.Providers) != 1 || p.Providers[0] != "github.com" {
		t.Errorf("providers = %v, want [github.com]", p.Providers)
	}
	if p.HostAlias != "" {
		t.Errorf("host alias = %q, want empty (global)", p.HostAlias)
	}
	if _, err := os.Stat(p.KeyPath); err != nil {
		t.Errorf("key pair not generated: %v", err)
	}
}

func TestTUISSHCreateWithAlias(t *testing.T) {
	_, cfg := sandbox(t)

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter) // SSH Profiles

	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEnter) // Create New

	waitOutput(t, tm, "Username")
	fieldType(tm, "work")
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // provider: keep the github.com default
	fieldType(tm, "gpm-work")
	press(tm, tea.KeyEnter) // host alias

	waitOutput(t, tm, "SSH profile created")
	if got := cfg.SSHProfiles["work"].HostAlias; got != "gpm-work" {
		t.Errorf("host alias = %q, want gpm-work", got)
	}
}

func TestTUISSHUpdateAlias(t *testing.T) {
	_, cfg := sandbox(t)
	seedSSH(t, cfg, "work")

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "work")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // open the work profile
	waitOutput(t, tm, "SSH Profile: work")
	press(tm, tea.KeyEnter) // Update

	waitOutput(t, tm, "Host alias")
	fieldType(tm, "gpm-work")
	press(tm, tea.KeyEnter)

	waitOutput(t, tm, "profile updated")
	if got := cfg.SSHProfiles["work"].HostAlias; got != "gpm-work" {
		t.Errorf("host alias = %q, want gpm-work", got)
	}
}

func TestTUISSHGlobalConstraintViaTUI(t *testing.T) {
	sandbox(t)
	cfg := config.New()
	seedSSH(t, cfg, "free") // no alias: the global ssh profile

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEnter) // Create New

	waitOutput(t, tm, "Username")
	fieldType(tm, "second")
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // provider: keep default
	press(tm, tea.KeyEnter) // host alias empty → refused inline

	waitOutput(t, tm, "only one profile can be global")
	if _, exists := cfg.SSHProfiles["second"]; exists {
		t.Error("the form must not create a second global ssh profile")
	}
}

func TestTUISSHConstraintKeepsFormOpen(t *testing.T) {
	sandbox(t)
	cfg := config.New()
	seedSSH(t, cfg, "free") // no alias: the global ssh profile

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEnter) // Create New

	waitOutput(t, tm, "Username")
	fieldType(tm, "second")
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // provider: keep default
	press(tm, tea.KeyEnter) // host alias empty → apply fails

	// The error must appear together with the still-open form: the
	// user is not kicked back to the list. (The preserved username is
	// verified behaviorally below — retyping only the alias and
	// resubmitting creates the profile.)
	waitAll(t, tm, "only one profile can be global", "Username", "Host alias")

	if _, exists := cfg.SSHProfiles["second"]; exists {
		t.Error("the failed submit must not create a profile")
	}

	// Fixing the input on the same form must succeed.
	fieldType(tm, "gpm-second")
	press(tm, tea.KeyEnter)

	waitOutput(t, tm, "SSH profile created")
	if p, ok := cfg.SSHProfiles["second"]; !ok || p.HostAlias != "gpm-second" {
		t.Errorf("profile after fix = %+v", p)
	}
}

func TestTUIGitCreateFlow(t *testing.T) {
	sandbox(t)
	cfg := config.New()

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "Git Profiles")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // Git Profiles

	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEnter) // Create New

	waitOutput(t, tm, "GitHub username")
	fieldType(tm, "octocat")
	press(tm, tea.KeyEnter)
	fieldType(tm, "octo@corp.dev")
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // directory: empty → global identity

	waitAll(t, tm, "Git profile created", "octocat <octo@corp.dev>", "global identity")

	p, ok := cfg.GitProfiles["octocat"]
	if !ok {
		t.Fatalf("git profile octocat not in config; have %v", cfg.GitProfiles)
	}
	if p.Email != "octo@corp.dev" {
		t.Errorf("email = %q", p.Email)
	}
	if len(p.Directories) != 0 {
		t.Errorf("directories = %v, want none (global)", p.Directories)
	}
}

func TestTUIBothCreateFlow(t *testing.T) {
	dev, cfg := sandbox(t)

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "Create")
	menuDown(tm, 2)
	press(tm, tea.KeyEnter) // Create

	waitOutput(t, tm, "Both Profile")
	press(tm, tea.KeyEnter) // Both Profile

	waitOutput(t, tm, "GitHub username")
	fieldType(tm, "tonny")
	press(tm, tea.KeyEnter)
	fieldType(tm, "tonny@corp.dev")
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // ssh provider: keep default
	press(tm, tea.KeyEnter) // host alias: empty → global key
	fieldType(tm, filepath.Join(dev, "works", "tonny"))
	press(tm, tea.KeyEnter)

	waitAll(t, tm, "Profile created successfully", "Git identity: tonny <tonny@corp.dev>",
		"SSH profile: tonny", "ssh-ed25519")

	if _, ok := cfg.GitProfiles["tonny"]; !ok {
		t.Fatalf("git profile tonny missing: %+v", cfg.GitProfiles)
	}
	ssh, ok := cfg.SSHProfiles["tonny"]
	if !ok {
		t.Fatalf("ssh profile tonny missing: %+v", cfg.SSHProfiles)
	}
	if _, err := os.Stat(ssh.KeyPath); err != nil {
		t.Errorf("combined flow must generate a key: %v", err)
	}
}

func TestTUIGlobalConstraintViaTUI(t *testing.T) {
	sandbox(t)
	cfg := config.New()
	seedGit(t, cfg, "octocat", "octo@corp.dev", "") // octocat is the global identity

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "Git Profiles")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEnter)

	waitOutput(t, tm, "GitHub username")
	fieldType(tm, "second")
	press(tm, tea.KeyEnter)
	fieldType(tm, "second@corp.dev")
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // directory empty → must be refused inline

	waitOutput(t, tm, "only one profile can be global")
	if _, exists := cfg.GitProfiles["second"]; exists {
		t.Error("the form must not create a second global profile")
	}
}

func TestTUIEscBackAndQuit(t *testing.T) {
	sandbox(t)
	cfg := config.New()
	seedSSH(t, cfg, "work")

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter) // into SSH Profiles
	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEnter) // into the create form
	waitOutput(t, tm, "Username")
	press(tm, tea.KeyEscape) // back out of the form
	waitOutput(t, tm, "Create New")
	press(tm, tea.KeyEscape) // back to home
	waitOutput(t, tm, "Run Doctor")

	if _, exists := cfg.SSHProfiles["tui"]; exists {
		t.Error("aborted form must not create a profile")
	}
}

func TestTUIEscAtHomeQuits(t *testing.T) {
	sandbox(t)
	tm := startProgram(t, config.New())

	waitOutput(t, tm, "Run Doctor")
	press(tm, tea.KeyEscape)
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}

func TestTUISSHDeleteFlow(t *testing.T) {
	dev, cfg := sandbox(t)
	seedSSH(t, cfg, "work")

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "work")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // open the work profile
	waitOutput(t, tm, "SSH Profile: work")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // Delete

	waitOutput(t, tm, `Delete SSH profile "work"?`)
	press(tm, tea.KeyRight) // Yes
	press(tm, tea.KeyEnter)
	press(tm, tea.KeyEnter) // keep the key pair (No)

	waitOutput(t, tm, "ssh profile deleted")

	if _, exists := cfg.SSHProfiles["work"]; exists {
		t.Error("profile should be deleted from config")
	}
	if _, err := os.Stat(filepath.Join(dev, ".ssh", "id_ed25519_work")); err != nil {
		t.Errorf("key must be kept when remove-key is No: %v", err)
	}
}

func TestTUIProvidersAddRemove(t *testing.T) {
	sandbox(t)
	cfg := config.New()
	seedSSH(t, cfg, "work")

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "work")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // open the work profile
	waitOutput(t, tm, "SSH Profile: work")
	menuDown(tm, 5)
	press(tm, tea.KeyEnter) // Providers

	waitOutput(t, tm, "Add Provider")
	press(tm, tea.KeyEnter) // Add Provider

	waitOutput(t, tm, "Provider domain")
	fieldType(tm, "gitlab.com")
	press(tm, tea.KeyEnter)

	waitOutput(t, tm, "gitlab.com")
	if got := cfg.SSHProfiles["work"].Providers; len(got) != 2 {
		t.Fatalf("providers = %v, want github.com + gitlab.com", got)
	}

	// Remove github.com: select it on the rebuilt list and confirm.
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // github.com
	waitOutput(t, tm, `Remove github.com from "work"?`)
	press(tm, tea.KeyRight) // Yes
	press(tm, tea.KeyEnter)

	waitOutput(t, tm, "provider removed")
	if got := cfg.SSHProfiles["work"].Providers; len(got) != 1 || got[0] != "gitlab.com" {
		t.Errorf("providers = %v, want [gitlab.com]", got)
	}
}

func TestTUIPublicKeyScreen(t *testing.T) {
	sandbox(t)
	cfg := config.New()
	seedSSH(t, cfg, "work")

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "SSH Profiles")
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "work")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "View Public Key")
	menuDown(tm, 2)
	press(tm, tea.KeyEnter)

	waitAll(t, tm, "Public Key: work", "ssh-ed25519", "Fingerprint: SHA256:")
	press(tm, tea.KeyEscape)
	waitOutput(t, tm, "Test Connection")
}

func TestTUIDoctorViaMenu(t *testing.T) {
	sandbox(t)
	cfg := config.New()
	seedSSH(t, cfg, "work")
	seedGit(t, cfg, "octo", "octo@corp.dev", "~/dir-octo")

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "Run Doctor")
	menuDown(tm, 3)
	press(tm, tea.KeyEnter) // Run Doctor

	waitAll(t, tm, "Doctor", "pass")

	press(tm, tea.KeyEscape) // back home
	waitOutput(t, tm, "Exit")
}

func TestTUIDirectoriesMenu(t *testing.T) {
	dev, cfg := sandbox(t)
	seedSSH(t, cfg, "work")
	seedGit(t, cfg, "octo", "octo@corp.dev", "~/dir-octo")

	tm := startProgram(t, cfg)
	defer tm.Quit()

	waitOutput(t, tm, "Git Profiles")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // Git Profiles

	waitOutput(t, tm, "octo <octo@corp.dev>")
	menuDown(tm, 1)
	press(tm, tea.KeyEnter) // open the profile
	waitOutput(t, tm, "Git Profile:")
	menuDown(tm, 2)
	press(tm, tea.KeyEnter) // Directories

	waitOutput(t, tm, "Add Directory")
	press(tm, tea.KeyEnter)
	waitOutput(t, tm, "Directory")
	fieldType(tm, filepath.Join(dev, "someproj"))
	press(tm, tea.KeyEnter)

	waitOutput(t, tm, "directory mapped")
	if got := cfg.GitProfiles["octo"].Directories; len(got) != 2 {
		t.Fatalf("directories = %v, want the seeded one plus the new one", got)
	}
}

func TestTUIExitViaMenu(t *testing.T) {
	sandbox(t)
	tm := startProgram(t, config.New())

	waitOutput(t, tm, "Exit")
	menuDown(tm, 5)
	press(tm, tea.KeyEnter) // Exit
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
}
