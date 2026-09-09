// Package managed implements the marker-delimited block editing used
// for foreign config files (~/.ssh/config, ~/.gitconfig). Everything
// between the markers is owned by gpm; content outside the markers is
// preserved byte-for-byte.
package managed

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tonmoydeb404/gpm/internal/config"
)

const (
	// Begin starts the gpm-managed block.
	Begin = "# BEGIN GPM MANAGED BLOCK (managed by gpm; edits inside will be overwritten)"
	// End terminates the gpm-managed block.
	End = "# END GPM MANAGED BLOCK"
)

// Replace swaps the managed block inside content for block. A missing
// block is appended (or skipped when block is empty); content outside
// the markers is never touched.
func Replace(content, block string) string {
	begin := strings.Index(content, Begin)
	if begin == -1 {
		if block == "" {
			return content
		}
		if strings.TrimSpace(content) == "" {
			return block
		}
		return strings.TrimRight(content, "\n") + "\n\n" + block
	}

	endRel := strings.Index(content[begin:], End)
	if endRel == -1 {
		// Unbalanced markers: leave the file alone rather than corrupt it.
		return content
	}
	end := begin + endRel + len(End)
	if end < len(content) && content[end] == '\n' {
		end++
	}
	before, after := content[:begin], content[end:]

	if block == "" {
		// Collapse the blank line the block used to sit behind.
		return strings.TrimRight(before, "\n") + "\n" + strings.TrimLeft(after, "\n")
	}
	return before + block + after
}

// Update rewrites the managed block of the config file at path,
// backing up the previous file first. Missing files are created with
// defaultPerm; existing files keep their permissions. Writes are
// skipped when nothing changes.
func Update(path, block string, defaultPerm os.FileMode) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if errors.Is(err, os.ErrNotExist) && block == "" {
		return nil // nothing to create and nothing to remove
	}
	newContent := Replace(string(existing), block)
	if newContent == string(existing) {
		return nil
	}
	perm := defaultPerm
	if info, statErr := os.Stat(path); statErr == nil {
		perm = info.Mode().Perm()
	}
	if err := config.BackupFile(path); err != nil {
		return fmt.Errorf("backup %s: %w", path, err)
	}
	return config.AtomicWrite(path, []byte(newContent), perm)
}

// HasBlock reports whether the file contains the managed markers.
func HasBlock(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	content := string(data)
	return strings.Contains(content, Begin) && strings.Contains(content, End), nil
}
