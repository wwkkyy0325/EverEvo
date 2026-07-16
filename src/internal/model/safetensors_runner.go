package model

import (
	"context"
	"fmt"
	"time"

	"everevo/internal/backends/safetensors"
)

// SafeTensorsModel 解析 .safetensors 文件头，读取张量元数据。
// 真实推理需要下游引擎（ONNX/PyTorch），此处仅提供元数据读取。
type SafeTensorsModel struct {
	id       string
	name     string
	modelPath string
	reader   *safetensors.Reader
	info     ModelInfo
}

// NewSafeTensorsRunner 创建 SafeTensors 运行器。
// 真实推理需要下游引擎（ONNX/PyTorch），此处仅提供张量元数据读取。
func NewSafeTensorsRunner(id, name, modelPath string) *SafeTensorsModel {
	return &SafeTensorsModel{
		id:        id,
		name:      name,
		modelPath: modelPath,
		info: ModelInfo{
			ID:           id,
			Name:         name,
			Type:         ModelTypeSafeTensors,
			State:        ModelStateIdle,
			Engine:       "safetensors-metadata",
			EngineStatus: "metadata-only",
		},
	}
}

func (m *SafeTensorsModel) ID() string      { return m.id }
func (m *SafeTensorsModel) Info() ModelInfo { return m.info }

func (m *SafeTensorsModel) Load() error {
	m.info.State = ModelStateLoading
	r, err := safetensors.Open(m.modelPath)
	if err != nil {
		m.info.State = ModelStateError
		return fmt.Errorf("解析 safetensors 文件失败: %w", err)
	}
	m.reader = r
	m.info.State = ModelStateReady
	m.info.LoadedAt = time.Now()
	m.info.Size = int64(len(r.TensorNames()))
	return nil
}

func (m *SafeTensorsModel) Unload() error {
	m.info.State = ModelStateIdle
	if m.reader != nil {
		m.reader.Close()
		m.reader = nil
	}
	return nil
}

func (m *SafeTensorsModel) Run(ctx context.Context, input []byte) ([]byte, error) {
	if m.info.State != ModelStateReady {
		return nil, fmt.Errorf("SafeTensors 模型 %s 未就绪", m.id)
	}
	m.info.State = ModelStateRunning
	defer func() { m.info.State = ModelStateReady }()

	names := m.reader.TensorNames()
	header := m.reader.Header()
	summary := fmt.Sprintf("[SafeTensors 元数据] 文件: %s\n%d 个张量 (仅元数据读取，真实推理需下游引擎):", m.modelPath, len(names))
	for _, n := range names {
		t := header[n]
		summary += fmt.Sprintf("\n  %s  dtype=%s  shape=%v  bytes=[%d..%d]", n, t.Dtype, t.Shape, t.DataOffsets[0], t.DataOffsets[1])
	}
	_ = time.Now()
	return []byte(summary), nil
}
