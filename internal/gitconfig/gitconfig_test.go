package gitconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonmoydeb404/gpm/internal/config"
)

func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("GPM_HOME", "")
	return dir
}

func sampleConfig() *config.Config {
	cfg := config.New()
	cfg.GitProfiles["work"] = config.GitProfile{Username: "work", Email: "jane@corp.com"}
	cfg.GitProfiles["oss"] = config.GitProfile{Username: "oss", Email: "jane@oss.dev"}
	if _, err := AddDir(cfg, "oss", "~/Works/oss"); err != nil {
		panic(err)
	}
	if _, err := AddDir(cfg, "work", "~/Works/corp"); err != nil {
		panic(err)
	}
	return cfg
}

// AddDir is a tiny test helper mapping a directory onto a profile.
func AddDir(cfg *config.Config, username, path string) (string, error) {
	abs, err := config.ExpandPath(path)
	if err != nil {
		return "", err
	}
	p := cfg.GitProfiles[username]
	p.Directories = append(p.Directories, abs)
	cfg.GitProfiles[username] = p
	return abs, nil
}

func TestSyncFromConfig(t *testing.T) {
	testHome(t)
	writeConfig(t, mustGlobal(t), "[alias]\n\tco = checkout\n")

	cfg := sampleConfig()
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatalf("SyncFromConfig() error = %v", err)
	}

	// Per-profile files: the username is the commit author name.
	for name, want := range map[string]string{
		"work": "[user]\n\tname = work\n\temail = jane@corp.com\n",
		"oss":  "[user]\n\tname = oss\n\temail = jane@oss.dev\n",
	} {
		path, err := ProfilePath(name)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read profile gitconfig %s: %v", name, err)
		}
		if string(data) != want {
			t.Errorf("profile %s content = %q, want %q", name, data, want)
		}
	}

	// Global gitconfig: user content first, managed block appended.
	data, err := os.ReadFile(mustGlobal(t))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.HasPrefix(content, "[alias]\n\tco = checkout\n") {
		t.Errorf("user gitconfig content not preserved:\n%s", content)
	}
	if !strings.Contains(content, `[includeIf "gitdir:`) {
		t.Errorf("includeIf rules missing:\n%s", content)
	}
	if strings.Count(content, "includeIf") != 2 {
		t.Errorf("expected 2 includeIf rules:\n%s", content)
	}
	if !strings.Contains(content, filepath.Join(mustGitconfigDir(t), "work")) {
		t.Errorf("include path does not point at the profile file:\n%s", content)
	}
}

func TestSyncFromConfigIsIdempotent(t *testing.T) {
	testHome(t)
	cfg := sampleConfig()
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	global := mustGlobal(t)
	first, _ := os.ReadFile(global)
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(global)
	if string(first) != string(second) {
		t.Errorf("second sync changed the global gitconfig:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestSyncFromConfigRemovesOrphans(t *testing.T) {
	testHome(t)
	cfg := config.New()
	cfg.GitProfiles["work"] = config.GitProfile{Username: "work", Email: "j@c.com"}
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(mustGitconfigDir(t), "deleted-profile")
	if err := os.WriteFile(stale, []byte("[user]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("orphaned profile file still exists")
	}
}

func TestIncludeBlockEmpty(t *testing.T) {
	testHome(t)
	block, err := IncludeBlock(config.New())
	if err != nil {
		t.Fatal(err)
	}
	if block != "" {
		t.Errorf("IncludeBlock() = %q, want empty", block)
	}
}

func TestProfileForGitconfigFile(t *testing.T) {
	testHome(t)
	dir := mustGitconfigDir(t)
	if name, ok := ProfileForGitconfigFile(filepath.Join(dir, "work")); !ok || name != "work" {
		t.Errorf("ProfileForGitconfigFile(profile file) = %q, %v", name, ok)
	}
	if _, ok := ProfileForGitconfigFile(filepath.Join(t.TempDir(), "work")); ok {
		t.Error("files outside the gpm dir must not map to profiles")
	}
}

func TestGlobalGitconfigPermsPreserved(t *testing.T) {
	testHome(t)
	global := mustGlobal(t)
	writeConfig(t, global, "")
	if err := os.Chmod(global, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := sampleConfig()
	if err := SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(global)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("global gitconfig perms = %o, want 644 (preserved)", perm)
	}
}

func mustGlobal(t *testing.T) string {
	t.Helper()
	p, err := GlobalPath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustGitconfigDir(t *testing.T) string {
	t.Helper()
	d, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
