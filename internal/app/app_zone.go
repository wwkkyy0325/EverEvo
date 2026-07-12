//go:build windows

package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"everevo/internal/zone"
)

// ─── Zone management API (exposed to frontend via Wails bind) ───────

// GetCurrentZone returns info about this instance's zone.
func (a *App) GetCurrentZone() zone.Zone {
	if a.zone != nil {
		return *a.zone
	}
	return zone.Zone{Name: "unknown", Type: zone.TypeProduction}
}

// ListZones returns all zones on disk.
func (a *App) ListZones() ([]zone.Zone, error) {
	return zone.List()
}

// CreateExperiment copies the production zone to a new experiment zone.
func (a *App) CreateExperiment(name string) (*zone.Zone, error) {
	if name == "" {
		return nil, fmt.Errorf("experiment name is required")
	}
	// Only allow experiments to be created from production.
	return zone.CreateFrom(name, "production", zone.TypeExperiment)
}

// LaunchZone starts an EverEvo instance for the named zone.
func (a *App) LaunchZone(name string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	return zone.Launch(name, exe)
}

// StopZone terminates a running zone's process.
func (a *App) StopZone(name string) error {
	return zone.Stop(name)
}

// MergeZone merges an experiment zone back into its parent (production).
// It creates a backup of production first, then overwrites production
// with the experiment's data.
func (a *App) MergeZone(name string) error {
	return zone.MergeToParent(name)
}

// DiscardZone removes an experiment or backup zone.
func (a *App) DiscardZone(name string) error {
	z, err := zone.Get(name)
	if err != nil {
		return err
	}
	if z.IsProduction() {
		return fmt.Errorf("cannot discard the production zone")
	}
	return zone.Remove(name)
}

// ─── Backup management ──────────────────────────────────────────────

// ListBackups returns all backup zones, newest first.
func (a *App) ListBackups() ([]zone.Zone, error) {
	all, err := zone.List()
	if err != nil {
		return nil, err
	}
	var backups []zone.Zone
	for _, z := range all {
		if z.IsBackup() {
			backups = append(backups, z)
		}
	}
	return backups, nil
}

// CreateBackup makes a timestamped backup of the production zone.
func (a *App) CreateBackup() (*zone.Zone, error) {
	return zone.CreateBackup("production")
}

// RestoreBackup replaces the production zone with a backup's data.
// It first backs up the current production, then overwrites it.
func (a *App) RestoreBackup(backupName string) error {
	// Safety: only allow restoring from a backup-type zone.
	b, err := zone.Get(backupName)
	if err != nil {
		return err
	}
	if !b.IsBackup() {
		return fmt.Errorf("%q is not a backup zone", backupName)
	}
	// Create a safety backup of current production before restoring.
	if _, err := zone.CreateBackup("production"); err != nil {
		return fmt.Errorf("safety backup before restore: %w", err)
	}
	// Overwrite production data with the backup's data.
	if err := overwriteZoneDataPreserveManifest(b.Dir, zone.Dir("production")); err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}
	return nil
}

// DeleteBackup removes a backup zone entirely.
func (a *App) DeleteBackup(name string) error {
	return zone.Remove(name)
}

// copyPath copies a file or directory tree from src to dst.
func copyPath(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		return copyDir(src, dst)
	}
	return copyFile(src, dst)
}

// copyFile copies a single file from src to dst.
func copyFile(src, dst string) error {
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	d, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = io.Copy(d, s)
	return err
}

// copyDir recursively copies a directory tree.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if err := copyPath(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

// overwriteZoneDataPreserveManifest replaces the destination zone's data
// with the source zone's data while keeping the destination's .manifest.json.
func overwriteZoneDataPreserveManifest(srcDir, dstDir string) error {
	// Remove all entries in dest except .manifest.json.
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		return fmt.Errorf("read dest dir: %w", err)
	}
	for _, e := range entries {
		if e.Name() == ".manifest.json" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dstDir, e.Name())); err != nil {
			return fmt.Errorf("remove %s: %w", e.Name(), err)
		}
	}
	// Copy all from src (except .manifest.json) to dest.
	entries, err = os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read src dir: %w", err)
	}
	for _, e := range entries {
		if e.Name() == ".manifest.json" {
			continue
		}
		srcPath := filepath.Join(srcDir, e.Name())
		dstPath := filepath.Join(dstDir, e.Name())
		if err := copyPath(srcPath, dstPath); err != nil {
			return fmt.Errorf("copy %s: %w", e.Name(), err)
		}
	}
	return nil
}
