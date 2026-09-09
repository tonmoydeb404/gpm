//go:build !windows

// Package perm applies and verifies the file permissions gpm promises:
// private keys restricted to the owner (0600), public files 0644,
// ~/.ssh 0700, hooks 0755. The Windows build of this package manages
// NTFS ACLs via icacls instead, where POSIX modes do not exist.
package perm

import (
	"fmt"
	"os"
)

// SetPrivate restricts path to the owner (0600).
func SetPrivate(path string) error { return os.Chmod(path, 0o600) }

// SetStandard makes path owner-writable and world-readable (0644).
func SetStandard(path string) error { return os.Chmod(path, 0o644) }

// SetDirPrivate restricts a directory to the owner (0700).
func SetDirPrivate(path string) error { return os.Chmod(path, 0o700) }

// SetExecutable marks path executable (0755).
func SetExecutable(path string) error { return os.Chmod(path, 0o755) }

// Private reports whether path has 0600 permissions. The second
// return value describes the current state for diagnostics.
func Private(path string) (bool, string) { return modeIs(path, 0o600) }

// Standard reports whether path has 0644 permissions.
func Standard(path string) (bool, string) { return modeIs(path, 0o644) }

// DirPrivate reports whether the directory has 0700 permissions.
func DirPrivate(path string) (bool, string) { return modeIs(path, 0o700) }

// ApplyMode applies a desired POSIX mode to a freshly written file.
func ApplyMode(path string, mode os.FileMode) error { return os.Chmod(path, mode) }

func modeIs(path string, want os.FileMode) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	got := info.Mode().Perm()
	return got == want, fmt.Sprintf("%03o", got)
}
