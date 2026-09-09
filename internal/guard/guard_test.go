package guard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tonmoydeb404/gpm/internal/gitcmd"
)

func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := gitcmd.Run(dir, "init", "-q"); err != nil {
		t.Skipf("git unavailable: %v", err)
	}
	return dir
}

func TestInstallAndRemove(t *testing.T) {
	repo := tempRepo(t)

	path, err := Install(repo, false)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !strings.Contains(path, filepath.Join(".git", "hooks", "pre-commit")) {
		t.Errorf("hook path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), hookMarker) {
		t.Errorf("hook missing marker: %q", data)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm()&0o100 == 0 {
			t.Error("hook must be executable")
		}
	}
	ok, err := IsInstalled(repo)
	if err != nil || !ok {
		t.Errorf("IsInstalled = %v, %v", ok, err)
	}

	removed, err := Remove(repo)
	if err != nil || !removed {
		t.Fatalf("Remove() = %v, %v", removed, err)
	}
	ok, _ = IsInstalled(repo)
	if ok {
		t.Error("hook still installed after Remove")
	}
}

func TestInstallRefusesForeignHook(t *testing.T) {
	repo := tempRepo(t)
	hooks := filepath.Join(repo, ".git", "hooks")
	os.MkdirAll(hooks, 0o755)
	os.WriteFile(filepath.Join(hooks, "pre-commit"), []byte("#!/bin/sh\necho custom\n"), 0o755)

	if _, err := Install(repo, false); err == nil {
		t.Fatal("expected refusal to overwrite a foreign hook")
	}
	if _, err := Install(repo, true); err != nil {
		t.Fatalf("Install(force) error = %v", err)
	}
	// After a forced install the hook is gpm's, so Remove must work.
	if removed, err := Remove(repo); err != nil || !removed {
		t.Fatalf("Remove() after force-install = %v, %v", removed, err)
	}
}

func TestRemoveMissingIsNoop(t *testing.T) {
	repo := tempRepo(t)
	removed, err := Remove(repo)
	if err != nil || removed {
		t.Errorf("Remove(missing) = %v, %v; want false, nil", removed, err)
	}
}
