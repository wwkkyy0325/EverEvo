// Package zone provides runtime-zone isolation for EverEvo instances.
//
// Each instance runs in a named "zone" under data/zones/<name>/ relative to the
// project root. Zones are one of three types:
//   - production: the live/stable instance (default)
//   - experiment: a sandboxed copy for testing self-modifications
//   - backup: a snapshot of production before a merge
//
// The .manifest.json file in each zone directory records metadata.
package zone

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"everevo/internal/atomic"
)

// Type classifies a zone.
type Type string

const (
	TypeProduction Type = "production"
	TypeExperiment Type = "experiment"
	TypeBackup     Type = "backup"
)

// Zone is a named, isolated instance environment.
type Zone struct {
	Name      string    `json:"name"`
	Dir       string    `json:"dir"`
	Type      Type      `json:"type"`
	Parent    string    `json:"parent"` // source zone name for experiments
	PID       int       `json:"pid"`
	MCPPort   int       `json:"mcpPort"`
	A2APort   int       `json:"a2aPort"`
	CreatedAt time.Time `json:"createdAt"`
}

// IsProduction reports whether this zone is the production zone.
func (z Zone) IsProduction() bool { return z.Type == TypeProduction }

// IsExperiment reports whether this zone is an experiment.
func (z Zone) IsExperiment() bool { return z.Type == TypeExperiment }

// IsBackup reports whether this zone is a backup.
func (z Zone) IsBackup() bool { return z.Type == TypeBackup }

// Manifest is the on-disk format for .manifest.json.
type Manifest struct {
	Name      string    `json:"name"`
	Type      Type      `json:"type"`
	Parent    string    `json:"parent,omitempty"`
	PID       int       `json:"pid,omitempty"`
	MCPPort   int       `json:"mcpPort,omitempty"`
	A2APort   int       `json:"a2aPort,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// manifest is the internal alias for Manifest.
type manifest = Manifest

// ReadManifest reads the .manifest.json from a zone directory.
func ReadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".manifest.json"))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// WriteManifest writes a .manifest.json to a zone directory atomically.
func WriteManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomic.WriteFile(filepath.Join(dir, ".manifest.json"), data, 0644)
}

// readManifest is the internal shorthand.
func readManifest(dir string) (*manifest, error) {
	m, err := ReadManifest(dir)
	if err != nil {
		return nil, err
	}
	return (*manifest)(m), nil
}

// writeManifest is the internal shorthand.
func writeManifest(dir string, m *manifest) error {
	return WriteManifest(dir, (*Manifest)(m))
}

// zoneFromManifest converts a manifest + directory into a Zone.
func zoneFromManifest(dir string, m *manifest) Zone {
	return Zone{
		Name:      m.Name,
		Dir:       dir,
		Type:      m.Type,
		Parent:    m.Parent,
		PID:       m.PID,
		MCPPort:   m.MCPPort,
		A2APort:   m.A2APort,
		CreatedAt: m.CreatedAt,
	}
}
