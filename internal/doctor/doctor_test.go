package doctor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tonmoydeb404/gpm/internal/config"
	"github.com/tonmoydeb404/gpm/internal/gitconfig"
	"github.com/tonmoydeb404/gpm/internal/sshconfig"
	"github.com/tonmoydeb404/gpm/internal/sshkey"

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

func setupHealthy(t *testing.T) (*config.Config, string) {
	t.Helper()
	home := testHome(t)
	cfg := config.New()
	if _, err := sshkey.Generate(filepath.Join(home, ".ssh", "id_ed25519_work"), "test"); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(home, ".ssh", "id_ed25519_work")
	cfg.GitProfiles["work"] = config.GitProfile{Username: "work", Email: "jane@corp.com"} // no dirs: global
	cfg.SSHProfiles["work"] = config.SSHProfile{Username: "work", KeyPath: keyPath, HostAlias: "gpm-work", Providers: []string{"github.com"}}
	if err := sshconfig.SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := gitconfig.SyncFromConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return cfg, home
}

func statusOf(results []Result, name string) *Result {
	for i := range results {
		if results[i].Name == name {
			return &results[i]
		}
	}
	return nil
}

func TestHealthySetupPasses(t *testing.T) {
	cfg, _ := setupHealthy(t)
	results := Run(cfg, Options{})
	for _, r := range results {
		if r.Status == Fail {
			t.Errorf("unexpected failure: %s: %s", r.Name, r.Detail)
		}
	}
	if r := statusOf(results, "Git profiles"); r == nil || r.Status != Pass {
		t.Errorf("Git profiles check = %+v", r)
	}
	if r := statusOf(results, "SSH profiles"); r == nil || r.Status != Pass {
		t.Errorf("SSH profiles check = %+v", r)
	}
	if r := statusOf(results, "Global profile"); r == nil || r.Status != Pass {
		t.Errorf("Global profile check = %+v", r)
	}
	if r := statusOf(results, "SSH config"); r == nil || r.Status != Pass {
		t.Errorf("SSH config check = %+v", r)
	}
	if r := statusOf(results, "Git config"); r == nil || r.Status != Pass {
		t.Errorf("Git config check = %+v", r)
	}
}

func TestMissingKeyFails(t *testing.T) {
	cfg, home := setupHealthy(t)
	os.Remove(filepath.Join(home, ".ssh", "id_ed25519_work"))
	results := Run(cfg, Options{})
	r := statusOf(results, "SSH keys")
	if r == nil || r.Status != Fail {
		t.Errorf("SSH keys check = %+v, want fail", r)
	}
	if r.Hint == "" {
		t.Error("failure should carry an actionable hint")
	}
}

func TestPermissionsFixed(t *testing.T) {
	cfg, home := setupHealthy(t)
	key := filepath.Join(home, ".ssh", "id_ed25519_work")
	os.Chmod(key, 0o644)

	r := statusOf(Run(cfg, Options{}), "Permissions")
	if r == nil || r.Status != Warn {
		t.Errorf("Permissions check = %+v, want warn", r)
	}

	r = statusOf(Run(cfg, Options{Fix: true}), "Permissions")
	if r == nil || r.Status != Pass {
		t.Errorf("Permissions check after fix = %+v, want pass", r)
	}
	if ok, cur := perm.Private(key); !ok {
		t.Errorf("key perms = %s, want private after fix", cur)
	}
}

func TestMissingSSHBlockFixed(t *testing.T) {
	cfg, home := setupHealthy(t)
	os.Remove(filepath.Join(home, ".ssh", "config"))

	r := statusOf(Run(cfg, Options{}), "SSH config")
	if r == nil || r.Status != Fail {
		t.Errorf("SSH config check = %+v, want fail", r)
	}

	r = statusOf(Run(cfg, Options{Fix: true}), "SSH config")
	if r == nil || r.Status != Pass {
		t.Errorf("SSH config check after fix = %+v, want pass", r)
	}
}

func TestMissingMappingPathWarns(t *testing.T) {
	cfg, _ := setupHealthy(t)
	p := cfg.GitProfiles["work"]
	p.Directories = []string{filepath.Join(t.TempDir(), "does/not/exist")}
	cfg.GitProfiles["work"] = p
	r := statusOf(Run(cfg, Options{}), "Directory mappings")
	if r == nil || r.Status != Warn {
		t.Errorf("Directory mappings check = %+v, want warn", r)
	}
}

func TestNoProfilesWarns(t *testing.T) {
	testHome(t)
	results := Run(config.New(), Options{})
	if r := statusOf(results, "Git profiles"); r == nil || r.Status != Warn {
		t.Errorf("Git profiles check = %+v, want warn", r)
	}
	if r := statusOf(results, "SSH profiles"); r == nil || r.Status != Warn {
		t.Errorf("SSH profiles check = %+v, want warn", r)
	}
	if r := statusOf(results, "Global profile"); r == nil || r.Status != Warn {
		t.Errorf("Global profile check = %+v, want warn", r)
	}
}

func TestMultipleGlobalsFail(t *testing.T) {
	cfg, _ := setupHealthy(t)
	cfg.GitProfiles["oss"] = config.GitProfile{Username: "oss", Email: "oss@x.com"} // second dir-less profile
	r := statusOf(Run(cfg, Options{}), "Global profile")
	if r == nil || r.Status != Fail {
		t.Errorf("Global profile check = %+v, want fail", r)
	}
}
