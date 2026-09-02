package dirs

import (
	"os"
	"path/filepath"
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

func cfgWithProfiles() *config.Config {
	cfg := config.New()
	cfg.GitProfiles["work"] = config.GitProfile{Username: "work", Email: "j@c.com"}
	cfg.GitProfiles["oss"] = config.GitProfile{Username: "oss", Email: "j@o.dev"}
	return cfg
}

func TestNormalize(t *testing.T) {
	home := testHome(t)
	wd := t.TempDir()
	t.Chdir(wd)

	abs, err := Normalize("~/x")
	if err != nil {
		t.Fatal(err)
	}
	if abs != filepath.Join(home, "x") {
		t.Errorf("Normalize(~/x) = %q", abs)
	}
	rel, err := Normalize("sub/dir")
	if err != nil {
		t.Fatal(err)
	}
	if rel != filepath.Join(wd, "sub/dir") {
		t.Errorf("Normalize(relative) = %q, want %q", rel, filepath.Join(wd, "sub/dir"))
	}
	if _, err := Normalize(""); err == nil {
		t.Error("expected error for empty path")
	}
}

func TestAddAndResolve(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()

	if _, err := Add(cfg, "~/Works/corp", "work", false); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	home, _ := homeDir()
	wantPath := filepath.Join(home, "Works/corp")
	if got := cfg.GitProfiles["work"].Directories; len(got) != 1 || got[0] != wantPath {
		t.Errorf("stored path = %v, want [%s]", got, wantPath)
	}

	// Exact and descendant matches resolve.
	if name, ok := Resolve(cfg, wantPath); !ok || name != "work" {
		t.Errorf("Resolve(dir itself) = %q, %v", name, ok)
	}
	if name, ok := Resolve(cfg, filepath.Join(wantPath, "proj", "app")); !ok || name != "work" {
		t.Errorf("Resolve(child) = %q, %v", name, ok)
	}

	// Boundary safety: sibling prefixes must not match.
	if _, ok := Resolve(cfg, filepath.Join(home, "Works/corpse")); ok {
		t.Error("prefix must respect path boundaries")
	}
	if _, ok := Resolve(cfg, filepath.Join(home, "Works")); ok {
		t.Error("unrelated dir must not resolve")
	}
}

func TestAddDeepestMappingWins(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	if _, err := Add(cfg, "~/corp", "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, "~/corp/oss", "oss", false); err != nil {
		t.Fatal(err)
	}
	home, _ := homeDir()
	name, ok := Resolve(cfg, filepath.Join(home, "corp/oss/repo"))
	if !ok || name != "oss" {
		t.Errorf("deepest mapping should win, got %q, %v", name, ok)
	}
	name, ok = Resolve(cfg, filepath.Join(home, "corp/repo"))
	if !ok || name != "work" {
		t.Errorf("parent mapping should win inside its subtree, got %q, %v", name, ok)
	}
}

func TestAddNestingNotes(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	if _, err := Add(cfg, "~/corp", "work", false); err != nil {
		t.Fatal(err)
	}
	res, err := Add(cfg, "~/corp/oss", "oss", false)
	if err != nil {
		t.Fatalf("Add() nested error = %v", err)
	}
	// One "no longer global" note plus one nesting note.
	if len(res.Notes) != 2 {
		t.Errorf("expected 2 notes, got %v", res.Notes)
	}
}

func TestAddSamePathConflict(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	if _, err := Add(cfg, "~/corp", "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, "~/corp", "oss", false); err == nil {
		t.Error("expected conflict error for same path")
	}
	res, err := Add(cfg, "~/corp", "oss", true)
	if err != nil {
		t.Fatalf("Add(--force) error = %v", err)
	}
	if !res.Moved {
		t.Error("force should report Moved")
	}
	if got := cfg.GitProfiles["oss"].Directories; len(got) != 1 {
		t.Errorf("oss directories = %v, want the remapped path", got)
	}
	if got := cfg.GitProfiles["work"].Directories; len(got) != 0 {
		t.Errorf("work directories = %v, want empty after remap", got)
	}
}

func TestAddUnknownProfile(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	if _, err := Add(cfg, "~/x", "ghost", false); err == nil {
		t.Error("expected error for unknown profile")
	}
}

func TestOwner(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	home, _ := homeDir()
	if _, err := Add(cfg, "~/corp", "work", false); err != nil {
		t.Fatal(err)
	}
	if owner, ok := Owner(cfg, filepath.Join(home, "corp")); !ok || owner != "work" {
		t.Errorf("Owner() = %q, %v; want work, true", owner, ok)
	}
	if _, ok := Owner(cfg, filepath.Join(home, "other")); ok {
		t.Error("unmapped path must have no owner")
	}
}

func TestAllAndCount(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	if _, err := Add(cfg, "~/b", "oss", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, "~/a", "work", false); err != nil {
		t.Fatal(err)
	}
	all := All(cfg)
	if len(all) != 2 || all[0].Path > all[1].Path {
		t.Errorf("All() not sorted by path: %+v", all)
	}
	if Count(cfg) != 2 {
		t.Errorf("Count() = %d, want 2", Count(cfg))
	}
}

func TestRemove(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	if _, err := Add(cfg, "~/corp", "work", false); err != nil {
		t.Fatal(err)
	}
	// Map "oss" too so removing work's only directory does not make a
	// second global profile.
	if _, err := Add(cfg, "~/oss", "oss", false); err != nil {
		t.Fatal(err)
	}
	removed, err := Remove(cfg, "~/corp")
	if err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if removed.Profile != "work" || Count(cfg) != 1 {
		t.Errorf("unexpected removal result: %+v", removed)
	}
	if _, err := Remove(cfg, "~/corp"); err == nil {
		t.Error("expected error removing unmapped path")
	}
}

func TestRemoveLastDirBlockedWhileGlobalExists(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles() // "oss" has no dirs yet: it is the global profile
	if _, err := Add(cfg, "~/corp", "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(cfg, "~/corp"); err == nil {
		t.Error("expected error: removing the last dir would create a second global profile")
	}
	// Mapping the global profile first frees the constraint.
	if _, err := Add(cfg, "~/oss", "oss", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(cfg, "~/corp"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if got := cfg.GitProfiles["work"].Directories; len(got) != 0 {
		t.Errorf("work directories = %v, want empty", got)
	}
}

func TestRemoveLastDirAllowedWithoutGlobal(t *testing.T) {
	testHome(t)
	cfg := cfgWithProfiles()
	if _, err := Add(cfg, "~/corp", "work", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Add(cfg, "~/oss", "oss", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Remove(cfg, "~/corp"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
}

func TestShorten(t *testing.T) {
	home := testHome(t)
	if got := Shorten(filepath.Join(home, "a/b")); got != "~/a/b" {
		t.Errorf("Shorten = %q, want ~/a/b", got)
	}
	if got := Shorten("/usr/local"); got != "/usr/local" {
		t.Errorf("Shorten = %q, want unchanged", got)
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil && resolved != home {
		if got := Shorten(filepath.Join(resolved, "x")); got != "~/x" {
			t.Errorf("Shorten(resolved home) = %q, want ~/x", got)
		}
	}
}

func homeDir() (string, error) {
	return os.UserHomeDir()
}
