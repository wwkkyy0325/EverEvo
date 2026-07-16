//go:build windows

package onnx

import (
	"errors"
	"fmt"
	"unsafe"

	ort "github.com/yalue/onnxruntime_go"
)

// TensorInfo 输入/输出张量元数据（仅名称，保持旧导出字段兼容）。
type TensorInfo struct {
	Name string `json:"name"`
}

// Session 封装一个已加载的 ONNX 模型会话。对外保持导出面：
// Input/Output []TensorInfo、RunInt64(map[string][]int64)、Close()。
type Session struct {
	sess       *ort.DynamicAdvancedSession
	Input      []TensorInfo `json:"input"`
	Output     []TensorInfo `json:"output"`
	inputNames []string
}

// LoadModel 加载 ONNX 模型文件。用 GetInputOutputInfo 读取输入/输出名称，
// 再创建 DynamicAdvancedSession（无需在创建时绑定固定张量）。
func LoadModel(modelPath string) (*Session, error) {
	if !Initialized() {
		return nil, errors.New("ONNX Runtime 未初始化，请先调用 Init")
	}

	ins, outs, err := ort.GetInputOutputInfo(modelPath)
	if err != nil {
		return nil, fmt.Errorf("读取模型输入输出信息失败: %w", err)
	}

	inNames := make([]string, len(ins))
	inInfos := make([]TensorInfo, len(ins))
	for i, in := range ins {
		inNames[i] = in.Name
		inInfos[i] = TensorInfo{Name: in.Name}
	}
	outNames := make([]string, len(outs))
	outInfos := make([]TensorInfo, len(outs))
	for i, o := range outs {
		outNames[i] = o.Name
		outInfos[i] = TensorInfo{Name: o.Name}
	}

	sess, err := ort.NewDynamicAdvancedSession(modelPath, inNames, outNames, nil)
	if err != nil {
		return nil, fmt.Errorf("创建 ONNX 会话失败: %w", err)
	}

	return &Session{
		sess:       sess,
		Input:      inInfos,
		Output:     outInfos,
		inputNames: inNames,
	}, nil
}

// RunInt64 对每个输入名构造一个独立的 int64 张量（shape [1, len(values)]），
// 执行推理，返回第一个输出的原始字节数据。输入名缺失时用零填充。
func (s *Session) RunInt64(inputs map[string][]int64) ([]byte, error) {
	if s.sess == nil {
		return nil, errors.New("会话未初始化")
	}
	if len(s.inputNames) == 0 {
		return nil, errors.New("模型无输入")
	}

	vals := make([]ort.Value, len(s.inputNames))
	for i, name := range s.inputNames {
		data := inputs[name]
		if len(data) == 0 {
			data = []int64{0}
		}
		shape := ort.NewShape(1, int64(len(data)))
		t, err := ort.NewTensor[int64](shape, data)
		if err != nil {
			for _, v := range vals {
				if v != nil {
					v.Destroy()
				}
			}
			return nil, fmt.Errorf("创建输入 %q 张量失败: %w", name, err)
		}
		vals[i] = t
	}
	defer func() {
		for _, v := range vals {
			if v != nil {
				v.Destroy()
			}
		}
	}()

	// outputs 元素留 nil，让 ORT 自动分配并按真实大小回填。
	outputs := make([]ort.Value, len(s.Output))
	if err := s.sess.Run(vals, outputs); err != nil {
		return nil, fmt.Errorf("推理失败: %w", err)
	}
	defer func() {
		for _, o := range outputs {
			if o != nil {
				o.Destroy()
			}
		}
	}()

	if len(outputs) == 0 || outputs[0] == nil {
		return nil, errors.New("推理返回空")
	}
	return tensorBytes(outputs[0])
}

// Close 释放模型会话资源。
func (s *Session) Close() {
	if s.sess != nil {
		_ = s.sess.Destroy()
		s.sess = nil
	}
}

// MeanPool 把 [seq, hidden] 的 float32 字节输出按 attention_mask 加权平均为 [hidden]。
// attentionMask[i]=1 表示真实 token，0 表示 pad。hidden 为隐藏维度。
// 用于 sentence-transformers 类模型得到句嵌入。返回 nil 表示输出形状不匹配。
func MeanPool(out []byte, attentionMask []int64, hidden int) []float32 {
	if hidden <= 0 || len(out) < 4 || len(out)%4 != 0 {
		return nil
	}
	floats := bytesToFloat32(out)
	if len(floats) == 0 || len(floats)%hidden != 0 || len(floats)/hidden < 1 {
		return nil
	}
	seq := len(floats) / hidden
	emb := make([]float32, hidden)
	var maskSum float32
	for i := 0; i < seq; i++ {
		var w float32 = 1
		if i < len(attentionMask) {
			w = float32(attentionMask[i])
		}
		maskSum += w
		for j := 0; j < hidden; j++ {
			emb[j] += floats[i*hidden+j] * w
		}
	}
	if maskSum > 0 {
		for j := range emb {
			emb[j] /= maskSum
		}
	}
	return emb
}

// bytesToFloat32 把 little-endian float32 字节解为 []float32（零拷贝视图）。
func bytesToFloat32(b []byte) []float32 {
	n := len(b) / 4
	if n == 0 {
		return nil
	}
	return unsafe.Slice((*float32)(unsafe.Pointer(&b[0])), n)
}

// tensorBytes 把 ORT 自动分配的输出 Value 转为（拷贝出的）原始字节。
// 拷贝以解耦返回字节与 Value 的生命周期（Value 会在 defer 中 Destroy）。
func tensorBytes(v ort.Value) ([]byte, error) {
	switch t := v.(type) {
	case *ort.Tensor[float32]:
		return sliceBytes(t.GetData()), nil
	case *ort.Tensor[int64]:
		return sliceBytes(t.GetData()), nil
	case *ort.Tensor[int32]:
		return sliceBytes(t.GetData()), nil
	case *ort.Tensor[float64]:
		return sliceBytes(t.GetData()), nil
	case *ort.Tensor[uint8]:
		return cloneBytes(t.GetData()), nil
	case *ort.Tensor[bool]:
		return sliceBytes(t.GetData()), nil
	case *ort.CustomDataTensor:
		return cloneBytes(t.GetData()), nil
	default:
		return nil, fmt.Errorf("不支持的输出张量类型 %T", t)
	}
}

// sliceBytes 把 typed 切片按字节视图拷贝为独立的 []byte。
func sliceBytes[T any](s []T) []byte {
	if len(s) == 0 {
		return []byte{}
	}
	view := unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*int(unsafe.Sizeof(s[0])))
	out := make([]byte, len(view))
	copy(out, view)
	return out
}

// cloneBytes 返回 b 的拷贝（与底层 Value 解耦）。
func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
