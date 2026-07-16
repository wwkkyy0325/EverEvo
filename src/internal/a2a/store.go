package a2a

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"everevo/internal/atomic"
)

// RemoteAgentConfig represents a persisted remote A2A agent connection.
type RemoteAgentConfig struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	URL         string     `json:"url"`
	Secret      string     `json:"secret,omitempty"` // Feishu-style HMAC secret for signing requests to this agent
	Status      string     `json:"status"`           // "connected" | "disconnected" | "error"
	Error       string     `json:"error,omitempty"`
	Card        *AgentCard `json:"card,omitempty"`
	ConnectedAt int64      `json:"connectedAt,omitempty"`
}

// Store persists remote agent connections to disk.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore creates a store backed by the given file path.
func NewStore(dataDir string) *Store {
	return &Store{path: filepath.Join(dataDir, "a2a_agents.json")}
}

// Load reads the persisted agent list.
func (s *Store) Load() ([]RemoteAgentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []RemoteAgentConfig{}, nil
		}
		return nil, fmt.Errorf("a2a store: read: %w", err)
	}

	var agents []RemoteAgentConfig
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, fmt.Errorf("a2a store: parse: %w", err)
	}
	if agents == nil {
		agents = []RemoteAgentConfig{}
	}
	return agents, nil
}

// Save persists the agent list.
func (s *Store) Save(agents []RemoteAgentConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return fmt.Errorf("a2a store: marshal: %w", err)
	}
	if err := atomic.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("a2a store: write: %w", err)
	}
	return nil
}

// nowMillis returns the current unix timestamp in milliseconds.
func nowMillis() int64 {
	return time.Now().UnixMilli()
}
