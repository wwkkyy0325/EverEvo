package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
)

// Status is the public status of the MCP server returned to the frontend.
type Status struct {
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	URL     string `json:"url"`
}

// Server is an MCP HTTP server that exposes EverEvo capabilities.
type Server struct {
	mu       sync.Mutex
	listener net.Listener
	httpSrv  *http.Server
	port     int
	running  bool
	app      App
	version  string
}

// App is the interface the MCP server needs from the application.
type App interface {
	ToolCaller
	ResourceProvider
}

// NewServer creates a new MCP server.
func NewServer(app App, version string) *Server {
	return &Server{app: app, version: version}
}

// findPort tries to listen on startPort, then startPort+1 ... up to +20.
// Returns the listener and actual port, or error if none available.
func findPort(startPort int) (net.Listener, int, error) {
	for offset := 0; offset < 20; offset++ {
		port := startPort + offset
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			if strings.Contains(err.Error(), "address already in use") ||
				strings.Contains(err.Error(), "Only one usage") {
				log.Printf("[mcp] port %d in use, trying %d", port, port+1)
				continue
			}
			return nil, 0, err
		}
		return l, port, nil
	}
	return nil, 0, fmt.Errorf("no available port in range %d-%d", startPort, startPort+19)
}

// Start starts the MCP HTTP server on the given port (only on 127.0.0.1).
// If the requested port is occupied, it automatically finds the next available.
func (s *Server) Start(port int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		log.Printf("[mcp] already running on port %d, ignoring Start()", s.port)
		return nil // idempotent: already running is not an error
	}

	if port <= 0 {
		port = 19800
	}

	l, actualPort, err := findPort(port)
	if err != nil {
		return fmt.Errorf("MCP server listen failed: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", s.handleMCP)
	mux.HandleFunc("/health", s.handleHealth)

	s.listener = l
	s.port = actualPort
	s.httpSrv = &http.Server{Handler: corsMiddleware(mux)}
	s.running = true

	go func() {
		addr := fmt.Sprintf("http://127.0.0.1:%d/mcp", actualPort)
		log.Printf("[mcp] server started on %s", addr)
		if err := s.httpSrv.Serve(l); err != nil && err != http.ErrServerClosed {
			log.Printf("[mcp] server error: %v", err)
		}
	}()

	return nil
}

// Stop shuts down the MCP server gracefully.
func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false
	if s.httpSrv != nil {
		s.httpSrv.Close()
	}
	if s.listener != nil {
		s.listener.Close()
	}
	log.Println("[mcp] server stopped")
	return nil
}

// Status returns the server's public status.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	url := ""
	if s.port > 0 {
		url = fmt.Sprintf("http://127.0.0.1:%d/mcp", s.port)
	}
	st := Status{
		Running: s.running,
		Port:    s.port,
		URL:     url,
	}
	log.Printf("[mcp] Status() → running=%v port=%d", st.Running, st.Port)
	return st
}

// IsRunning returns whether the server is currently running.
func (s *Server) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Port returns the port the server is running on.
func (s *Server) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}

// handleHealth returns a simple health check.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"running": s.IsRunning(),
		"port":    s.Port(),
	})
}

// handleMCP is the main MCP endpoint (POST for requests).
func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "SSE not supported; use POST for MCP requests"})
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]string{"error": "use POST for MCP requests"})
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			JSONRPC: "2.0", ID: nil,
			Error: RPCErr{Code: -32700, Message: "Parse error"},
		})
		return
	}

	var req Request
	if err := json.Unmarshal(body, &req); err != nil || req.Method == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			JSONRPC: "2.0", ID: nil,
			Error: RPCErr{Code: -32700, Message: "Parse error"},
		})
		return
	}

	if req.ID == nil {
		s.handleNotification(req)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	result, rpcErr := s.dispatch(req)
	w.Header().Set("Content-Type", "application/json")

	if rpcErr != nil {
		writeJSON(w, http.StatusOK, ErrorResponse{
			JSONRPC: "2.0", ID: req.ID, Error: *rpcErr,
		})
		return
	}

	writeJSON(w, http.StatusOK, SuccessResponse{
		JSONRPC: "2.0", ID: req.ID, Result: result,
	})
}

// dispatch routes an MCP request to the appropriate handler.
func (s *Server) dispatch(req Request) (any, *RPCErr) {
	switch req.Method {
	case "initialize":
		return HandleInitialize(s.version), nil
	case "notifications/initialized":
		return map[string]string{}, nil

	case "tools/list":
		result, err := HandleToolsList()
		if err != nil {
			return nil, &RPCErr{Code: -32603, Message: err.Error()}
		}
		return result, nil
	case "tools/call":
		var p CallToolParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, &RPCErr{Code: -32602, Message: "Invalid params: " + err.Error()}
		}
		result, err := HandleToolsCall(s.app, p.Name, p.Arguments)
		if err != nil {
			return nil, &RPCErr{Code: -32603, Message: err.Error()}
		}
		return result, nil

	case "resources/list":
		result, err := HandleResourcesList()
		if err != nil {
			return nil, &RPCErr{Code: -32603, Message: err.Error()}
		}
		return result, nil
	case "resources/read":
		var p ReadResourceParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, &RPCErr{Code: -32602, Message: "Invalid params: " + err.Error()}
		}
		result, err := HandleResourcesRead(s.app, p.URI)
		if err != nil {
			return nil, &RPCErr{Code: -32603, Message: err.Error()}
		}
		return result, nil

	case "prompts/list":
		result, err := HandlePromptsList()
		if err != nil {
			return nil, &RPCErr{Code: -32603, Message: err.Error()}
		}
		return result, nil
	case "prompts/get":
		var p GetPromptParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, &RPCErr{Code: -32602, Message: "Invalid params: " + err.Error()}
		}
		result, err := HandlePromptsGet(p.Name, p.Arguments)
		if err != nil {
			return nil, &RPCErr{Code: -32603, Message: err.Error()}
		}
		if result == nil {
			return nil, &RPCErr{Code: -32602, Message: "Unknown prompt: " + p.Name}
		}
		return result, nil

	case "ping":
		return map[string]string{}, nil

	default:
		return nil, &RPCErr{Code: -32601, Message: "Method not found: " + req.Method}
	}
}

func (s *Server) handleNotification(req Request) {
	switch req.Method {
	case "notifications/initialized":
		log.Println("[mcp] client initialized")
	case "notifications/cancelled":
	default:
		log.Printf("[mcp] unhandled notification: %s", req.Method)
	}
}

func decodeParams(params any, target any) error {
	data, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Mcp-Session-Id, MCP-Protocol-Version")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
