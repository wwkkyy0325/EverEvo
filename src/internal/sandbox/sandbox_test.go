package sandbox

import (
	"os"
	"path/filepath"
	"testing"

	"everevo/internal/storage"
)

func TestDefaultProfile(t *testing.T) {
	p := DefaultProfile()
	if p.MaxMemoryMB != 2048 {
		t.Errorf("DefaultProfile.MaxMemoryMB = %d, want 2048", p.MaxMemoryMB)
	}
	if p.TimeoutSec != 300 {
		t.Errorf("DefaultProfile.TimeoutSec = %d, want 300", p.TimeoutSec)
	}
	if p.AllowNetwork {
		t.Error("DefaultProfile.AllowNetwork should be false")
	}
	if len(p.ReadPaths) == 0 {
		t.Error("DefaultProfile.ReadPaths should not be empty")
	}
}

func TestWorkspaceProfile(t *testing.T) {
	id := "test-session-123"
	p := WorkspaceProfile(id)

	ws := storage.WorkspaceDir(id)
	found := false
	for _, wp := range p.WritePaths {
		if wp == ws {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("WorkspaceProfile.WritePaths should contain workspace dir %s, got %v", ws, p.WritePaths)
	}

	if p.AllowNetwork {
		t.Error("WorkspaceProfile.AllowNetwork should be false by default")
	}
}

func TestPrepareWorkspace(t *testing.T) {
	id := "test-prepare-ws"
	ws, err := PrepareWorkspace(id)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	defer os.RemoveAll(filepath.Join(storage.SessionsDir(), id))

	if _, err := os.Stat(ws); os.IsNotExist(err) {
		t.Errorf("workspace dir not created: %s", ws)
	}

	// Verify sandbox.json was written.
	configPath := filepath.Join(storage.SessionsDir(), id, "sandbox.json")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Errorf("sandbox.json not created: %s", configPath)
	}
}

func TestCleanupWorkspace(t *testing.T) {
	id := "test-cleanup-ws"
	_, err := PrepareWorkspace(id)
	if err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}

	if err := CleanupWorkspace(id); err != nil {
		t.Fatalf("CleanupWorkspace: %v", err)
	}

	sessionDir := filepath.Join(storage.SessionsDir(), id)
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Errorf("session dir should be removed: %s", sessionDir)
	}
}

func TestResultFields(t *testing.T) {
	r := Result{ExitCode: 0, Stdout: "hello", Stderr: "", Duration: "1s"}
	if r.ExitCode != 0 {
		t.Errorf("Result.ExitCode = %d, want 0", r.ExitCode)
	}
	if r.Stdout != "hello" {
		t.Errorf("Result.Stdout = %s, want hello", r.Stdout)
	}
}
