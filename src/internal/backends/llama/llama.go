//go:build windows

// Package llama wraps llama.cpp's llama-server.exe as a subprocess,
// communicating via its OpenAI-compatible HTTP API.
// No CGo required — pure Go + pre-built binary.
package llama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"everevo/internal/backends"
	"everevo/internal/httpclient"
)

var (
	mu          sync.Mutex
	initialized bool
	binPath     string
)

// Init locates llama-server.exe. Call once at startup.
func Init(bin string) error {
	mu.Lock()
	defer mu.Unlock()

	if bin != "" {
		if _, err := os.Stat(bin); err == nil {
			binPath = bin
			initialized = true
			return nil
		}
	}
	// Search for llama-server.exe via backends detection
	for _, b := range backends.Detect() {
		if b.OK && b.Key == "llama" && b.DLLPath != "" {
			binPath = b.DLLPath
			initialized = true
			return nil
		}
	}
	return fmt.Errorf("llama-server.exe not found; download from https://github.com/ggml-org/llama.cpp/releases")
}

// Initialized returns whether Init succeeded.
func Initialized() bool {
	mu.Lock()
	defer mu.Unlock()
	return initialized
}

// Binary returns the path to llama-server.exe.
func Binary() string {
	mu.Lock()
	defer mu.Unlock()
	return binPath
}

// Close is a no-op (individual servers are stopped per-model).
func Close() {}

// ─── Server ─────────────────────────────────────────────────────

// ServerConfig holds optional tuning parameters for StartServer.
// Zero values mean "use defaults".
type ServerConfig struct {
	CtxSize   int // context size in tokens (0 → 4096)
	BatchSize int // batch processing size (0 → 512)
	GPULayers int // layers to offload to GPU (0 → 9999 = all)
}

// Server manages a running llama-server subprocess for one model.
type Server struct {
	cmd       *exec.Cmd
	port      int
	modelPath string
	baseURL   string
	client    *http.Client
	mu        sync.Mutex
}

// findFreePort returns an available TCP port on localhost.
func findFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// StartServer launches llama-server for the given model with optional config.
func StartServer(modelPath string, cfg ServerConfig) (*Server, error) {
	mu.Lock()
	bin := binPath
	mu.Unlock()

	if bin == "" {
		return nil, fmt.Errorf("llama-server.exe not available; download from https://github.com/ggml-org/llama.cpp/releases")
	}
	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("model file not found: %s", modelPath)
	}

	// Apply defaults for zero-valued config fields
	if cfg.CtxSize <= 0 {
		cfg.CtxSize = 4096
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 512
	}
	if cfg.GPULayers <= 0 {
		cfg.GPULayers = 9999 // offload all layers to GPU (ignored if CPU-only build)
	}

	port, err := findFreePort()
	if err != nil {
		return nil, fmt.Errorf("find port: %w", err)
	}

	args := []string{
		"-m", modelPath,
		"--port", fmt.Sprintf("%d", port),
		"--host", "127.0.0.1",
		"-ngl", fmt.Sprintf("%d", cfg.GPULayers),
		"--ctx-size", fmt.Sprintf("%d", cfg.CtxSize),
		"--batch-size", fmt.Sprintf("%d", cfg.BatchSize),
		"--no-webui",
	}

	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start llama-server: %w", err)
	}

	srv := &Server{
		cmd:       cmd,
		port:      port,
		modelPath: modelPath,
		baseURL:   fmt.Sprintf("http://127.0.0.1:%d", port),
		client:    httpclient.New(120 * time.Second),
	}

	// Wait for server to become healthy (max 30s)
	if err := srv.waitReady(30 * time.Second); err != nil {
		srv.Stop()
		return nil, fmt.Errorf("llama-server not ready: %w", err)
	}

	log.Printf("[llama] server ready on port %d ctx=%d for %s", port, cfg.CtxSize, filepath.Base(modelPath))
	return srv, nil
}

// waitReady polls /health until the server responds.
func (s *Server) waitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := s.baseURL + "/health"
	for time.Now().Before(deadline) {
		resp, err := s.client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", url)
}

// Stop terminates the llama-server subprocess.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd == nil || s.cmd.Process == nil {
		return
	}

	// Graceful shutdown via /health with shutdown action (if supported)
	// Fallback: kill process
	done := make(chan error, 1)
	go func() {
		done <- s.cmd.Wait()
	}()

	// Try to terminate gracefully
	s.cmd.Process.Signal(os.Interrupt)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		s.cmd.Process.Kill()
		<-done
	}

	log.Printf("[llama] server stopped (port %d)", s.port)
}

// Port returns the server's port.
func (s *Server) Port() int { return s.port }

// Health checks if the server is responding.
func (s *Server) Health() bool {
	resp, err := s.client.Get(s.baseURL + "/health")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

// ─── Chat API ───────────────────────────────────────────────────

// ChatMessage is a message in the chat completion request.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest mirrors the OpenAI /v1/chat/completions format.
type ChatRequest struct {
	Messages   []ChatMessage `json:"messages"`
	MaxTokens  int           `json:"max_tokens,omitempty"`
	Stream     bool          `json:"stream"`
}

// ChatChoice is one completion choice.
type ChatChoice struct {
	Message ChatMessage `json:"message"`
}

// ChatUsage tracks token usage.
type ChatUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResponse is the response from /v1/chat/completions.
type ChatResponse struct {
	Choices []ChatChoice `json:"choices"`
	Usage   ChatUsage    `json:"usage"`
}

// Chat sends a single-turn chat completion request.
func (s *Server) Chat(messages []ChatMessage, maxTokens int) (*ChatResponse, error) {
	reqBody := ChatRequest{
		Messages:  messages,
		MaxTokens: maxTokens,
		Stream:    false,
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	resp, err := s.client.Post(
		s.baseURL+"/v1/chat/completions",
		"application/json",
		bytes.NewReader(data),
	)
	if err != nil {
		return nil, fmt.Errorf("llama-server request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("llama-server error %d: %s", resp.StatusCode, strings.TrimSpace(string(body[:min(len(body), 300)])))
	}

	var cr ChatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return &cr, nil
}

// Generate sends a simple prompt and returns the response text.
func (s *Server) Generate(prompt string, maxTokens int) (string, error) {
	if maxTokens <= 0 {
		maxTokens = 512
	}
	msgs := []ChatMessage{
		{Role: "user", Content: prompt},
	}
	cr, err := s.Chat(msgs, maxTokens)
	if err != nil {
		return "", err
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("no response from model")
	}
	return cr.Choices[0].Message.Content, nil
}
