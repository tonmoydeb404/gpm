package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonmoydeb404/gpm/internal/perm"
)

// testHome isolates the test from the real home directory.
func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("USERPROFILE", dir)
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("GPM_HOME", "")
	return dir
}

func TestLoadMissingReturnsEmpty(t *testing.T) {
	testHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.GitProfiles) != 0 || len(cfg.SSHProfiles) != 0 {
		t.Fatalf("expected empty profiles, got %d git / %d ssh", len(cfg.GitProfiles), len(cfg.SSHProfiles))
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	testHome(t)

	cfg := New()
	cfg.GitProfiles["janedoe"] = GitProfile{
		Username:    "janedoe",
		Email:       "jane@corp.com",
		Directories: []string{"/abs/Works/corp"},
	}
	cfg.SSHProfiles["work"] = SSHProfile{
		Username:  "work",
		HostAlias: "gpm-work",
		KeyPath:   "~/.ssh/id_ed25519_work",
		Providers: []string{"github.com"},
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	g := got.GitProfiles["janedoe"]
	if g.Email != "jane@corp.com" || len(g.Directories) != 1 || g.Directories[0] != "/abs/Works/corp" {
		t.Errorf("git profile roundtrip mismatch: %+v", g)
	}
	s := got.SSHProfiles["work"]
	if s.HostAlias != "gpm-work" || s.KeyPath != "~/.ssh/id_ed25519_work" ||
		len(s.Providers) != 1 || s.Providers[0] != "github.com" {
		t.Errorf("ssh profile roundtrip mismatch: %+v", s)
	}
}

func TestGlobalSSHProfile(t *testing.T) {
	cfg := New()
	if _, ok := cfg.GlobalSSHProfile(); ok {
		t.Error("empty config must have no global ssh profile")
	}

	cfg.SSHProfiles["a"] = SSHProfile{Username: "a", HostAlias: "gpm-a", Providers: []string{"github.com"}}
	cfg.SSHProfiles["g"] = SSHProfile{Username: "g", Providers: []string{"github.com"}}
	got, ok := cfg.GlobalSSHProfile()
	if !ok || got.Username != "g" {
		t.Errorf("GlobalSSHProfile() = %+v, %v; want g, true", got, ok)
	}
}

func TestLoadNormalizesUsernameFromMapKey(t *testing.T) {
	testHome(t)
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	// A config written before the username field existed stores the
	// identity only in the TOML table key.
	stale := `
[git_profiles."jane"]
email = 'jane@corp.com'

[ssh_profiles.work]
key_path = '~/.ssh/id_ed25519_work'
providers = ['github.com']
`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(stale), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if g := got.GitProfiles["jane"]; g.Username != "jane" {
		t.Errorf("git username = %q, want jane (from the table key)", g.Username)
	}
	if s := got.SSHProfiles["work"]; s.Username != "work" {
		t.Errorf("ssh username = %q, want work (from the table key)", s.Username)
	}
}

func TestGlobalGitProfile(t *testing.T) {
	cfg := New()
	if _, ok := cfg.GlobalGitProfile(); ok {
		t.Error("empty config must have no global profile")
	}

	cfg.GitProfiles["a"] = GitProfile{Username: "a", Email: "a@b.c", Directories: []string{"/x"}}
	cfg.GitProfiles["g"] = GitProfile{Username: "g", Email: "g@b.c"}
	got, ok := cfg.GlobalGitProfile()
	if !ok || got.Username != "g" {
		t.Errorf("GlobalGitProfile() = %+v, %v; want g, true", got, ok)
	}
}

func TestSavePermissions(t *testing.T) {
	testHome(t)
	cfg := New()
	cfg.GitProfiles["a"] = GitProfile{Username: "a", Email: "a@b.c"}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path, err := Path()
	if err != nil {
		t.Fatal(err)
	}
	if ok, cur := perm.Private(path); !ok {
		t.Errorf("config perms = %s, want private", cur)
	}
}

func TestBackupFileAndPrune(t *testing.T) {
	testHome(t)
	src := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(src, []byte("v1"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range 15 {
		if err := os.WriteFile(src, []byte(fmt.Sprintf("v%d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := BackupFile(src); err != nil {
			t.Fatalf("BackupFile() error = %v", err)
		}
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "backups", "config.*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != backupKeep {
		t.Errorf("backups = %d, want %d", len(matches), backupKeep)
	}
	// The newest backup must contain the most recent content.
	oldest := matches[len(matches)-1]
	data, err := os.ReadFile(oldest)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v14" {
		t.Errorf("newest backup content = %q, want %q", data, "v14")
	}
}

func TestHomeSandbox(t *testing.T) {
	home := testHome(t)
	dev := filepath.Join(t.TempDir(), "dev-sandbox")
	t.Setenv("GPM_HOME", dev)

	got, err := Home()
	if err != nil {
		t.Fatalf("Home() error = %v", err)
	}
	if got != filepath.Clean(dev) {
		t.Errorf("Home() = %q, want %q", got, dev)
	}
	dir, _ := Dir()
	if !strings.HasPrefix(dir, dev) {
		t.Errorf("Dir() = %q, want under %q", dir, dev)
	}
	sshDir, _ := SSHDir()
	if want := filepath.Join(dev, ".ssh"); sshDir != want {
		t.Errorf("SSHDir() = %q, want %q", sshDir, want)
	}
	expanded, _ := ExpandPath("~/keys/id")
	if want := filepath.Join(dev, "keys/id"); expanded != filepath.Clean(want) {
		t.Errorf("ExpandPath(~/keys/id) = %q, want %q", expanded, want)
	}
	// The real home must stay untouched by path resolution.
	if strings.HasPrefix(dir, home) && !strings.HasPrefix(dir, dev) {
		t.Errorf("Dir() leaked into the real home: %q", dir)
	}
}

func TestBackupMissingFileIsNoop(t *testing.T) {
	testHome(t)
	if err := BackupFile(filepath.Join(t.TempDir(), "nope")); err != nil {
		t.Errorf("BackupFile(missing) error = %v", err)
	}
}

func TestExpandPath(t *testing.T) {
	home := testHome(t)
	cases := []struct{ in, want string }{
		{"~/x/y", filepath.Join(home, "x/y")},
		{"~", home},
		{"/abs/path", "/abs/path"},
		{"", ""},
	}
	for _, c := range cases {
		got, err := ExpandPath(c.in)
		if err != nil {
			t.Fatalf("ExpandPath(%q) error = %v", c.in, err)
		}
		want := c.want
		if want != "" {
			want = filepath.Clean(want)
		}
		if got != want {
			t.Errorf("ExpandPath(%q) = %q, want %q", c.in, got, want)
		}
	}
}
