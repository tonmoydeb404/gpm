package gitprofile

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/tonmoydeb/gpm/internal/config"
)

func testHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, ".config"))
	t.Setenv("GPM_HOME", "")
	return dir
}

func TestValidate(t *testing.T) {
	if err := Validate(config.GitProfile{Username: "jane", Email: "jane@x.com"}); err != nil {
		t.Errorf("valid profile rejected: %v", err)
	}
	if err := Validate(config.GitProfile{Username: "", Email: "jane@x.com"}); err == nil {
		t.Error("empty username must be rejected")
	}
	if err := Validate(config.GitProfile{Username: "ja ne", Email: "jane@x.com"}); err == nil {
		t.Error("username with a space must be rejected")
	}
	if err := Validate(config.GitProfile{Username: "jane", Email: "not-an-email"}); err == nil {
		t.Error("invalid email must be rejected")
	}
}

func TestAddFirstProfileIsGlobal(t *testing.T) {
	testHome(t)
	cfg := config.New()
	res, err := Add(cfg, config.GitProfile{Username: "jane", Email: "jane@x.com"}, "")
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if !res.IsGlobal {
		t.Error("first dir-less profile must be global")
	}
	if _, ok := cfg.GlobalGitProfile(); !ok {
		t.Error("global profile not registered")
	}
}

func TestAddSecondGlobalRefused(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.GitProfile{Username: "jane", Email: "jane@x.com"}, ""); err != nil {
		t.Fatal(err)
	}
	_, err := Add(cfg, config.GitProfile{Username: "oss", Email: "oss@x.com"}, "")
	if err == nil {
		t.Fatal("expected error: only one profile can be global")
	}
	if !strings.Contains(err.Error(), "global") {
		t.Errorf("error should mention global: %v", err)
	}
}

func TestAddWithDirectory(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.GitProfile{Username: "jane", Email: "jane@x.com"}, "~/Works/corp"); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	home, _ := filepath.Abs(filepath.Join("home"))
	_ = home
	homeDir, _ := config.Home()
	want := filepath.Join(homeDir, "Works/corp")
	if got := cfg.GitProfiles["jane"].Directories; len(got) != 1 || got[0] != want {
		t.Errorf("directories = %v, want [%s]", got, want)
	}
	if _, ok := cfg.GlobalGitProfile(); ok {
		t.Error("profile with a directory must not be global")
	}
}

func TestAddDuplicateUsernameRefused(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.GitProfile{Username: "jane", Email: "jane@x.com"}, "~/a"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, config.GitProfile{Username: "jane", Email: "other@x.com"}, "~/b"); err == nil {
		t.Error("duplicate username must be refused")
	}
}

func TestAddDirectoryAlreadyMappedRefused(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.GitProfile{Username: "jane", Email: "jane@x.com"}, "~/Works"); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, config.GitProfile{Username: "oss", Email: "oss@x.com"}, "~/Works"); err == nil {
		t.Error("directory owned by another profile must be refused")
	}
}

func TestEditKeepsUsername(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.GitProfile{Username: "jane", Email: "jane@x.com"}, "~/a"); err != nil {
		t.Fatal(err)
	}
	if err := Edit(cfg, "jane", func(p *config.GitProfile) {
		p.Email = "new@x.com"
		p.Username = "hijacked"
	}); err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	p := cfg.GitProfiles["jane"]
	if p.Username != "jane" {
		t.Errorf("username must be immutable, got %q", p.Username)
	}
	if p.Email != "new@x.com" {
		t.Errorf("email = %q, want new@x.com", p.Email)
	}
}

func TestRemove(t *testing.T) {
	testHome(t)
	cfg := config.New()
	if _, err := Add(cfg, config.GitProfile{Username: "jane", Email: "jane@x.com"}, "~/a"); err != nil {
		t.Fatal(err)
	}
	res, err := Remove(cfg, "jane")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if res.RemovedDirs != 1 || res.WasGlobal {
		t.Errorf("RemoveResult = %+v", res)
	}
	if _, ok := cfg.GitProfiles["jane"]; ok {
		t.Error("profile still present after Remove")
	}
}

func TestSortedUsernames(t *testing.T) {
	cfg := config.New()
	cfg.GitProfiles["z"] = config.GitProfile{Username: "z", Email: "z@x.com"}
	cfg.GitProfiles["a"] = config.GitProfile{Username: "a", Email: "a@x.com"}
	got := SortedUsernames(cfg)
	if len(got) != 2 || got[0] != "a" || got[1] != "z" {
		t.Errorf("SortedUsernames() = %v", got)
	}
}
