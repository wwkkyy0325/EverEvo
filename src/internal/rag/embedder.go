//go:build windows

package rag

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"everevo/internal/toolbox"
)

// EmbedChunks batch-embeds a slice of texts using the ONNX model at modelDir.
// modelDir is the package directory (contains config.json + onnx/model.onnx or model.onnx).
func EmbedChunks(modelDir string, texts []string) ([][]float32, error) {
	meta := toolbox.Detect(modelDir)
	if meta.Type != toolbox.TypeSentenceEmbedding {
		return nil, fmt.Errorf("模型 %s 不是句向量模型（检测为 %s）", modelDir, meta.Type)
	}
	onnxPath, err := findOnnxInDir(modelDir)
	if err != nil {
		return nil, err
	}
	emb, err := toolbox.NewEmbedder(onnxPath, meta.Hidden)
	if err != nil {
		return nil, fmt.Errorf("加载嵌入模型失败: %w", err)
	}
	defer emb.Close()
	return emb.Embed(texts)
}

// EmbedQuery embeds a single query string.
func EmbedQuery(modelDir, query string) ([]float32, error) {
	vecs, err := EmbedChunks(modelDir, []string{query})
	if err != nil {
		return nil, err
	}
	return vecs[0], nil
}

// findOnnxInDir locates an .onnx file in the model package directory.
func findOnnxInDir(dir string) (string, error) {
	candidates := []string{filepath.Join(dir, "model.onnx"), filepath.Join(dir, "onnx", "model.onnx")}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	var found string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if found == "" && strings.EqualFold(filepath.Ext(p), ".onnx") {
			found = p
		}
		return nil
	})
	if found != "" {
		return found, nil
	}
	return "", fmt.Errorf("未在 %s 找到 .onnx 文件", dir)
}
