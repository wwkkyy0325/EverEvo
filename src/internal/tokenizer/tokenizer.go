//go:build windows

package tokenizer

import (
	"fmt"
	"os"
	"path/filepath"

	sugarme "github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
)

// maxLen 是 MiniLM ONNX 导出时的截断/序列长度（tokenizer.json truncation.max_length=128）。
const maxLen = 128

// Tokenizer 包装一个针对特定模型目录加载的 sugarme 分词器。
type Tokenizer struct {
	tk *sugarme.Tokenizer
}

// New 从模型文件附近加载 tokenizer.json。
// MiniLM 布局：<pkg>/onnx/model.onnx，tokenizer.json 在 <pkg>/tokenizer.json，
// 因此从 modelPath 所在目录向上查找。
func New(modelPath string) (*Tokenizer, error) {
	p := locateTokenizerJSON(modelPath)
	if p == "" {
		return nil, fmt.Errorf("未找到 tokenizer.json（从 %s 向上查找）", modelPath)
	}
	tk, err := pretrained.FromFile(p)
	if err != nil {
		return nil, fmt.Errorf("加载 tokenizer.json 失败: %w", err)
	}
	// 截断到 maxLen（匹配 MiniLM 导出配置）。不固定 padding——用动态 seqlen 更高效，
	// mean-pool 时 attention_mask 全 1 即可。
	tk.WithTruncation(&sugarme.TruncationParams{
		MaxLength: maxLen,
		Strategy:  sugarme.LongestFirst,
	})
	return &Tokenizer{tk: tk}, nil
}

// locateTokenizerJSON 从 modelPath 所在目录向上查找 tokenizer.json（最多 4 层）。
func locateTokenizerJSON(modelPath string) string {
	dir := filepath.Dir(modelPath)
	for i := 0; i < 4; i++ {
		cand := filepath.Join(dir, "tokenizer.json")
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// Encode 把文本编码为 BERT 三元组（input_ids / attention_mask / token_type_ids）。
// 长度为实际 token 数（含 [CLS]/[SEP]，已截断到 maxLen），不 padding。
func (t *Tokenizer) Encode(text string) (inputIds, attentionMask, tokenTypeIds []int64, err error) {
	// Guard against tokenizer panics (sugarme/tokenizer has known slice-bounds bugs
	// on edge-case unicode sequences). Recover and return an error instead of crashing.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("tokenizer panic (text len=%d): %v", len(text), r)
			inputIds, attentionMask, tokenTypeIds = nil, nil, nil
		}
	}()
	// Truncate very long text before tokenizing — the BERT tokenizer has a
	// 512-token limit and long inputs trigger pathological edge cases.
	if len(text) > 4096 {
		text = text[:4096]
	}
	enc, err := t.tk.EncodeSingle(text, true)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("tokenize 失败: %w", err)
	}
	inputIds = toInt64(enc.Ids)
	if len(inputIds) == 0 {
		return nil, nil, nil, fmt.Errorf("tokenize 结果为空")
	}
	attentionMask = toInt64(enc.AttentionMask)
	tokenTypeIds = toInt64(enc.TypeIds)
	// 兜底：部分分词器不返回 attention_mask / token_type_ids，按 BERT 单句语义补齐
	if len(attentionMask) < len(inputIds) {
		am := make([]int64, len(inputIds))
		for i := range am {
			if i < len(attentionMask) {
				am[i] = attentionMask[i]
			} else {
				am[i] = 1 // 真实 token
			}
		}
		attentionMask = am
	}
	if len(tokenTypeIds) < len(inputIds) {
		tt := make([]int64, len(inputIds))
		for i := range tt {
			if i < len(tokenTypeIds) {
				tt[i] = tokenTypeIds[i]
			}
		}
		tokenTypeIds = tt // 单句全 0
	}
	return inputIds, attentionMask, tokenTypeIds, nil
}

func toInt64(in []int) []int64 {
	out := make([]int64, len(in))
	for i, v := range in {
		out[i] = int64(v)
	}
	return out
}
