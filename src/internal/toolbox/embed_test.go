//go:build windows

package toolbox

import (
	"os"
	"path/filepath"
	"testing"

	"everevo/internal/backends/onnx"
)

func findEmbedTestDLL() string {
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		root := filepath.Join(wd, "..", "..")
		candidates = append(candidates,
			filepath.Join(root, "third_party", "onnxruntime", "win-x64", "onnxruntime.dll"),
			filepath.Join(root, "build", "bin", "onnxruntime.dll"),
		)
	}
	if sr := os.Getenv("SystemRoot"); sr != "" {
		candidates = append(candidates, filepath.Join(sr, "System32", "onnxruntime.dll"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// TestEmbedSimilarity 验证句向量工具：近义句相似度应高于无关句，搜索 Top-1 应为近义句。
func TestEmbedSimilarity(t *testing.T) {
	dll := findEmbedTestDLL()
	if dll == "" {
		t.Skip("未找到 onnxruntime.dll，跳过")
	}
	if err := onnx.Init(dll); err != nil {
		t.Skipf("ONNX Init 失败（需 1.26+）: %v", err)
	}
	defer onnx.Close()

	wd, _ := os.Getwd()
	modelDir := filepath.Join(wd, "..", "..", "data", "models",
		"sentence-transformers_all-MiniLM-L6-v2")
	if _, err := os.Stat(filepath.Join(modelDir, "config.json")); err != nil {
		t.Skipf("未找到测试模型: %v", err)
	}

	meta := Detect(modelDir)
	if meta.Type != TypeSentenceEmbedding {
		t.Fatalf("探测类型应为 sentence-embedding，实得 %s", meta.Type)
	}
	t.Logf("探测: type=%s hidden=%d", meta.Type, meta.Hidden)

	onnxPath := filepath.Join(modelDir, "onnx", "model.onnx")
	if _, err := os.Stat(onnxPath); err != nil {
		t.Skipf("未找到 ONNX 文件: %v", err)
	}

	emb, err := NewEmbedder(onnxPath, meta.Hidden)
	if err != nil {
		t.Fatalf("NewEmbedder: %v", err)
	}
	defer emb.Close()

	vecs, err := emb.Embed([]string{
		"猫在睡觉",
		"小猫睡着了",
		"今天的股市行情不错",
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	syn := Cosine(vecs[0], vecs[1])
	unrel := Cosine(vecs[0], vecs[2])
	t.Logf("近义相似度=%.4f 无关相似度=%.4f", syn, unrel)
	if syn <= unrel {
		t.Errorf("近义句相似度(%.4f)应高于无关句(%.4f)", syn, unrel)
	}

	hits := Search(vecs[0], [][]float32{vecs[1], vecs[2]}, 2)
	if len(hits) != 2 || hits[0].Index != 0 {
		t.Errorf("Top-1 应为近义句(index 0)，实得 %v", hits)
	}
}
