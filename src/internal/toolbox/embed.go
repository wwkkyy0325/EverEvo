//go:build windows

package toolbox

import (
	"fmt"
	"math"

	"everevo/internal/backends/onnx"
	"everevo/internal/tokenizer"
)

// Embedder 封装一个已加载的句向量模型（tokenizer + ONNX 会话）。
type Embedder struct {
	tok    *tokenizer.Tokenizer
	sess   *onnx.Session
	hidden int
}

// NewEmbedder 加载句向量模型。hidden 为嵌入维度（来自 config.json hidden_size）。
func NewEmbedder(modelPath string, hidden int) (*Embedder, error) {
	tok, err := tokenizer.New(modelPath)
	if err != nil {
		return nil, fmt.Errorf("加载 tokenizer 失败: %w", err)
	}
	sess, err := onnx.LoadModel(modelPath)
	if err != nil {
		return nil, fmt.Errorf("加载 ONNX 模型失败: %w", err)
	}
	if hidden <= 0 {
		hidden = 384
	}
	return &Embedder{tok: tok, sess: sess, hidden: hidden}, nil
}

// Close 释放会话资源。
func (e *Embedder) Close() {
	if e.sess != nil {
		e.sess.Close()
	}
}

// Embed 把多段文本编码为句嵌入（attention-mask 加权 mean-pool）。
func (e *Embedder) Embed(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		ids, attn, types, err := e.tok.Encode(t)
		if err != nil {
			return nil, fmt.Errorf("tokenize 第 %d 段失败: %w", i, err)
		}
		raw, err := e.sess.RunInt64(map[string][]int64{
			"input_ids":      ids,
			"attention_mask": attn,
			"token_type_ids": types,
		})
		if err != nil {
			return nil, fmt.Errorf("推理第 %d 段失败: %w", i, err)
		}
		emb := onnx.MeanPool(raw, attn, e.hidden)
		if len(emb) == 0 {
			return nil, fmt.Errorf("第 %d 段嵌入为空", i)
		}
		out[i] = emb
	}
	return out, nil
}

// Cosine 计算两个向量的余弦相似度（范围 -1..1）。
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Hit 是一次搜索命中。
type Hit struct {
	Index int     // corpus 索引
	Score float64 // 余弦相似度
}

// Search 返回 corpus 中与 query 最相似的 Top-K 命中（按相似度降序）。
func Search(query []float32, corpus [][]float32, k int) []Hit {
	if k <= 0 || len(corpus) == 0 {
		return nil
	}
	hits := make([]Hit, len(corpus))
	for i, c := range corpus {
		hits[i] = Hit{Index: i, Score: Cosine(query, c)}
	}
	// 部分选择排序取 Top-K（corpus 通常不大）
	limit := k
	if limit > len(hits) {
		limit = len(hits)
	}
	for i := 0; i < limit; i++ {
		max := i
		for j := i + 1; j < len(hits); j++ {
			if hits[j].Score > hits[max].Score {
				max = j
			}
		}
		hits[i], hits[max] = hits[max], hits[i]
	}
	return hits[:limit]
}
