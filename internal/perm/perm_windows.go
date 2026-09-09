//go:build windows

package perm

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// icacls runs icacls against path and returns its combined output.
func icacls(path string, args ...string) (string, error) {
	cmd := exec.Command("icacls", append([]string{path}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("icacls %s: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// SetPrivate restricts path to the current user and SYSTEM with an
// explicit ACL, mirroring what OpenSSH for Windows requires of private
// keys. Inherited ACEs are dropped first.
func SetPrivate(path string) error {
	u, err := currentUser()
	if err != nil {
		return err
	}
	_, err = icacls(path, "/inheritance:r", "/grant:r", u+":F", "/grant:r", "SYSTEM:F")
	return err
}

// SetStandard, SetDirPrivate and SetExecutable are no-ops: Windows
// files already carry usable default ACLs and there is no exec bit.
func SetStandard(string) error   { return nil }
func SetDirPrivate(string) error { return nil }
func SetExecutable(string) error { return nil }

// Private reports whether path's ACL is explicit (no inherited ACEs),
// which is what SetPrivate produces. The "(I)" marker in icacls output
// is locale-independent.
func Private(path string) (bool, string) {
	out, err := icacls(path)
	if err != nil {
		return false, "cannot verify ACL"
	}
	if strings.Contains(out, "(I)") {
		return false, "inherited ACL"
	}
	return true, "restricted ACL"
}

// Standard and DirPrivate cannot be meaningfully verified on Windows;
// default ACLs are treated as fine.
func Standard(string) (bool, string)   { return true, "default" }
func DirPrivate(string) (bool, string) { return true, "default" }

// ApplyMode applies a POSIX-style mode to a freshly written file:
// modes without group/other access (e.g. 0600) get the restrictive
// ACL, looser modes keep the default ACL.
func ApplyMode(path string, mode os.FileMode) error {
	if mode&0o077 == 0 {
		return SetPrivate(path)
	}
	return nil
}

func currentUser() (string, error) {
	if u, err := user.Current(); err == nil {
		return u.Username, nil
	}
	if name := os.Getenv("USERNAME"); name != "" {
		return name, nil
	}
	return "", fmt.Errorf("resolve current user for ACL grant")
}
