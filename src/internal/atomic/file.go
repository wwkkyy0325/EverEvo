// Package atomic provides crash-safe file operations.
//
// WriteFile writes data to a temporary file in the same directory,
// then renames it over the destination. On crash, either the old
// file survives intact or the new file is complete — never a
// partial write.
package atomic

import (
	"fmt"
	"os"
	"path/filepath"
)

// WriteFile atomically writes data to the named file.
// Uses temp-file + rename so the target is never left in a
// partially-written state.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".atomic-*")
	if err != nil {
		return fmt.Errorf("atomic write: create temp: %w", err)
	}
	tmpName := tmp.Name()

	// Write data
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("atomic write: write: %w", err)
	}
	// Ensure data hits disk before rename
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("atomic write: sync: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomic write: close: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomic write: chmod: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("atomic write: rename: %w", err)
	}
	return nil
}
