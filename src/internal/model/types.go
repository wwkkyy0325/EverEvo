package model

import "time"

// ModelType 标识模型种类。
type ModelType string

const (
	ModelTypePlaceholder   ModelType = "placeholder"
	ModelTypeONNX          ModelType = "onnx"
	ModelTypeGGUF          ModelType = "gguf"
	ModelTypeSafeTensors   ModelType = "safetensors"
	ModelTypePyTorch       ModelType = "pytorch"
)

// ModelState 表示已加载模型的生命周期状态。
type ModelState string

const (
	ModelStateIdle    ModelState = "idle"    // 空闲
	ModelStateLoading ModelState = "loading" // 加载中
	ModelStateReady   ModelState = "ready"   // 就绪
	ModelStateError   ModelState = "error"   // 异常
	ModelStateRunning ModelState = "running" // 运行中
)

// ModelInfo 是已加载模型的元数据，可安全传给前端。
type ModelInfo struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Type         ModelType  `json:"type"`
	State        ModelState `json:"state"`
	Size         int64      `json:"size"`
	LoadedAt     time.Time  `json:"loadedAt"`
	Engine       string     `json:"engine"`       // 后端引擎标识，如 "onnx", "llama", "safetensors-metadata", "placeholder"
	EngineStatus string     `json:"engineStatus"` // "live"(真实推理), "metadata-only"(仅元数据), "unavailable"(DLL缺失), "unsupported"(需转换)
}
