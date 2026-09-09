package importer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/sshkey"
)

// writeKey generates a key pair under ~/.ssh and returns its path.
func writeKey(t *testing.T, home, name string) string {
	t.Helper()
	key := filepath.Join(home, ".ssh", name)
	if _, err := sshkey.Generate(key, "test"); err != nil {
		t.Fatal(err)
	}
	return key
}

// writeSSHConfig installs ~/.ssh/config with the given content.
func writeSSHConfig(t *testing.T, home, content string) {
	t.Helper()
	dir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseGitConfigLines(t *testing.T) {
	content := `# comment
[user]
	name = Jane Doe
	email = jane@corp.com ; inline comment
[include]
	path = "~/.gitconfig-extra"
[includeIf "gitdir:~/works/corp/"]
	path = corp.gitconfig
[bare]
	enabled
`
	vars := parseGitConfigLines(content)
	want := []gitVar{
		{section: "user", key: "name", value: "Jane Doe"},
		{section: "user", key: "email", value: "jane@corp.com"},
		{section: "include", key: "path", value: "~/.gitconfig-extra"},
		{section: "includeif", subsection: "gitdir:~/works/corp/", key: "path", value: "corp.gitconfig"},
		{section: "bare", key: "enabled", value: "true"},
	}
	if len(vars) != len(want) {
		t.Fatalf("vars = %+v, want %d entries", vars, len(want))
	}
	for i, w := range want {
		if vars[i] != w {
			t.Errorf("vars[%d] = %+v, want %+v", i, vars[i], w)
		}
	}
}

func TestScanGitConfigIncludeChain(t *testing.T) {
	home := testHome(t)
	key := writeKey(t, home, "id_work")
	writeSSHConfig(t, home, "Host work\n  HostName github.com\n  IdentityFile "+key+"\n")

	// root -> identities -> includeIf -> corp.gitconfig (email)
	root := filepath.Join(home, ".gitconfig")
	os.WriteFile(root, []byte("[include]\n\tpath = ~/.gitconfig-identities\n"), 0o644)
	identities := filepath.Join(home, ".gitconfig-identities")
	os.WriteFile(identities, []byte("[includeIf \"gitdir:~/works/corp/\"]\n\tpath = "+filepath.Join(home, "corp.gitconfig")+"\n"), 0o644)
	os.WriteFile(filepath.Join(home, "corp.gitconfig"), []byte("[user]\n\temail = jane@corp.com\n"), 0o600)

	existing := config.New()
	existing.GitProfiles["work"] = config.GitProfile{Username: "work", Email: "jane@corp.com"}

	rep := Scan(existing, ScanOptions{})
	if len(rep.Candidates) != 1 {
		t.Fatalf("candidates = %+v, want 1", rep.Candidates)
	}
	c := rep.Candidates[0]
	found := false
	for _, d := range c.Directories {
		if d == filepath.Join(home, "works", "corp") {
			found = true
		}
	}
	if !found {
		t.Errorf("directory mapping lost through the include chain: %+v", c)
	}
}

func TestScanGitConfigCycle(t *testing.T) {
	home := testHome(t)
	writeKey(t, home, "id_work")
	writeSSHConfig(t, home, "Host work\n  HostName github.com\n  IdentityFile "+filepath.Join(home, ".ssh", "id_work")+"\n")

	a := filepath.Join(home, "a.gitconfig")
	b := filepath.Join(home, "b.gitconfig")
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[include]\n\tpath = a.gitconfig\n"), 0o644)
	os.WriteFile(a, []byte("[include]\n\tpath = b.gitconfig\n"), 0o644)
	os.WriteFile(b, []byte("[include]\n\tpath = a.gitconfig\n[user]\n\temail = jane@corp.com\n"), 0o644)

	done := make(chan *Report, 1)
	go func() { done <- Scan(nil, ScanOptions{}) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("Scan did not finish; include cycle not guarded")
	}
}

func TestScanSSHInclude(t *testing.T) {
	home := testHome(t)
	key := writeKey(t, home, "id_work")
	writeSSHConfig(t, home, "Include ~/.ssh/config.d/*\n")
	os.MkdirAll(filepath.Join(home, ".ssh", "config.d"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config.d", "work"),
		[]byte("Host work\n  HostName github.com\n  IdentityFile "+key+"\n"), 0o600)

	rep := Scan(nil, ScanOptions{})
	if len(rep.Candidates) != 1 || rep.Candidates[0].Username != "work" {
		t.Fatalf("candidates = %+v, want the included work host", rep.Candidates)
	}
	if rep.Candidates[0].KeyPath != key {
		t.Errorf("key path = %q, want %q", rep.Candidates[0].KeyPath, key)
	}
}

func TestResolveEffectiveSSHFallback(t *testing.T) {
	if _, _, ok := resolveEffectiveSSH(filepath.Join(t.TempDir(), "missing"), "any"); ok {
		t.Error("expected failure for a missing config file")
	}
}

func TestScanReportFresh(t *testing.T) {
	home := testHome(t)
	key := writeKey(t, home, "id_work")
	writeSSHConfig(t, home, "Host work\n  HostName github.com\n  IdentityFile "+key+"\n")
	rep := Scan(nil, ScanOptions{})

	cfg := config.New()
	if got := rep.Fresh(cfg); len(got) != 1 {
		t.Fatalf("fresh = %d, want 1", len(got))
	}
	cfg.SSHProfiles["work"] = config.SSHProfile{Username: "work", KeyPath: key}
	if got := rep.Fresh(cfg); len(got) != 0 {
		t.Fatalf("fresh = %d, want 0 after import", len(got))
	}
}

func TestGitdirDirVariants(t *testing.T) {
	home := testHome(t)
	cases := map[string]struct {
		cond string
		want string
	}{
		"trailing slash": {`gitdir:~/works/corp/`, filepath.Join(home, "works", "corp")},
		"glob":           {`gitdir:~/works/**`, filepath.Join(home, "works")},
		"insensitive":    {`gitdir/i:~/Works/Corp/`, filepath.Join(home, "Works", "Corp")},
		"no slash":       {`gitdir:~/works/corp`, filepath.Join(home, "works", "corp")},
	}
	for name, tc := range cases {
		got, ok := gitdirDir(tc.cond)
		if !ok || got != tc.want {
			t.Errorf("%s: gitdirDir(%q) = %q, %v; want %q", name, tc.cond, got, ok, tc.want)
		}
	}
	if _, ok := gitdirDir(`gitdir:is/absolute/already`); ok {
		t.Error("bare relative condition must be rejected")
	}
	if _, ok := gitdirDir(`onbranch:main`); ok {
		t.Error("non-gitdir condition must be rejected")
	}
}

func TestGlobalIdentityDetection(t *testing.T) {
	home := testHome(t)
	key1 := writeKey(t, home, "id_global")
	key2 := writeKey(t, home, "id_work")
	writeSSHConfig(t, home, "Host github.com\n  HostName github.com\n  IdentityFile "+key1+"\nHost github.com-work\n  HostName github.com\n  IdentityFile "+key2+"\n")
	// global gitconfig identity
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = alice\n\temail = alice@example.com\n"), 0o644)

	rep := Scan(nil, ScanOptions{})
	if len(rep.Candidates) != 2 {
		t.Fatalf("candidates = %+v, want 2 (global + scoped)", rep.Candidates)
	}
	var global *Candidate
	for i := range rep.Candidates {
		if rep.Candidates[i].HostAlias == "" {
			global = &rep.Candidates[i]
		}
	}
	if global == nil {
		t.Fatalf("global candidate missing: %+v", rep.Candidates)
	}
	if global.Hosts[0] != "github.com" {
		t.Errorf("global hosts = %v, want github.com", global.Hosts)
	}
	if global.Email != "alice@example.com" {
		t.Errorf("global email = %q, want alice@example.com", global.Email)
	}
	if global.Username != "alice" {
		t.Errorf("global username = %q, want alice (config-based)", global.Username)
	}
}

func TestConfigBasedUsernamePairingWork(t *testing.T) {
	home := testHome(t)
	k1 := writeKey(t, home, "id_global")
	k2 := writeKey(t, home, "id_work")
	writeSSHConfig(t, home, "Host github.com\n  HostName github.com\n  IdentityFile "+k1+"\nHost github.com-work\n  HostName github.com\n  IdentityFile "+k2+"\n")
	os.WriteFile(filepath.Join(home, ".gitconfig"), []byte("[user]\n\tname = tonmoydeb404\n\temail = tonmoydeb404@gmail.com\n[includeIf \"gitdir:~/work/\"]\n\tpath = "+filepath.Join(home, ".gitconfig-work")+"\n"), 0o644)
	os.WriteFile(filepath.Join(home, ".gitconfig-work"), []byte("[user]\n\tname = tonmoydeb.work\n\temail = tonmoydeb.work@gmail.com\n"), 0o600)

	rep := Scan(nil, ScanOptions{})
	var work *Candidate
	for i := range rep.Candidates {
		if rep.Candidates[i].HostAlias == "github.com-work" {
			work = &rep.Candidates[i]
		}
	}
	if work == nil {
		t.Fatalf("work candidate missing: %+v", rep.Candidates)
	}
	if work.Username != "tonmoydeb.work" {
		t.Errorf("work username = %q, want tonmoydeb.work (config-based via suffix heuristic)", work.Username)
	}
	if work.Email != "tonmoydeb.work@gmail.com" {
		t.Errorf("work email = %q, want tonmoydeb.work@gmail.com", work.Email)
	}
	if len(work.Directories) != 1 || work.Directories[0] != filepath.Join(home, "work") {
		t.Errorf("work directories = %v, want [~/work]", work.Directories)
	}
}

func TestStripGitValue(t *testing.T) {
	cases := map[string]string{
		` jane@corp.com`:         "jane@corp.com",
		`"my key"`:               "my key",
		`value # comment`:        "value",
		`"quoted # not comment"`: "quoted # not comment",
		`esc\"aped`:              `esc"aped`,
		``:                       "",
	}
	for in, want := range cases {
		if got := stripGitValue(in); got != want {
			t.Errorf("stripGitValue(%q) = %q, want %q", in, got, want)
		}
	}
}
