package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Manager 管理所有已加载模型的生命周期。线程安全。
type Manager struct {
	mu     sync.RWMutex
	models map[string]ModelRunner
}

// NewManager 创建一个空的模型管理器。
func NewManager() *Manager {
	return &Manager{
		models: make(map[string]ModelRunner),
	}
}

// LoadModel 加载占位模型（无真实推理，用于测试链路）。
func (mgr *Manager) LoadModel(id, name string) (ModelInfo, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if _, exists := mgr.models[id]; exists {
		return ModelInfo{}, fmt.Errorf("模型 %q 已加载", id)
	}

	m := NewPlaceholder(id, name)
	if err := m.Load(); err != nil {
		return ModelInfo{}, fmt.Errorf("加载模型 %q 失败: %w", id, err)
	}
	mgr.models[id] = m
	return m.Info(), nil
}

// LoadModelFile 加载模型文件（.onnx 路由到 ONNX Runtime，否则走占位）。
func (mgr *Manager) LoadModelFile(id, name, modelPath string) (ModelInfo, error) {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if _, exists := mgr.models[id]; exists {
		return ModelInfo{}, fmt.Errorf("模型 %q 已加载", id)
	}

	if _, err := os.Stat(modelPath); os.IsNotExist(err) {
		return ModelInfo{}, fmt.Errorf("模型文件不存在: %s", modelPath)
	}

	ext := strings.ToLower(filepath.Ext(modelPath))
	var m ModelRunner
	switch ext {
	case ".onnx":
		onnxM, err := NewONNXRunner(id, name, modelPath)
		if err != nil {
			return ModelInfo{}, fmt.Errorf("创建 ONNX 运行器失败: %w", err)
		}
		m = onnxM
	case ".gguf":
		llamaM, err := NewLlamaRunner(id, name, modelPath, 0)
		if err != nil {
			return ModelInfo{}, fmt.Errorf("创建 llama.cpp 运行器失败: %w", err)
		}
		m = llamaM
	case ".safetensors", ".bin":
		m = NewSafeTensorsRunner(id, name, modelPath)
	case ".pt", ".pth":
		m = NewPyTorchPlaceholder(id, name)
	default:
		return ModelInfo{}, fmt.Errorf("不支持的模型格式: %s", ext)
	}

	if err := m.Load(); err != nil {
		return ModelInfo{}, fmt.Errorf("加载模型 %q 失败: %w", id, err)
	}
	mgr.models[id] = m
	return m.Info(), nil
}

// UnloadModel 卸载并移除指定 ID 的模型。
func (mgr *Manager) UnloadModel(id string) error {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	m, exists := mgr.models[id]
	if !exists {
		return fmt.Errorf("模型 %q 未找到", id)
	}
	if err := m.Unload(); err != nil {
		return err
	}
	delete(mgr.models, id)
	return nil
}

// RunModel 对已加载的模型执行推理。
func (mgr *Manager) RunModel(ctx context.Context, id string, input []byte) ([]byte, error) {
	mgr.mu.RLock()
	m, exists := mgr.models[id]
	mgr.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("模型 %q 未找到", id)
	}
	return m.Run(ctx, input)
}

// List 返回所有已加载模型的信息。
func (mgr *Manager) List() []ModelInfo {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()

	list := make([]ModelInfo, 0, len(mgr.models))
	for _, m := range mgr.models {
		list = append(list, m.Info())
	}
	return list
}

// Count 返回已加载模型的数量。
func (mgr *Manager) Count() int {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return len(mgr.models)
}

// Shutdown 卸载所有模型。
func (mgr *Manager) Shutdown() {
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	for id, m := range mgr.models {
		_ = m.Unload()
		delete(mgr.models, id)
	}
}
