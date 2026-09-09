package sshprofile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/sshkey"
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

func TestValidateHost(t *testing.T) {
	for _, host := range []string{"github.com", "gitlab.com", "git.company.com", "ssh-git-01.internal"} {
		if err := ValidateHost(host); err != nil {
			t.Errorf("ValidateHost(%q) error = %v", host, err)
		}
	}
	for _, host := range []string{"", "github com", "https://github.com", "github.com/x", "git:laptop"} {
		if err := ValidateHost(host); err == nil {
			t.Errorf("ValidateHost(%q) must fail", host)
		}
	}
}

func TestValidateAlias(t *testing.T) {
	if err := ValidateAlias(""); err != nil {
		t.Errorf("empty alias (global) must be allowed: %v", err)
	}
	for _, alias := range []string{"gpm-work", "work", "work.github.com"} {
		if err := ValidateAlias(alias); err != nil {
			t.Errorf("ValidateAlias(%q) error = %v", alias, err)
		}
	}
	for _, alias := range []string{"my alias", "alias/x"} {
		if err := ValidateAlias(alias); err == nil {
			t.Errorf("ValidateAlias(%q) must fail", alias)
		}
	}
}

func TestNormalizeProviders(t *testing.T) {
	got, err := NormalizeProviders([]string{" GitHub.COM ", "github.com", "GitLab.com"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "github.com" || got[1] != "gitlab.com" {
		t.Errorf("NormalizeProviders() = %v, want [github.com gitlab.com]", got)
	}
}

func TestAddGeneratesKey(t *testing.T) {
	testHome(t)
	cfg := config.New()
	res, err := Add(cfg, config.SSHProfile{
		Username:  "work",
		HostAlias: "gpm-work",
		Providers: []string{"github.com"},
	}, AddOpts{GenerateKey: true})
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !res.GeneratedKey || res.PublicKey == "" {
		t.Fatalf("expected a generated key, got %+v", res)
	}
	p := cfg.SSHProfiles["work"]
	if p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
		t.Errorf("key not on disk: %q", p.KeyPath)
	}
	homeDir, _ := config.Home()
	if want := filepath.Join(homeDir, ".ssh", "id_ed25519_work"); p.KeyPath != want {
		t.Errorf("KeyPath = %q, want %q", p.KeyPath, want)
	}
	// The username is the key comment.
	if !strings.HasSuffix(strings.TrimSpace(res.PublicKey), " work") {
		t.Errorf("key comment should be the username: %q", res.PublicKey)
	}
}

func TestAddGlobalAndConstraint(t *testing.T) {
	testHome(t)
	cfg := config.New()

	// The first profile without an alias becomes the global identity.
	if _, err := Add(cfg, config.SSHProfile{Username: "global", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatalf("Add(global) error = %v", err)
	}
	if _, ok := cfg.GlobalSSHProfile(); !ok {
		t.Error("expected a global ssh profile")
	}

	_, err := Add(cfg, config.SSHProfile{Username: "second", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true})
	if err == nil {
		t.Fatal("a second global ssh profile must be refused")
	}
	if !strings.Contains(err.Error(), "global") {
		t.Errorf("error should mention global: %v", err)
	}

	// With an alias it is fine.
	if _, err := Add(cfg, config.SSHProfile{Username: "second", HostAlias: "gpm-second", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatalf("Add(second) error = %v", err)
	}
}

func TestAliasUniqueness(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.SSHProfile{Username: "work", HostAlias: "gpm-work", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatal(err)
	}
	_, err := Add(cfg, config.SSHProfile{Username: "copy", HostAlias: "gpm-work", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true})
	if err == nil {
		t.Fatal("duplicate alias must be refused")
	}
	// The same profile may keep its alias.
	if err := Edit(cfg, "work", func(p *config.SSHProfile) { p.HostAlias = "gpm-work" }); err != nil {
		t.Fatalf("Edit with unchanged alias error = %v", err)
	}
}

func TestEditAliasRules(t *testing.T) {
	testHome(t)
	cfg := config.New()
	// "free" has no alias: it is the global profile.
	if _, err := Add(cfg, config.SSHProfile{Username: "free", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, config.SSHProfile{Username: "work", HostAlias: "gpm-work", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, config.SSHProfile{Username: "oss", HostAlias: "gpm-oss", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatal(err)
	}

	// Clearing an alias is refused while another profile is global.
	if err := Edit(cfg, "work", func(p *config.SSHProfile) { p.HostAlias = "" }); err == nil {
		t.Fatal("clearing an alias must be refused while another profile is global")
	}

	// Stealing an alias is refused.
	if err := Edit(cfg, "work", func(p *config.SSHProfile) { p.HostAlias = "gpm-oss" }); err == nil {
		t.Fatal("stealing an alias must be refused")
	}

	// A normal rename works.
	if err := Edit(cfg, "work", func(p *config.SSHProfile) { p.HostAlias = "corp" }); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if got := cfg.SSHProfiles["work"].HostAlias; got != "corp" {
		t.Errorf("HostAlias = %q, want corp", got)
	}

	// With the global profile gone, clearing an alias is allowed.
	if _, err := Remove(cfg, "free", RemoveOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := Edit(cfg, "oss", func(p *config.SSHProfile) { p.HostAlias = "" }); err != nil {
		t.Fatalf("clearing an alias without a global profile should work: %v", err)
	}
	if _, ok := cfg.GlobalSSHProfile(); !ok {
		t.Error("oss should now be the global ssh profile")
	}
}

func TestAddDuplicateRefused(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.SSHProfile{Username: "work", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatal(err)
	}
	_, err := Add(cfg, config.SSHProfile{Username: "work", Providers: []string{"gitlab.com"}}, AddOpts{GenerateKey: true})
	if err == nil {
		t.Fatal("duplicate ssh profile must be refused")
	}
}

func TestAddRequiresProvider(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.SSHProfile{Username: "work"}, AddOpts{GenerateKey: true}); err == nil {
		t.Fatal("profile without providers must be refused")
	}
}

func TestAddExistingKeyMustExist(t *testing.T) {
	testHome(t)
	cfg := config.New()
	_, err := Add(cfg, config.SSHProfile{Username: "work", KeyPath: "~/.ssh/nope", Providers: []string{"github.com"}}, AddOpts{})
	if err == nil {
		t.Fatal("missing existing key must be refused")
	}
}

func TestEditKeepsUsername(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.SSHProfile{Username: "work", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatal(err)
	}
	if err := Edit(cfg, "work", func(p *config.SSHProfile) {
		p.Username = "hijacked"
	}); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	p := cfg.SSHProfiles["work"]
	if p.Username != "work" {
		t.Errorf("username must be immutable, got %q", p.Username)
	}
}

func TestProviders(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.SSHProfile{Username: "work", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true}); err != nil {
		t.Fatal(err)
	}
	if err := AddProvider(cfg, "work", "GitLab.COM"); err != nil {
		t.Fatalf("AddProvider() error = %v", err)
	}
	if got := cfg.SSHProfiles["work"].Providers; len(got) != 2 || got[1] != "gitlab.com" {
		t.Errorf("providers = %v", got)
	}
	if err := AddProvider(cfg, "work", "github.com"); err == nil {
		t.Error("duplicate provider must be refused")
	}
	if err := RemoveProvider(cfg, "work", "gitlab.com"); err != nil {
		t.Fatalf("RemoveProvider() error = %v", err)
	}
	if err := RemoveProvider(cfg, "work", "github.com"); err == nil {
		t.Error("removing the last provider must be refused")
	}
}

func TestRemove(t *testing.T) {
	testHome(t)
	cfg := config.New()
	res, err := Add(cfg, config.SSHProfile{Username: "work", Providers: []string{"github.com"}}, AddOpts{GenerateKey: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Remove(cfg, "work", RemoveOpts{RemoveKey: true})
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if out.RemovedKeyPath != res.KeyPath {
		t.Errorf("RemovedKeyPath = %q, want %q", out.RemovedKeyPath, res.KeyPath)
	}
	if sshkey.Exists(res.KeyPath) {
		t.Error("key pair still on disk after Remove")
	}
	if _, ok := cfg.SSHProfiles["work"]; ok {
		t.Error("profile still present after Remove")
	}
}

func TestSortedUsernames(t *testing.T) {
	cfg := config.New()
	cfg.SSHProfiles["z"] = config.SSHProfile{Username: "z", Providers: []string{"github.com"}}
	cfg.SSHProfiles["a"] = config.SSHProfile{Username: "a", Providers: []string{"github.com"}}
	got := SortedUsernames(cfg)
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Errorf("SortedUsernames() = %v", got)
	}
}
