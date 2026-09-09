package importer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/gitprofile"
	"github.com/tonmoydeb404/gpm/internal/managed"
	"github.com/tonmoydeb404/gpm/internal/sshkey"
	"github.com/tonmoydeb404/gpm/internal/sshprofile"
)

func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("GPM_HOME", "")
	return dir
}

func TestParseSSHConfig(t *testing.T) {
	content := `# leading comment
Host work github-work
  HostName github.com
  User git
  IdentityFile ~/.ssh/id_work

Host="quoted alias"
  Hostname github.com
  IdentityFile = /abs/path/key

Host *.corp.example.com
  HostName corp.example.com

Host oss
HostName github.com
IdentityFile ~/.ssh/id_oss

`
	stanzas := parseSSHConfig(content)
	if len(stanzas) != 4 {
		t.Fatalf("stanzas = %d, want 4: %+v", len(stanzas), stanzas)
	}
	first := stanzas[0]
	if first.primaryHost() != "work" {
		t.Errorf("primaryHost = %q, want work", first.primaryHost())
	}
	if first.get("hostname") != "github.com" || first.get("user") != "git" {
		t.Errorf("options parsed wrong: %+v", first.options)
	}
	if first.firstIdentityFile() != "~/.ssh/id_work" {
		t.Errorf("identityfile = %q", first.firstIdentityFile())
	}
	if stanzas[1].primaryHost() != "quoted alias" {
		t.Errorf("quoted host = %q", stanzas[1].primaryHost())
	}
	if stanzas[3].get("hostname") != "github.com" {
		t.Errorf("compact form hostname = %q", stanzas[3].get("hostname"))
	}
}

func TestStripManaged(t *testing.T) {
	content := "Host mine\n  HostName example.com\n\n" +
		managed.Begin + "\nHost gpm-work\n" + managed.End + "\n"
	out := stripManaged(content)
	if strings.Contains(out, "gpm-work") {
		t.Errorf("managed content survived: %q", out)
	}
	if !strings.Contains(out, "Host mine") {
		t.Errorf("user content lost: %q", out)
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"work":      "work",
		"gh work":   "gh-work",
		"!!weird!!": "weird",
		"9lives":    "9lives",
		"":          "",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitdirDir(t *testing.T) {
	home := testHome(t)
	if d, ok := gitdirDir(`gitdir:~/works/corp/`); !ok || d != filepath.Join(home, "works", "corp") {
		t.Errorf("gitdirDir = %q, %v", d, ok)
	}
	if d, ok := gitdirDir(`gitdir/i:~/works/corp/**`); !ok || d != filepath.Join(home, "works", "corp") {
		t.Errorf("gitdirDir = %q, %v", d, ok)
	}
	if _, ok := gitdirDir("onpurpose"); ok {
		t.Error("non-gitdir condition must not match")
	}
	if _, ok := gitdirDir("gitdir:./relative"); ok {
		t.Error("relative gitdir condition must not match")
	}
}

func TestFileEmail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg")
	os.WriteFile(path, []byte("[user]\n\tname = J\n\temail = jane@corp.com\n"), 0o600)
	if got := fileEmail(path, map[string]bool{}, 0); got != "jane@corp.com" {
		t.Errorf("fileEmail = %q", got)
	}

	// A file that includes another one picks up the included email,
	// with later definitions winning.
	included := filepath.Join(dir, "extra")
	os.WriteFile(included, []byte("[user]\n\temail = extra@corp.com\n"), 0o600)
	chain := filepath.Join(dir, "chain")
	os.WriteFile(chain, []byte("[user]\n\temail = jane@corp.com\n[include]\n\tpath = extra\n"), 0o600)
	if got := fileEmail(chain, map[string]bool{}, 0); got != "extra@corp.com" {
		t.Errorf("fileEmail(chain) = %q, want the included email", got)
	}
}

func TestDetectSingleCandidateWithGlobalIdentity(t *testing.T) {
	home := testHome(t)
	key := filepath.Join(home, ".ssh", "id_work")
	if _, err := sshkey.Generate(key, "test"); err != nil {
		t.Fatal(err)
	}
	sshCfg := "Host work\n  HostName github.com\n  User git\n  IdentityFile " + key + "\n"
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(sshCfg), 0o600)

	gitCfg := "[user]\n\tname = Jane Doe\n\temail = jane@corp.com\n" +
		`[includeIf "gitdir:` + home + `/works/corp/"]` + "\n\tpath = " + home + `/.config/gpm/gitconfig/work` + "\n"
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte(gitCfg), 0o644)

	// The includeIf target is a pre-existing file with the account email.
	includeTarget := filepath.Join(home, ".config", "gpm", "gitconfig", "work")
	os.MkdirAll(filepath.Dir(includeTarget), 0o700)
	os.WriteFile(includeTarget, []byte("[user]\n\temail = jane@corp.com\n"), 0o600)

	det, err := Detect(nil)
	if err != nil {
		t.Fatalf("Detect(nil) error = %v", err)
	}
	if len(det.Candidates) != 1 {
		t.Fatalf("candidates = %+v, want 1", det.Candidates)
	}
	c := det.Candidates[0]
	if c.Username != "work" || c.Email != "jane@corp.com" || c.KeyPath != key {
		t.Errorf("candidate = %+v", c)
	}
	if len(c.Hosts) != 1 || c.Hosts[0] != "github.com" {
		t.Errorf("candidate hosts = %+v", c.Hosts)
	}
	if len(c.Directories) != 1 {
		t.Errorf("candidate directories = %+v", c.Directories)
	}
}

func TestDetectSkipsMissingKeysAndImportsOtherHosts(t *testing.T) {
	home := testHome(t)
	sshCfg := `
Host broken
  HostName github.com
  IdentityFile ~/.ssh/does_not_exist

Host corp
  HostName git.company.com
  IdentityFile ` + filepath.Join(home, ".ssh", "id_corp") + `
`
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(sshCfg), 0o600)
	if _, err := sshkey.Generate(filepath.Join(home, ".ssh", "id_corp"), "test"); err != nil {
		t.Fatal(err)
	}
	det, err := Detect(nil)
	if err != nil {
		t.Fatalf("Detect(nil) error = %v", err)
	}
	if len(det.Candidates) != 1 {
		t.Fatalf("candidates = %+v, want 1", det.Candidates)
	}
	c := det.Candidates[0]
	if c.Username != "corp" || len(c.Hosts) != 1 || c.Hosts[0] != "git.company.com" {
		t.Errorf("candidate = %+v", c)
	}
	if len(det.Warnings) == 0 {
		t.Error("expected warnings for skipped hosts")
	}
}

func TestDetectMissingSSHConfig(t *testing.T) {
	testHome(t)
	det, err := Detect(nil)
	if err != nil {
		t.Fatalf("Detect(nil) error = %v", err)
	}
	if len(det.Candidates) != 0 {
		t.Errorf("expected no candidates, got %+v", det.Candidates)
	}
}

func TestImportRoundtripThroughProfiles(t *testing.T) {
	home := testHome(t)
	key := filepath.Join(home, ".ssh", "id_oss")
	sshkey.Generate(key, "test")
	cfg := config.New()

	// The import steps shared by the CLI and the TUI.
	if _, err := sshprofile.Add(cfg, config.SSHProfile{
		Username:  "oss",
		KeyPath:   key,
		HostAlias: "oss",
		Providers: []string{"github.com"},
	}, sshprofile.AddOpts{}); err != nil {
		t.Fatalf("sshprofile.Add() error = %v", err)
	}
	if _, err := gitprofile.Add(cfg, config.GitProfile{
		Username: "oss",
		Email:    "jane@oss.dev",
	}, ""); err != nil {
		t.Fatalf("gitprofile.Add() error = %v", err)
	}

	if cfg.SSHProfiles["oss"].KeyPath != key {
		t.Errorf("ssh profile not stored: %+v", cfg.SSHProfiles)
	}
	if cfg.GitProfiles["oss"].Email != "jane@oss.dev" {
		t.Errorf("git profile not stored: %+v", cfg.GitProfiles)
	}
}
