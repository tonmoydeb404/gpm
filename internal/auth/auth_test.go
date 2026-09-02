package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseGreeting(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Hi octocat! You've successfully authenticated, but GitHub does not provide shell access.", "octocat"},
		{"PTY allocation request failed\nHi jane-doe! ...", "jane-doe"},
		{"git@github.com: Permission denied (publickey).", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := ParseGreeting(c.in); got != c.want {
			t.Errorf("ParseGreeting(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTestGitHubSuccess(t *testing.T) {
	orig := runSSH
	defer func() { runSSH = orig }()
	runSSH = func(args []string) (string, int, error) {
		if !contains(args, "git@github.com") {
			t.Errorf("expected host argument, got %v", args)
		}
		if !contains(args, "IdentitiesOnly=yes") {
			t.Errorf("expected IdentitiesOnly, got %v", args)
		}
		return "Hi octocat! You've successfully authenticated.\n", 1, nil
	}
	res, err := TestGitHub("/fake/key")
	if err != nil {
		t.Fatalf("TestGitHub() error = %v", err)
	}
	if !res.OK || res.Username != "octocat" {
		t.Errorf("result = %+v", res)
	}
}

func TestTestGitHubDenied(t *testing.T) {
	orig := runSSH
	defer func() { runSSH = orig }()
	runSSH = func(args []string) (string, int, error) {
		return "git@github.com: Permission denied (publickey).", 255, nil
	}
	res, err := TestGitHub("/fake/key")
	if err == nil {
		t.Fatal("expected error for denied key")
	}
	if res.OK {
		t.Error("result must not be OK")
	}
	if !strings.Contains(err.Error(), "Permission denied") {
		t.Errorf("error should carry ssh output, got %v", err)
	}
}

func TestTestGitHubBinaryMissing(t *testing.T) {
	orig := runSSH
	defer func() { runSSH = orig }()
	runSSH = func(args []string) (string, int, error) {
		return "", -1, errors.New("exec: \"ssh\": executable file not found in $PATH")
	}
	if _, err := TestGitHub("/fake/key"); err == nil {
		t.Fatal("expected error when ssh cannot run")
	}
}

func TestSandboxSSHFlags(t *testing.T) {
	orig := runSSH
	defer func() { runSSH = orig }()
	var gotArgs []string
	runSSH = func(args []string) (string, int, error) {
		gotArgs = args
		return "Hi octocat!\n", 1, nil
	}
	dev := t.TempDir()
	t.Setenv("GPM_HOME", dev)
	if _, err := TestGitHub("/fake/key"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(gotArgs, " ")
	if !strings.Contains(joined, "-F "+filepath.Join(dev, ".ssh", "config")) {
		t.Errorf("sandbox ssh must use -F with the sandbox config, args: %v", gotArgs)
	}
	if !strings.Contains(joined, "UserKnownHostsFile="+filepath.Join(dev, ".ssh", "known_hosts")) {
		t.Errorf("sandbox ssh must isolate known_hosts, args: %v", gotArgs)
	}

	os.Unsetenv("GPM_HOME")
	if _, err := TestGitHub("/fake/key"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(gotArgs, " "), "-F ") {
		t.Errorf("non-sandbox ssh must not pass -F, args: %v", gotArgs)
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
