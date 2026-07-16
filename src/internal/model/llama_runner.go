package model

import (
	"context"
	"fmt"
	"log"
	"time"

	"everevo/internal/backends/llama"
)

// LlamaModel runs a GGUF model via llama.cpp subprocess server.
type LlamaModel struct {
	id      string
	name    string
	path    string
	ctxSize int
	srv     *llama.Server
	info    ModelInfo
}

// NewLlamaRunner creates a LlamaModel. Load() starts the server.
// ctxSize specifies the context window in tokens (0 → server default 4096).
func NewLlamaRunner(id, name, modelPath string, ctxSize int) (*LlamaModel, error) {
	status := "unavailable"
	if llama.Initialized() {
		status = "available"
	}
	return &LlamaModel{
		id:      id,
		name:    name,
		path:    modelPath,
		ctxSize: ctxSize,
		info: ModelInfo{
			ID:           id,
			Name:         name,
			Type:         ModelTypeGGUF,
			State:        ModelStateIdle,
			Engine:       "llama",
			EngineStatus: status,
		},
	}, nil
}

func (m *LlamaModel) ID() string      { return m.id }
func (m *LlamaModel) Info() ModelInfo { return m.info }

func (m *LlamaModel) Load() error {
	if !llama.Initialized() {
		return fmt.Errorf("llama.cpp not available; download llama-server.exe from https://github.com/ggml-org/llama.cpp/releases and place it next to EverEvo.exe")
	}

	m.info.State = ModelStateLoading
	cfg := llama.ServerConfig{CtxSize: m.ctxSize}
	srv, err := llama.StartServer(m.path, cfg)
	if err != nil {
		m.info.State = ModelStateError
		return fmt.Errorf("start llama server: %w", err)
	}
	m.srv = srv
	m.info.State = ModelStateReady
	m.info.EngineStatus = "live"
	m.info.LoadedAt = time.Now()
	log.Printf("[llama] model loaded: %s on port %d", m.name, srv.Port())
	return nil
}

func (m *LlamaModel) Unload() error {
	if m.srv != nil {
		m.srv.Stop()
		m.srv = nil
	}
	m.info.State = ModelStateIdle
	m.info.EngineStatus = "available"
	return nil
}

func (m *LlamaModel) Run(ctx context.Context, input []byte) ([]byte, error) {
	if m.info.State != ModelStateReady {
		return nil, fmt.Errorf("GGUF model %s not ready", m.id)
	}
	if m.srv == nil || !m.srv.Health() {
		return nil, fmt.Errorf("llama server not responsive")
	}

	m.info.State = ModelStateRunning
	defer func() { m.info.State = ModelStateReady }()

	text, err := m.srv.Generate(string(input), 512)
	if err != nil {
		return nil, fmt.Errorf("llama generate: %w", err)
	}
	return []byte(text), nil
}
