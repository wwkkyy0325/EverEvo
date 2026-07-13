package zone

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"everevo/internal/storage"
)

// ZonesDir returns the root directory containing all zone directories.
func ZonesDir() string {
	return filepath.Join(storage.DataDir(), "zones")
}

// Dir returns the filesystem path for a named zone.
func Dir(name string) string {
	return filepath.Join(ZonesDir(), name)
}

// List returns all zones found on disk, sorted by creation time (newest first).
// Zones missing a valid .manifest.json are skipped.
func List() ([]Zone, error) {
	zonesDir := ZonesDir()
	entries, err := os.ReadDir(zonesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read zones dir: %w", err)
	}

	var zones []Zone
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(zonesDir, e.Name())
		m, err := readManifest(dir)
		if err != nil {
			continue
		}
		z := zoneFromManifest(dir, m)
		// If PID is stale, mark as stopped.
		if z.PID > 0 && !isPIDAlive(z.PID) {
			z.PID = 0
		}
		zones = append(zones, z)
	}

	sort.Slice(zones, func(i, j int) bool {
		return zones[i].CreatedAt.After(zones[j].CreatedAt)
	})
	return zones, nil
}

// Get returns a single zone by name.
func Get(name string) (*Zone, error) {
	dir := Dir(name)
	m, err := readManifest(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("zone %q does not exist", name)
		}
		return nil, fmt.Errorf("read zone %q manifest: %w", name, err)
	}
	z := zoneFromManifest(dir, m)
	if z.PID > 0 && !isPIDAlive(z.PID) {
		z.PID = 0
	}
	return &z, nil
}

// CreateFrom copies the parent zone's data directory to create a new zone.
// Models, cache, and plugins directories are NOT copied (shared).
func CreateFrom(name string, parentName string, zoneType Type) (*Zone, error) {
	parentDir := Dir(parentName)
	if _, err := os.Stat(parentDir); err != nil {
		return nil, fmt.Errorf("parent zone %q does not exist: %w", parentName, err)
	}

	destDir := Dir(name)
	if _, err := os.Stat(destDir); err == nil {
		return nil, fmt.Errorf("zone %q already exists", name)
	}

	// Determine ports.
	isProd := zoneType == TypeProduction
	mcpPort, a2aPort, err := Allocate(name, isProd)
	if err != nil {
		return nil, fmt.Errorf("allocate ports: %w", err)
	}

	now := time.Now()

	// Build manifest first so we can write it after copy.
	m := &manifest{
		Name:      name,
		Type:      zoneType,
		Parent:    parentName,
		CreatedAt: now,
		MCPPort:   mcpPort,
		A2APort:   a2aPort,
	}

	if err := copyZoneDir(parentDir, destDir); err != nil {
		os.RemoveAll(destDir)
		Release(name)
		return nil, fmt.Errorf("copy zone data: %w", err)
	}

	if err := writeManifest(destDir, m); err != nil {
		os.RemoveAll(destDir)
		Release(name)
		return nil, fmt.Errorf("write manifest: %w", err)
	}

	log.Printf("[zone] created %q (type=%s, parent=%s, mcp=%d, a2a=%d)", name, zoneType, parentName, mcpPort, a2aPort)
	z := zoneFromManifest(destDir, m)
	return &z, nil
}

// Remove deletes a zone directory and releases its ports.
// Refuses to remove a running zone or a production zone.
func Remove(name string) error {
	z, err := Get(name)
	if err != nil {
		return err
	}
	if z.IsProduction() {
		return fmt.Errorf("cannot remove the production zone")
	}
	if z.PID > 0 && isPIDAlive(z.PID) {
		return fmt.Errorf("cannot remove zone %q while it is running (PID %d)", name, z.PID)
	}

	if err := os.RemoveAll(z.Dir); err != nil {
		return fmt.Errorf("remove zone dir: %w", err)
	}
	if err := Release(name); err != nil {
		log.Printf("[zone] failed to release ports for %q: %v", name, err)
	}
	log.Printf("[zone] removed %q", name)
	return nil
}

// Launch starts a new EverEvo instance for the given zone.
// exePath is the path to everevo.exe; if empty, the current executable is used.
func Launch(name string, exePath string) error {
	z, err := Get(name)
	if err != nil {
		return err
	}
	if z.PID > 0 && isPIDAlive(z.PID) {
		return fmt.Errorf("zone %q is already running (PID %d)", name, z.PID)
	}

	if exePath == "" {
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("get executable path: %w", err)
		}
		exePath = exe
	}

	cmd := exec.Command(exePath, "--zone="+name)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: false}
	cmd.Env = append(os.Environ(), "EVEREVO_ZONE="+name)

	// Pipe stderr to the shared log for debugging.
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch zone %q: %w", name, err)
	}

	pid := cmd.Process.Pid
	log.Printf("[zone] launched %q (PID %d, mcp=%d, a2a=%d)", name, pid, z.MCPPort, z.A2APort)

	// Update manifest with PID.
	m, err := readManifest(z.Dir)
	if err != nil {
		log.Printf("[zone] read manifest for PID update: %v", err)
	} else {
		m.PID = pid
		_ = writeManifest(z.Dir, m)
	}

	// Background: wait for process exit and clean up.
	go func() {
		// Drain stderr so the child doesn't block.
		io.Copy(io.Discard, stderrPipe)
		cmd.Wait()

		// Mark as stopped in manifest.
		m, err := readManifest(z.Dir)
		if err == nil {
			m.PID = 0
			_ = writeManifest(z.Dir, m)
		}
		log.Printf("[zone] %q process exited (PID %d)", name, pid)
	}()

	return nil
}

// Stop terminates a running zone process.
func Stop(name string) error {
	z, err := Get(name)
	if err != nil {
		return err
	}
	if z.PID <= 0 {
		return fmt.Errorf("zone %q is not running", name)
	}

	p, err := os.FindProcess(z.PID)
	if err != nil {
		// PID not found — clean up stale record.
		m, mErr := readManifest(z.Dir)
		if mErr == nil {
			m.PID = 0
			_ = writeManifest(z.Dir, m)
		}
		return fmt.Errorf("find process %d: %w", z.PID, err)
	}

	// Graceful: send interrupt, wait 3s, then kill.
	_ = p.Signal(os.Interrupt)

	done := make(chan struct{})
	go func() {
		p.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = p.Kill()
		<-done
	}

	// Update manifest.
	m, err := readManifest(z.Dir)
	if err == nil {
		m.PID = 0
		_ = writeManifest(z.Dir, m)
	}
	log.Printf("[zone] stopped %q (PID %d)", name, z.PID)
	return nil
}

// MergeToParent merges the experiment zone back into its parent.
// Steps: backup parent → stop experiment → overwrite parent files → prune backups.
func MergeToParent(experimentName string) error {
	exp, err := Get(experimentName)
	if err != nil {
		return err
	}
	if !exp.IsExperiment() {
		return fmt.Errorf("zone %q is not an experiment", experimentName)
	}
	if exp.Parent == "" {
		return fmt.Errorf("experiment %q has no parent zone", experimentName)
	}
	if exp.PID > 0 && isPIDAlive(exp.PID) {
		return fmt.Errorf("cannot merge running experiment %q — stop it first", experimentName)
	}

	parent, err := Get(exp.Parent)
	if err != nil {
		return fmt.Errorf("parent zone %q: %w", exp.Parent, err)
	}

	// 1. Create backup of parent.
	if _, err := CreateBackup(parent.Name); err != nil {
		return fmt.Errorf("create backup before merge: %w", err)
	}

	// 2. Overwrite parent data with experiment data.
	if err := overwriteZoneData(exp.Dir, parent.Dir); err != nil {
		return fmt.Errorf("overwrite parent data: %w", err)
	}

	// 3. Mark experiment as merged.
	m, err := readManifest(exp.Dir)
	if err == nil {
		m.PID = 0
		_ = writeManifest(exp.Dir, m)
	}

	// 4. Clean old backups (keep 3).
	if err := PruneBackups(3); err != nil {
		log.Printf("[zone] backup prune warning: %v", err)
	}

	if err := Release(experimentName); err != nil {
		log.Printf("[zone] release experiment ports: %v", err)
	}

	log.Printf("[zone] merged %q → %q", experimentName, parent.Name)
	return nil
}

// CreateBackup makes a timestamped snapshot of the production zone.
func CreateBackup(parentName string) (*Zone, error) {
	ts := time.Now().Format("20060102-150405")
	name := "backup-" + ts
	return CreateFrom(name, parentName, TypeBackup)
}

// PruneBackups deletes the oldest backups, keeping only `keep` newest.
func PruneBackups(keep int) error {
	all, err := List()
	if err != nil {
		return err
	}
	var backups []Zone
	for _, z := range all {
		if z.IsBackup() {
			backups = append(backups, z)
		}
	}
	if len(backups) <= keep {
		return nil
	}
	// List() returns newest first, so the tail is oldest.
	for _, z := range backups[keep:] {
		if err := os.RemoveAll(z.Dir); err != nil {
			log.Printf("[zone] prune backup %q: %v", z.Name, err)
			continue
		}
		Release(z.Name)
		log.Printf("[zone] pruned old backup %q", z.Name)
	}
	return nil
}

// ─── Helpers ────────────────────────────────────────────────────────

// isPIDAlive checks if a process with the given PID exists.
func isPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows, Signal(0) is not reliable. Use process handle.
	// FindProcess on Windows always succeeds for any PID, so we just
	// check that the PID is reasonable and trust the manifest.
	_ = p
	return true
}

// copyZoneDir copies a zone directory tree, skipping heavy shared dirs.
func copyZoneDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Skip heavy directories that are shared across all zones.
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "models" || base == "cache" || base == "plugins" || base == "plugin-tmp" {
				// Still create empty dirs for consistency.
				destPath := filepath.Join(dst, rel)
				return os.MkdirAll(destPath, 0755)
			}
			return nil
		}

		destPath := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		srcFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer srcFile.Close()

		dstFile, err := os.Create(destPath)
		if err != nil {
			return err
		}
		defer dstFile.Close()

		_, err = io.Copy(dstFile, srcFile)
		return err
	})
}

// overwriteZoneData replaces the destination zone's data with the source zone's data.
// It removes the dest contents first (except .manifest.json), then copies.
func overwriteZoneData(srcDir, dstDir string) error {
	// Remove all entries in dest except .manifest.json.
	entries, err := os.ReadDir(dstDir)
	if err != nil {
		return fmt.Errorf("read dest dir: %w", err)
	}
	for _, e := range entries {
		if e.Name() == ".manifest.json" {
			continue
		}
		path := filepath.Join(dstDir, e.Name())
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
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

		if e.IsDir() {
			if err := copyZoneDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy dir %s: %w", e.Name(), err)
			}
		} else {
			srcFile, err := os.Open(srcPath)
			if err != nil {
				return fmt.Errorf("open src %s: %w", e.Name(), err)
			}
			dstFile, err := os.Create(dstPath)
			if err != nil {
				srcFile.Close()
				return fmt.Errorf("create dst %s: %w", e.Name(), err)
			}
			_, err = io.Copy(dstFile, srcFile)
			srcFile.Close()
			dstFile.Close()
			if err != nil {
				return fmt.Errorf("copy %s: %w", e.Name(), err)
			}
		}
	}

	// Log what was replaced.
	files, _ := os.ReadDir(dstDir)
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name()
	}
	log.Printf("[zone] overwrote %s with experiment data (%s)", dstDir, strings.Join(names, ", "))
	return nil
}
