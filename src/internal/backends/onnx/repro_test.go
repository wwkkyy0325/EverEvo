//go:build windows

package onnx

import (
	"os"
	"path/filepath"
	"testing"

	"everevo/internal/tokenizer"
)

// findTestDLL 在常见位置查找 onnxruntime.dll：测试二进制旁（bundled 1.26）
// → third_party → build/bin → System32。
func findTestDLL() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "onnxruntime.dll"))
	}
	if wd, err := os.Getwd(); err == nil {
		root := filepath.Join(wd, "..", "..", "..")
		candidates = append(candidates,
			filepath.Join(root, "third_party", "onnxruntime", "win-x64", "onnxruntime.dll"),
			filepath.Join(root, "build", "bin", "onnxruntime.dll"),
		)
	}
	if sr := os.Getenv("SystemRoot"); sr != "" {
		candidates = append(candidates, filepath.Join(sr, "System32", "onnxruntime.dll"))
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// TestLoadAndRun 验证 ONNX 绑定 + tokenizer：用真实分词把文本编码为
// input_ids/attention_mask/token_type_ids，喂给 MiniLM 并跑通推理（不崩溃）。
// 需要 onnxruntime 1.26+ 的 DLL（yalue 锁定 ORT API 26）。
func TestLoadAndRun(t *testing.T) {
	dll := findTestDLL()
	if dll == "" {
		t.Skip("未找到 onnxruntime.dll，跳过 smoke 测试")
	}
	if err := Init(dll); err != nil {
		t.Skipf("ONNX Init 失败（可能是 DLL 版本过低，需要 1.26+）: %v", err)
	}
	defer Close()

	wd, _ := os.Getwd()
	model := filepath.Join(wd, "..", "..", "..", "data", "models",
		"sentence-transformers_all-MiniLM-L6-v2", "onnx", "model.onnx")
	if _, err := os.Stat(model); err != nil {
		t.Skipf("未找到测试模型 %s: %v", model, err)
	}

	sess, err := LoadModel(model)
	if err != nil {
		t.Fatalf("LoadModel 失败: %v", err)
	}
	defer sess.Close()
	t.Logf("加载成功: %d 个输入, %d 个输出", len(sess.Input), len(sess.Output))

	tok, err := tokenizer.New(model)
	if err != nil {
		t.Fatalf("tokenizer 加载失败: %v", err)
	}
	ids, attn, types, err := tok.Encode("hello world from EverEvo")
	if err != nil {
		t.Fatalf("tokenize 失败: %v", err)
	}
	t.Logf("tokenize: %d tokens", len(ids))

	out, err := sess.RunInt64(map[string][]int64{
		"input_ids":      ids,
		"attention_mask": attn,
		"token_type_ids": types,
	})
	if err != nil {
		t.Fatalf("RunInt64 失败: %v", err)
	}
	t.Logf("Run 成功，输出 %d 字节（%d 个 float32）", len(out), len(out)/4)
	if len(out) == 0 {
		t.Errorf("输出为空")
	}
}
