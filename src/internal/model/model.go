package model

import (
	"context"
	"fmt"
	"time"
)

// ModelRunner 是所有模型后端必须实现的接口。
type ModelRunner interface {
	ID() string
	Info() ModelInfo
	Load() error
	Unload() error
	Run(ctx context.Context, input []byte) ([]byte, error)
}

// PlaceholderModel 是验证架构链路的占位实现，将输入通过 Rust 引擎回显。
type PlaceholderModel struct {
	id   string
	name string
	info ModelInfo
}

// NewPlaceholder 创建通用占位模型（用于测试链路，无文件关联）。
func NewPlaceholder(id, name string) *PlaceholderModel {
	return newPlaceholderWithType(id, name, ModelTypePlaceholder, "placeholder", "unavailable")
}

// NewPyTorchPlaceholder 创建 PyTorch 格式占位模型（提示需转换为 ONNX/GGUF）。
func NewPyTorchPlaceholder(id, name string) *PlaceholderModel {
	return newPlaceholderWithType(id, name, ModelTypePyTorch, "none", "unsupported")
}

func newPlaceholderWithType(id, name string, mtype ModelType, engine, engineStatus string) *PlaceholderModel {
	return &PlaceholderModel{
		id:   id,
		name: name,
		info: ModelInfo{
			ID:           id,
			Name:         name,
			Type:         mtype,
			State:        ModelStateIdle,
			Size:         0,
			Engine:       engine,
			EngineStatus: engineStatus,
		},
	}
}

func (m *PlaceholderModel) ID() string     { return m.id }
func (m *PlaceholderModel) Info() ModelInfo { return m.info }

func (m *PlaceholderModel) Load() error {
	m.info.State = ModelStateLoading
	// 模拟加载 —— 后续改为调用 bridge.LoadModel()
	time.Sleep(10 * time.Millisecond)
	m.info.State = ModelStateReady
	m.info.LoadedAt = time.Now()
	return nil
}

func (m *PlaceholderModel) Unload() error {
	m.info.State = ModelStateIdle
	return nil
}

func (m *PlaceholderModel) Run(ctx context.Context, input []byte) ([]byte, error) {
	if m.info.State != ModelStateReady {
		return nil, fmt.Errorf("模型 %s 未就绪（当前状态: %s）", m.id, m.info.State)
	}
	m.info.State = ModelStateRunning
	defer func() { m.info.State = ModelStateReady }()

	switch m.info.Type {
	case ModelTypePyTorch:
		output := fmt.Sprintf("[PyTorch 占位] 文件需转换为 ONNX(.onnx) 或 GGUF(.gguf) 后使用。\n已接收 %d 字节: %s", len(input), string(input))
		return []byte(output), nil
	default:
		output := fmt.Sprintf("[占位] 已接收 %d 字节: %s", len(input), string(input))
		return []byte(output), nil
	}
}
