package model

import (
	"context"
	"fmt"
	"strings"
	"time"

	"everevo/internal/backends/onnx"
	"everevo/internal/tokenizer"
)

// minimHidden 是 MiniLM-L6-v2 的隐藏维度（last_hidden_state 最后一维）。
// 用于对 [1, seq, hidden] 做 mean-pool 得到句嵌入。
const minimHidden = 384

// ONNXModel 将 ONNX Session 包装为 ModelRunner。
type ONNXModel struct {
	id      string
	name    string
	session *onnx.Session
	tok     *tokenizer.Tokenizer // 可选：NLP 模型的分词器；无则无法把文本转为输入
	info    ModelInfo
}

// NewONNXRunner 加载 ONNX 模型并创建运行器。同时尝试加载模型附带的 tokenizer。
func NewONNXRunner(id, name, modelPath string) (*ONNXModel, error) {
	sess, err := onnx.LoadModel(modelPath)
	if err != nil {
		return nil, err
	}
	// tokenizer 可选：加载失败不致命（只是无法处理文本输入）
	tok, _ := tokenizer.New(modelPath)
	return &ONNXModel{
		id:      id,
		name:    name,
		session: sess,
		tok:     tok,
		info: ModelInfo{
			ID:           id,
			Name:         name,
			Type:         ModelTypeONNX,
			State:        ModelStateIdle,
			Engine:       "onnx",
			EngineStatus: "live",
		},
	}, nil
}

func (m *ONNXModel) ID() string      { return m.id }
func (m *ONNXModel) Info() ModelInfo { return m.info }

func (m *ONNXModel) Load() error {
	m.info.State = ModelStateLoading
	time.Sleep(10 * time.Millisecond)
	m.info.State = ModelStateReady
	m.info.LoadedAt = time.Now()
	return nil
}

func (m *ONNXModel) Unload() error {
	m.info.State = ModelStateIdle
	if m.session != nil {
		m.session.Close()
		m.session = nil
	}
	return nil
}

func (m *ONNXModel) Run(ctx context.Context, input []byte) ([]byte, error) {
	if m.info.State != ModelStateReady {
		return nil, fmt.Errorf("ONNX 模型 %s 未就绪", m.id)
	}
	if m.session == nil {
		return nil, fmt.Errorf("ONNX 会话为空")
	}
	m.info.State = ModelStateRunning
	defer func() { m.info.State = ModelStateReady }()

	if m.tok == nil {
		return nil, fmt.Errorf("该模型未附带 tokenizer，无法将文本转为模型输入")
	}
	inputIds, attn, typeIds, err := m.tok.Encode(string(input))
	if err != nil {
		return nil, err
	}
	out, err := m.session.RunInt64(map[string][]int64{
		"input_ids":      inputIds,
		"attention_mask": attn,
		"token_type_ids": typeIds,
	})
	if err != nil {
		return nil, err
	}
	// 后处理：MiniLM 输出 last_hidden_state [1, seq, 384] float32，
	// 按 attention_mask 加权 mean-pool（排除 pad token）得到 [384] 句嵌入。
	return poolAndRender(out, attn, minimHidden), nil
}

// poolAndRender 把输出按 attention_mask 加权 mean-pool，渲染 "embedding[N]: [v1, …]" 摘要。
// mean-pool 逻辑由 onnx.MeanPool 提供（与句向量工具共用同一份实现）。
func poolAndRender(out []byte, attn []int64, hidden int) []byte {
	emb := onnx.MeanPool(out, attn, hidden)
	if len(emb) == 0 {
		return []byte(fmt.Sprintf("输出 %d 字节", len(out)))
	}
	n := 8
	if n > len(emb) {
		n = len(emb)
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		parts[i] = fmt.Sprintf("%.4f", emb[i])
	}
	return []byte(fmt.Sprintf("embedding[%d]: [%s …]", len(emb), strings.Join(parts, ", ")))
}
