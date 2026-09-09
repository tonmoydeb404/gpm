package sshconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/perm"
)

func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("GPM_HOME", "")
	return dir
}

func sshConfigPath(t *testing.T) string {
	t.Helper()
	p, err := config.SSHConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUpdateCreatesFile(t *testing.T) {
	testHome(t)
	path := sshConfigPath(t)
	hosts := []Host{{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/id_ed25519_work"}}
	if err := Update(path, hosts); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "Host gpm-work") ||
		!strings.Contains(content, "HostName github.com") ||
		!strings.Contains(content, "IdentityFile ~/.ssh/id_ed25519_work") ||
		!strings.Contains(content, "IdentitiesOnly yes") {
		t.Errorf("managed block incomplete:\n%s", content)
	}
	if !strings.HasPrefix(content, BeginMarker) {
		t.Errorf("new file should start with the managed block:\n%s", content)
	}
	if ok, cur := perm.Private(path); !ok {
		t.Errorf("ssh config perms = %s, want private", cur)
	}
}

const userContent = `Host bastion
  HostName bastion.example.com
  User admin

Host *
  AddKeysToAgent yes
`

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUpdateAppendsAndPreservesUserContent(t *testing.T) {
	testHome(t)
	path := sshConfigPath(t)
	writeConfig(t, path, userContent)

	hosts := []Host{{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/id_ed25519_work"}}
	if err := Update(path, hosts); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)

	if !strings.HasPrefix(content, userContent) {
		t.Errorf("user content not preserved verbatim:\n%s", content)
	}
	if !strings.Contains(content, "Host gpm-work") {
		t.Errorf("managed block missing:\n%s", content)
	}
}

func TestUpdateReplacesOnlyManagedBlock(t *testing.T) {
	testHome(t)
	path := sshConfigPath(t)
	writeConfig(t, path, userContent)

	first := []Host{{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/old"}}
	if err := Update(path, first); err != nil {
		t.Fatal(err)
	}
	second := []Host{
		{Alias: "gpm-oss", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/id_ed25519_oss"},
		{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/id_ed25519_work"},
	}
	if err := Update(path, second); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.HasPrefix(content, userContent) {
		t.Errorf("user content not preserved across update:\n%s", content)
	}
	if strings.Contains(content, "old") {
		t.Errorf("stale managed entry survived:\n%s", content)
	}
	if strings.Count(content, BeginMarker) != 1 {
		t.Errorf("expected exactly one managed block:\n%s", content)
	}
	// Deterministic order: aliases sorted.
	if strings.Index(content, "Host gpm-oss") > strings.Index(content, "Host gpm-work") {
		t.Errorf("hosts not sorted by alias:\n%s", content)
	}
}

func TestUpdateRemovesManagedBlock(t *testing.T) {
	testHome(t)
	path := sshConfigPath(t)
	hosts := []Host{{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/k"}}
	if err := Update(path, hosts); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if strings.Contains(content, BeginMarker) || strings.Contains(content, "gpm-work") {
		t.Errorf("managed block not removed:\n%s", content)
	}
}

func TestUpdateRemovesBlockKeepsUserContent(t *testing.T) {
	testHome(t)
	path := sshConfigPath(t)
	writeConfig(t, path, userContent)
	hosts := []Host{{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/k"}}
	if err := Update(path, hosts); err != nil {
		t.Fatal(err)
	}
	if err := Update(path, nil); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != userContent {
		t.Errorf("user content damaged after block removal:\n%q", data)
	}
}

func TestSyncFromConfig(t *testing.T) {
	testHome(t)
	cfg := config.New()
	cfg.SSHProfiles["work"] = config.SSHProfile{
		Username:  "work",
		HostAlias: "gpm-work",
		KeyPath:   "~/.ssh/id_ed25519_work",
		Providers: []string{"github.com"},
	}
	cfg.SSHProfiles["incomplete"] = config.SSHProfile{Username: "incomplete", HostAlias: "gpm-inc", Providers: []string{"github.com"}}
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatalf("SyncFromConfig() error = %v", err)
	}
	path := sshConfigPath(t)
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.Contains(content, "Host gpm-work") {
		t.Errorf("gpm-work host missing:\n%s", content)
	}
	if strings.Contains(content, "incomplete") {
		t.Errorf("profile without key must not appear:\n%s", content)
	}
}

func TestSyncFromConfigGlobalProfileHasNoStanza(t *testing.T) {
	testHome(t)
	cfg := config.New()
	cfg.SSHProfiles["global"] = config.SSHProfile{
		Username:  "global",
		KeyPath:   "~/.ssh/id_ed25519_global",
		Providers: []string{"github.com"},
	}
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(sshConfigPath(t))
	if strings.Contains(string(data), "Host ") {
		t.Errorf("global profile (empty alias) must not produce stanzas:\n%s", data)
	}
}

func TestSyncFromConfigMultiProvider(t *testing.T) {
	testHome(t)
	cfg := config.New()
	cfg.SSHProfiles["company"] = config.SSHProfile{
		Username:  "company",
		HostAlias: "corp",
		KeyPath:   "~/.ssh/id_ed25519_company",
		Providers: []string{"gitlab.com", "github.com"},
	}
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(sshConfigPath(t))
	content := string(data)
	// The first provider (sorted) carries the plain alias; the rest are
	// suffixed with the host.
	for _, alias := range []string{
		"Host corp",
		"Host corp-github-com",
		"HostName github.com",
		"HostName gitlab.com",
	} {
		if !strings.Contains(content, alias) {
			t.Errorf("missing %q in managed block:\n%s", alias, content)
		}
	}
	if strings.Contains(content, "Host corp-gitlab-com") {
		t.Errorf("only the first provider should be suffixed:\n%s", content)
	}
}

func TestUpdateLeavesUnbalancedMarkersAlone(t *testing.T) {
	testHome(t)
	path := sshConfigPath(t)
	corrupt := userContent + BeginMarker + "\nHost gpm-x\n" // no end marker
	writeConfig(t, path, corrupt)
	if err := Update(path, []Host{{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/k"}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != corrupt {
		t.Errorf("file with unbalanced markers must not be modified:\n%q", data)
	}
}

func TestBackupCreatedBeforeUpdate(t *testing.T) {
	testHome(t)
	path := sshConfigPath(t)
	writeConfig(t, path, userContent)
	if err := Update(path, []Host{{Alias: "gpm-work", HostName: "github.com", User: "git", IdentityFile: "~/.ssh/k"}}); err != nil {
		t.Fatal(err)
	}
	dir, err := config.Dir()
	if err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "backups", "config.*"))
	if len(matches) != 1 {
		t.Errorf("expected 1 backup, got %d", len(matches))
	}
}
