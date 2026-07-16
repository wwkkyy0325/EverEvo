// Package a2a implements a minimal Google Agent-to-Agent (A2A) protocol layer:
// agent-card discovery, JSON-RPC 2.0 task messaging, and HTTP server + client.
// Phase 1 covers synchronous tasks/send (no SSE/push yet).
package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TaskExecutor is the callback that executes a task on the LLM.
// The server is transport-only; actual LLM call is delegated.
type TaskExecutor func(ctx context.Context, messages []Message) (string, error)

// Server is an HTTP JSON-RPC 2.0 A2A server.
type Server struct {
	mu        sync.RWMutex
	card      AgentCard
	executor  TaskExecutor
	secret    string
	httpSrv   *http.Server
	port      int
	running   bool
	tasks     map[string]*Task
	done      chan struct{} // closed in Stop to end the task-cleanup goroutine
}

// ServerStatus is returned to the frontend.
type ServerStatus struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
}

// NewServer creates an A2A server with the given agent card, task executor and
// optional Feishu-style signing secret.
func NewServer(card AgentCard, executor TaskExecutor, secret string) *Server {
	return &Server{
		card:     card,
		executor: executor,
		secret:   secret,
		tasks:    make(map[string]*Task),
	}
}

// Start begins listening on the given port.
func (s *Server) Start(port int) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("A2A server already running")
	}
	s.port = port
	s.httpSrv = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: s,
	}
	s.mu.Unlock()

	ln, err := net.Listen("tcp", s.httpSrv.Addr)
	if err != nil {
		return fmt.Errorf("A2A listen %s: %w", s.httpSrv.Addr, err)
	}

	s.mu.Lock()
	s.running = true
	s.done = make(chan struct{})
	s.mu.Unlock()

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[a2a] server error: %v", err)
		}
	}()
	go s.cleanupLoop()

	log.Printf("[a2a] server started on %s", s.httpSrv.Addr)
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.running || s.httpSrv == nil {
		s.mu.Unlock()
		return nil
	}
	srv := s.httpSrv
	s.running = false
	done := s.done
	s.done = nil
	s.mu.Unlock()

	// Shutdown outside the lock: HTTP handlers acquire s.mu (RLock/Lock), so
	// calling Shutdown while holding the write lock deadlocks any in-flight
	// request until the 5s timeout — the bug that previously stalled restarts.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("A2A shutdown: %w", err)
	}
	if done != nil {
		close(done)
	}
	log.Println("[a2a] server stopped")
	return nil
}

// cleanupLoop periodically evicts finished tasks so s.tasks cannot grow without
// bound — every tasks/send stored a Task and nothing ever removed it.
func (s *Server) cleanupLoop() {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.evictOldTasks(time.Hour)
		}
	}
}

func (s *Server) evictOldTasks(maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge).UTC().Format(time.RFC3339)
	s.mu.Lock()
	for id, t := range s.tasks {
		// Never evict in-flight tasks; only finished ones older than maxAge.
		if t.Status.State == StateWorking || t.Status.State == StateSubmitted {
			continue
		}
		if t.Status.Timestamp != "" && t.Status.Timestamp < cutoff {
			delete(s.tasks, id)
		}
	}
	s.mu.Unlock()
}

// Status returns the current server status.
func (s *Server) Status() ServerStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	status := ServerStatus{Running: s.running, Port: s.port}
	if s.running {
		status.URL = fmt.Sprintf("http://127.0.0.1:%d", s.port)
	}
	return status
}

// IsRunning returns whether the server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// ServeHTTP implements http.Handler — routes requests.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-A2A-Timestamp, X-A2A-Signature")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// agent-card discovery stays public (otherwise peers could not connect);
	// task endpoints require a valid Feishu-style signature when a secret is set.
	if r.Method == http.MethodGet && r.URL.Path == "/.well-known/agent-card.json" {
		s.handleAgentCard(w, r)
		return
	}

	if r.Method == http.MethodPost && (r.URL.Path == "/" || r.URL.Path == "") {
		if err := VerifySignature(s.secret, r.Header.Get(HeaderTimestamp), r.Header.Get(HeaderSignature), time.Now()); err != nil {
			writeRPCError(w, nil, -32001, "Unauthorized: "+err.Error())
			return
		}
		s.handleJSONRPC(w, r)
		return
	}

	http.NotFound(w, r)
}

// handleAgentCard returns the AgentCard JSON.
func (s *Server) handleAgentCard(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	card := s.card
	s.mu.RUnlock()

	// Advertise the host the client actually reached us on (correct behind proxies
	// and in tests); fall back to the configured port if no Host header is present.
	host := r.Host
	if host == "" {
		host = fmt.Sprintf("127.0.0.1:%d", s.port)
	}
	card.URL = "http://" + host

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(card)
}

// handleJSONRPC processes a JSON-RPC 2.0 request.
func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeRPCError(w, nil, -32700, "Parse error")
		return
	}

	if req.JSONRPC != "2.0" {
		writeRPCError(w, req.ID, -32600, "Invalid Request: jsonrpc must be 2.0")
		return
	}

	switch req.Method {
	case MethodSend:
		s.handleSend(w, req)
	case MethodGet:
		s.handleGet(w, req)
	case MethodCancel:
		s.handleCancel(w, req)
	default:
		writeRPCError(w, req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

// handleSend processes tasks/send.
func (s *Server) handleSend(w http.ResponseWriter, req JSONRPCRequest) {
	var params SendParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, fmt.Sprintf("Invalid params: %v", err))
		return
	}

	taskID := params.ID
	if taskID == "" {
		taskID = uuid.New().String()
	}

	task := &Task{
		ID:        taskID,
		SessionID: params.SessionID,
		Status: TaskStatus{
			State:     StateWorking,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		History: []Message{params.Message},
	}

	// Store task
	s.mu.Lock()
	s.tasks[taskID] = task
	s.mu.Unlock()

	// Execute via the registered executor
	if s.executor != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		text, execErr := s.executor(ctx, task.History)
		now := time.Now().UTC().Format(time.RFC3339)

		s.mu.Lock()
		if execErr != nil {
			task.Status.State = StateFailed
			task.Status.Message = &Message{Role: "agent", Parts: []Part{{Kind: "text", Text: execErr.Error()}}}
		} else {
			task.Status.State = StateCompleted
			task.Artifacts = []Artifact{{Name: "output", Parts: []Part{{Kind: "text", Text: text}}}}
		}
		task.Status.Timestamp = now
		s.mu.Unlock()
	} else {
		s.mu.Lock()
		task.Status.State = StateFailed
		task.Status.Message = &Message{Role: "agent", Parts: []Part{{Kind: "text", Text: "No task executor configured"}}}
		task.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)
		s.mu.Unlock()
	}

	s.mu.RLock()
	result := *task
	s.mu.RUnlock()

	writeRPCResult(w, req.ID, result)
}

// handleGet processes tasks/get.
func (s *Server) handleGet(w http.ResponseWriter, req JSONRPCRequest) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, fmt.Sprintf("Invalid params: %v", err))
		return
	}

	s.mu.RLock()
	task, ok := s.tasks[params.ID]
	s.mu.RUnlock()

	if !ok {
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("Task not found: %s", params.ID))
		return
	}

	writeRPCResult(w, req.ID, task)
}

// handleCancel processes tasks/cancel.
func (s *Server) handleCancel(w http.ResponseWriter, req JSONRPCRequest) {
	var params struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		writeRPCError(w, req.ID, -32602, fmt.Sprintf("Invalid params: %v", err))
		return
	}

	s.mu.Lock()
	task, ok := s.tasks[params.ID]
	if !ok {
		s.mu.Unlock()
		writeRPCError(w, req.ID, -32000, fmt.Sprintf("Task not found: %s", params.ID))
		return
	}
	task.Status.State = StateCanceled
	task.Status.Timestamp = time.Now().UTC().Format(time.RFC3339)
	result := *task
	s.mu.Unlock()

	writeRPCResult(w, req.ID, result)
}

// SetCard updates the agent card (for runtime changes to name/description).
func (s *Server) SetCard(card AgentCard) {
	s.mu.Lock()
	s.card = card
	s.mu.Unlock()
}

// ── helpers ──

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, message string) {
	// Per JSON-RPC 2.0 spec every error is answered. A request whose id could
	// not be parsed is answered with id=null.
	if id == nil && code == -32700 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &JSONRPCError{Code: code, Message: message},
		})
		return
	}
	respID := id
	if respID == nil {
		respID = json.RawMessage("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      respID,
		Error:   &JSONRPCError{Code: code, Message: message},
	})
}
