package a2a

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// EventCallback is called when agent state changes, for Wails EventsEmit.
type EventCallback func(event string, data interface{})

// Manager orchestrates the A2A server and multiple client connections.
type Manager struct {
	mu           sync.RWMutex
	server       *Server
	clients      map[string]*Client
	agents       map[string]*RemoteAgentConfig
	store        *Store
	onEvent      EventCallback
	card         AgentCard
	executor     TaskExecutor
	serverSecret string
	done         chan struct{} // closed in Close to stop the keep-alive pinger
}

// NewManager creates an A2A manager.
func NewManager(dataDir string, card AgentCard, executor TaskExecutor, onEvent EventCallback, serverSecret string) *Manager {
	m := &Manager{
		clients:      make(map[string]*Client),
		agents:       make(map[string]*RemoteAgentConfig),
		store:        NewStore(dataDir),
		onEvent:      onEvent,
		card:         card,
		executor:     executor,
		serverSecret: serverSecret,
		done:         make(chan struct{}),
	}
	go m.pingLoop()
	return m
}

// ── Server ──

// StartServer creates and starts the A2A HTTP server.
func (m *Manager) StartServer(port int) error {
	m.mu.Lock()
	oldSrv := m.server
	newSrv := NewServer(m.card, m.executor, m.serverSecret)
	if err := newSrv.Start(port); err != nil {
		m.mu.Unlock()
		return err
	}
	m.server = newSrv
	m.mu.Unlock()

	// Stop the previous server outside the lock — server.Stop may block for up
	// to its Shutdown timeout, and we must not hold m.mu that long. emit also
	// stays outside the lock so a slow EventsEmit can't stall manager calls.
	if oldSrv != nil {
		_ = oldSrv.Stop()
	}
	m.emit("agent:changed", map[string]string{"action": "server_started"})
	return nil
}

// StopServer stops the A2A server if running.
func (m *Manager) StopServer() error {
	m.mu.Lock()
	srv := m.server
	if srv == nil || !srv.IsRunning() {
		m.mu.Unlock()
		return nil
	}
	m.server = nil
	m.mu.Unlock()

	// Stop the HTTP server outside the manager lock — server.Stop can block for
	// up to its Shutdown timeout, and holding m.mu that long blocks every
	// concurrent manager call (ListRemoteAgents / SendTask / ...).
	err := srv.Stop()
	m.emit("agent:changed", map[string]string{"action": "server_stopped"})
	return err
}

// ServerStatus returns the server status.
func (m *Manager) ServerStatus() ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.server == nil {
		return ServerStatus{Running: false}
	}
	return m.server.Status()
}

// UpdateCard updates the agent card and restarts the server if running.
func (m *Manager) UpdateCard(card AgentCard) {
	m.mu.Lock()
	m.card = card
	srv := m.server
	m.mu.Unlock()

	if srv != nil {
		srv.SetCard(card)
	}
}

// SetServerSecret updates the secret used to verify inbound task requests.
// Applies on the next StartServer; a running server keeps its old secret until
// restarted.
func (m *Manager) SetServerSecret(secret string) {
	m.mu.Lock()
	m.serverSecret = secret
	m.mu.Unlock()
}

// GetCard returns the current agent card.
func (m *Manager) GetCard() AgentCard {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.card
}

// ── Remote Agents ──

// LoadRemoteAgents loads persisted remote agent configs from disk.
func (m *Manager) LoadRemoteAgents() {
	agents, err := m.store.Load()
	if err != nil {
		log.Printf("[a2a] load remote agents: %v", err)
		return
	}

	m.mu.Lock()
	for i := range agents {
		a := agents[i]
		a.Status = "disconnected"
		m.agents[a.ID] = &a
	}
	m.mu.Unlock()

	log.Printf("[a2a] loaded %d remote agents", len(agents))
}

// ListRemoteAgents returns all configured remote agents.
func (m *Manager) ListRemoteAgents() []RemoteAgentConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]RemoteAgentConfig, 0, len(m.agents))
	for _, a := range m.agents {
		result = append(result, *a)
	}
	return result
}

// AddRemoteAgent adds a new remote agent config and persists it.
func (m *Manager) AddRemoteAgent(name, url, secret string) (*RemoteAgentConfig, error) {
	m.mu.Lock()
	agent := &RemoteAgentConfig{
		ID:     uuid.New().String(),
		Name:   name,
		URL:    url,
		Secret: secret,
		Status: "disconnected",
	}
	m.agents[agent.ID] = agent
	m.mu.Unlock()

	if err := m.saveAgents(); err != nil {
		return nil, err
	}

	m.emit("agent:changed", map[string]string{"action": "agent_added", "id": agent.ID})
	return agent, nil
}

// RemoveRemoteAgent removes a remote agent and its client connection.
func (m *Manager) RemoveRemoteAgent(id string) error {
	m.mu.Lock()
	if _, ok := m.clients[id]; ok {
		delete(m.clients, id)
	}
	delete(m.agents, id)
	m.mu.Unlock()

	if err := m.saveAgents(); err != nil {
		return err
	}

	m.emit("agent:changed", map[string]string{"action": "agent_removed", "id": id})
	return nil
}

// ConnectRemoteAgent connects to a remote agent and fetches its card.
func (m *Manager) ConnectRemoteAgent(id string) error {
	m.mu.RLock()
	agent, ok := m.agents[id]
	m.mu.RUnlock()

	if !ok {
		return ErrAgentNotFound
	}

	client := NewClient(agent.URL, agent.Secret)
	ctx := context.Background()

	if err := client.Connect(ctx); err != nil {
		m.mu.Lock()
		agent.Status = "error"
		agent.Error = err.Error()
		m.mu.Unlock()
		_ = m.saveAgents()
		m.emit("agent:changed", map[string]string{"action": "agent_error", "id": id})
		return err
	}

	m.mu.Lock()
	agent.Status = "connected"
	agent.Error = ""
	agent.Card = client.GetCard()
	agent.ConnectedAt = nowMillis()
	m.clients[id] = client
	m.mu.Unlock()

	if err := m.saveAgents(); err != nil {
		log.Printf("[a2a] save after connect: %v", err)
	}

	m.emit("agent:changed", map[string]string{"action": "agent_connected", "id": id})
	return nil
}

// DisconnectRemoteAgent disconnects from a remote agent.
func (m *Manager) DisconnectRemoteAgent(id string) error {
	m.mu.Lock()
	delete(m.clients, id)
	if agent, ok := m.agents[id]; ok {
		agent.Status = "disconnected"
		agent.ConnectedAt = 0
	}
	m.mu.Unlock()

	if err := m.saveAgents(); err != nil {
		return err
	}

	m.emit("agent:changed", map[string]string{"action": "agent_disconnected", "id": id})
	return nil
}

// UpdateRemoteAgent updates name/url of an existing remote agent.
func (m *Manager) UpdateRemoteAgent(id, name, url, secret string) error {
	m.mu.Lock()
	agent, ok := m.agents[id]
	if !ok {
		m.mu.Unlock()
		return ErrAgentNotFound
	}
	agent.Name = name
	agent.URL = url
	agent.Secret = secret
	m.mu.Unlock()

	if err := m.saveAgents(); err != nil {
		return err
	}

	m.emit("agent:changed", map[string]string{"action": "agent_updated", "id": id})
	return nil
}

// SendTask sends a task to a connected remote agent.
func (m *Manager) SendTask(agentID string, text string) (*Task, error) {
	m.mu.RLock()
	client, ok := m.clients[agentID]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrAgentNotConnected
	}

	msg := TextMessage("user", text)
	ctx := context.Background()
	task, err := client.SendTask(ctx, msg)
	if err != nil {
		// Mark the agent errored so the UI stops showing a stale "connected".
		m.mu.Lock()
		if agent, ok := m.agents[agentID]; ok {
			agent.Status = "error"
			agent.Error = err.Error()
		}
		delete(m.clients, agentID)
		m.mu.Unlock()
		_ = m.saveAgents()
		m.emit("agent:changed", map[string]string{"action": "agent_error", "id": agentID})
		return nil, err
	}
	return task, nil
}

// GetTask retrieves a task status from a connected remote agent.
func (m *Manager) GetTask(agentID, taskID string) (*Task, error) {
	m.mu.RLock()
	client, ok := m.clients[agentID]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrAgentNotConnected
	}

	ctx := context.Background()
	return client.GetTask(ctx, taskID)
}

// DisconnectAll disconnects all remote agents (called on shutdown).
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	for id := range m.clients {
		log.Printf("[a2a] disconnecting %s", id)
		if agent, ok := m.agents[id]; ok {
			agent.Status = "disconnected"
		}
	}
	m.clients = make(map[string]*Client)
	m.mu.Unlock()

	// Persist outside the write lock — saveAgents takes the read lock itself,
	// and taking it while holding the write lock self-deadlocks (the exact bug
	// that hung shutdown at "[a2a] server stopped").
	_ = m.saveAgents()
}

// Close stops the keep-alive pinger. Called once on app shutdown.
func (m *Manager) Close() {
	select {
	case <-m.done:
	default:
		close(m.done)
	}
}

// pingLoop health-checks connected remote agents every minute so a dead peer is
// reflected as "error" instead of a stale "connected".
func (m *Manager) pingLoop() {
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-m.done:
			return
		case <-t.C:
			m.pingAll()
		}
	}
}

func (m *Manager) pingAll() {
	m.mu.RLock()
	type conn struct {
		id string
		c  *Client
	}
	var conns []conn
	for id, c := range m.clients {
		conns = append(conns, conn{id, c})
	}
	m.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	changed := false
	for _, e := range conns {
		if err := e.c.Ping(ctx); err != nil {
			m.mu.Lock()
			if agent, ok := m.agents[e.id]; ok {
				agent.Status = "error"
				agent.Error = "unreachable: " + err.Error()
			}
			delete(m.clients, e.id)
			m.mu.Unlock()
			changed = true
		}
	}
	if changed {
		_ = m.saveAgents()
		m.emit("agent:changed", map[string]string{"action": "agents_health"})
	}
}

// ── internal ──

func (m *Manager) saveAgents() error {
	m.mu.RLock()
	agents := make([]RemoteAgentConfig, 0, len(m.agents))
	for _, a := range m.agents {
		agents = append(agents, *a)
	}
	m.mu.RUnlock()

	return m.store.Save(agents)
}

func (m *Manager) emit(event string, data interface{}) {
	if m.onEvent != nil {
		m.onEvent(event, data)
	}
}

// ── errors ──

var (
	ErrAgentNotFound     = &AgentError{msg: "agent not found"}
	ErrAgentNotConnected = &AgentError{msg: "agent not connected"}
)

// AgentError is a typed error for A2A operations.
type AgentError struct {
	msg string
}

func (e *AgentError) Error() string { return e.msg }
