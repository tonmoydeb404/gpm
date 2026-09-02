package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempRepo(t *testing.T) (repo, outside string) {
	t.Helper()
	repo = t.TempDir()
	outside = t.TempDir()
	if _, err := Run(repo, "init", "-q"); err != nil {
		t.Skipf("git unavailable or init failed: %v", err)
	}
	return repo, outside
}

func TestInsideWorkTree(t *testing.T) {
	repo, outside := tempRepo(t)
	if !InsideWorkTree(repo) {
		t.Error("repo should be a work tree")
	}
	if InsideWorkTree(outside) {
		t.Error("plain temp dir should not be a work tree")
	}
	if _, err := TopLevel(outside); err == nil {
		t.Error("TopLevel outside a repo should fail")
	} else if !strings.Contains(err.Error(), ErrNotRepo.Error()) {
		t.Errorf("error should wrap ErrNotRepo, got %v", err)
	}
}

func TestIdentityRoundtrip(t *testing.T) {
	repo, _ := tempRepo(t)

	if err := SetIdentity(repo, "Jane Doe", "jane@corp.com"); err != nil {
		t.Fatalf("SetIdentity() error = %v", err)
	}
	email, err := ConfigValue(repo, "user.email")
	if err != nil || email != "jane@corp.com" {
		t.Errorf("ConfigValue(user.email) = %q, %v", email, err)
	}

	originEmail, origin, err := ConfigOriginValue(repo, "user.email")
	if err != nil || originEmail != "jane@corp.com" {
		t.Fatalf("ConfigOriginValue = %q, %v, %v", originEmail, origin, err)
	}
	if !filepath.IsAbs(origin) {
		t.Errorf("origin = %q, want absolute path", origin)
	}
	if filepath.Dir(origin) != filepath.Join(repo, ".git") {
		t.Errorf("origin file = %q, want inside %s/.git", origin, repo)
	}

	name, email, err := GetRepoLocalIdentity(repo)
	if err != nil || name != "Jane Doe" || email != "jane@corp.com" {
		t.Errorf("GetRepoLocalIdentity = %q, %q, %v", name, email, err)
	}

	if err := UnsetIdentity(repo); err != nil {
		t.Fatalf("UnsetIdentity() error = %v", err)
	}
	name, email, err = GetRepoLocalIdentity(repo)
	if err != nil {
		t.Fatalf("GetRepoLocalIdentity() after unset error = %v", err)
	}
	if name != "" || email != "" {
		t.Errorf("repo-local identity after unset = %q, %q, want empty", name, email)
	}
}

func TestConfigValueUnset(t *testing.T) {
	repo, _ := tempRepo(t)
	val, err := ConfigValue(repo, "gpm.nonexistent.key")
	if err != nil {
		t.Fatalf("ConfigValue() error = %v", err)
	}
	if val != "" {
		t.Errorf("ConfigValue() = %q, want empty", val)
	}
}

func TestSandboxGitConfigIsolation(t *testing.T) {
	repo, _ := tempRepo(t)
	dev := t.TempDir()
	t.Setenv("GPM_HOME", dev)

	// Something in the REAL global config that must not be visible.
	real := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(real, "gitconfig"))
	os.WriteFile(filepath.Join(real, "gitconfig"), []byte("[gpm]\n\treal = 1\n"), 0o600)

	sandboxGitconfig := filepath.Join(dev, ".gitconfig")
	os.MkdirAll(dev, 0o700)
	os.WriteFile(sandboxGitconfig, []byte("[gpm]\n\tsandbox = 1\n"), 0o600)

	val, err := ConfigValue(repo, "gpm.sandbox")
	if err != nil || val != "1" {
		t.Errorf("ConfigValue(gpm.sandbox) = %q, %v; want 1 via sandbox gitconfig", val, err)
	}
	val, err = ConfigValue(repo, "gpm.real")
	if err != nil || val != "" {
		t.Errorf("ConfigValue(gpm.real) = %q, %v; want empty (real global config must be hidden)", val, err)
	}
}

func TestRunInMissingDir(t *testing.T) {
	if _, err := Run(filepath.Join(os.TempDir(), "gpm-missing-dir"), "status"); err == nil {
		t.Error("expected error running git in a missing directory")
	}
}
