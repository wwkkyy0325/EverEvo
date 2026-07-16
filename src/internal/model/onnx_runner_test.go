//go:build windows

package model

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"everevo/internal/backends/onnx"
)

// TestONNXRunnerEndToEnd 走 app 实际路径：NewONNXRunner（含 tokenizer）→ Load → Run，
// 验证文本被正确分词、推理、按 attention_mask 加权 mean-pool，产出 embedding 摘要。
func TestONNXRunnerEndToEnd(t *testing.T) {
	dll := findModelTestDLL()
	if dll == "" {
		t.Skip("未找到 onnxruntime.dll，跳过")
	}
	if err := onnx.Init(dll); err != nil {
		t.Skipf("onnx Init 失败（需 1.26+）: %v", err)
	}
	defer onnx.Close()

	wd, _ := os.Getwd()
	model := filepath.Join(wd, "..", "..", "data", "models",
		"sentence-transformers_all-MiniLM-L6-v2", "onnx", "model.onnx")
	if _, err := os.Stat(model); err != nil {
		t.Skipf("未找到测试模型: %v", err)
	}

	m, err := NewONNXRunner("e2e", "MiniLM", model)
	if err != nil {
		t.Fatalf("NewONNXRunner: %v", err)
	}
	if err := m.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	defer m.Unload()

	out, err := m.Run(context.Background(), []byte("hello world from EverEvo"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	s := string(out)
	t.Logf("Run 输出: %s", s)
	if !strings.HasPrefix(s, "embedding[") {
		t.Errorf("输出不像 embedding 摘要: %q", s)
	}
}

// findModelTestDLL 在 third_party / build/bin / System32 查找 onnxruntime.dll。
func findModelTestDLL() string {
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
