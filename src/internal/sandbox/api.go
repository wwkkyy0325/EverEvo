// Package sandbox provides process-level isolation for agent execution.
//
// Public API: Profile, Run, PrepareWorkspace, CleanupWorkspace.
// Implementation details live in sandbox/internal/ (Go-compiler-enforced private).
package sandbox

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"everevo/internal/storage"
)

// Profile configures a sandbox instance.
type Profile struct {
	ReadPaths     []string `json:"readPaths"`
	WritePaths    []string `json:"writePaths"`
	MaxMemoryMB   int      `json:"maxMemoryMB"`
	TimeoutSec    int      `json:"timeoutSec"`
	AllowNetwork  bool     `json:"allowNetwork"`
	AllowedCmds   []string `json:"allowedCommands,omitempty"`
	Env           []string `json:"env,omitempty"`
}

// Result holds the outcome of a sandboxed command.
type Result struct {
	ExitCode int    `json:"exitCode"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	Duration string `json:"duration"`
}

// DefaultProfile returns a safe default config.
func DefaultProfile() Profile {
	return Profile{
		ReadPaths:    []string{storage.DataDir()},
		MaxMemoryMB:  2048,
		TimeoutSec:   300,
		AllowNetwork: false,
	}
}

// WorkspaceProfile returns a config scoped to a session workspace.
func WorkspaceProfile(sessionID string) Profile {
	ws := storage.WorkspaceDir(sessionID)
	return Profile{
		ReadPaths:    []string{ws, storage.DataDir()},
		WritePaths:   []string{ws},
		MaxMemoryMB:  2048,
		TimeoutSec:   300,
		AllowNetwork: false,
	}
}

// Run executes a command inside the sandbox.
func Run(profile Profile, command string, args ...string) (Result, error) {
	start := time.Now()

	for _, p := range profile.WritePaths {
		os.MkdirAll(p, 0755)
	}

	cmd, err := startSandbox(profile, command, args...)
	if err != nil {
		return Result{}, fmt.Errorf("sandbox start: %w", err)
	}

	log.Printf("[sandbox] %s %v (timeout=%ds net=%v)", command, args, profile.TimeoutSec, profile.AllowNetwork)

	result := waitSandbox(cmd, profile.TimeoutSec)
	result.Duration = time.Since(start).Round(time.Millisecond).String()
	return result, nil
}

// PrepareWorkspace creates a session workspace directory. Returns the path.
func PrepareWorkspace(sessionID string) (string, error) {
	ws := storage.WorkspaceDir(sessionID)
	if err := os.MkdirAll(ws, 0755); err != nil {
		return "", fmt.Errorf("prepare workspace: %w", err)
	}

	profile := WorkspaceProfile(sessionID)
	configDir := filepath.Join(storage.SessionsDir(), sessionID)
	os.MkdirAll(configDir, 0755)
	data, _ := json.MarshalIndent(profile, "", "  ")
	os.WriteFile(filepath.Join(configDir, "sandbox.json"), data, 0644)

	log.Printf("[sandbox] workspace ready: %s", ws)
	return ws, nil
}

// CleanupWorkspace removes a session workspace.
func CleanupWorkspace(sessionID string) error {
	dir := filepath.Join(storage.SessionsDir(), sessionID)
	log.Printf("[sandbox] cleanup: %s", dir)
	return os.RemoveAll(dir)
}
