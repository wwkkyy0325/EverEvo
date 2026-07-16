//go:build windows

package toolbox

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Type is the detected model type.
type Type string

const (
	TypeSentenceEmbedding Type = "sentence-embedding"
	TypeUnknown           Type = "unknown"
)

// ModelMeta holds the detection result for a model package.
type ModelMeta struct {
	Dir    string
	Type   Type
	Hidden int // embedding dimension (from config.json hidden_size)
}

type hfConfig struct {
	Architectures []string `json:"architectures"`
	ModelType     string   `json:"model_type"`
	HiddenSize    int      `json:"hidden_size"`
}

// Detect probes a downloaded model package to determine its type.
// Detection priority: 1_Pooling/config_sentence_transformers.json (strong, sentence-embedding)
// > config.json architectures/model_type (weak, heuristic).
func Detect(pkgDir string) ModelMeta {
	m := ModelMeta{Dir: pkgDir, Type: TypeUnknown}
	cfg := readHFConfig(pkgDir)
	m.Hidden = cfg.HiddenSize

	// Strong signal: sentence-transformers pooler or config
	if fileExists(filepath.Join(pkgDir, "1_Pooling", "config.json")) ||
		fileExists(filepath.Join(pkgDir, "config_sentence_transformers.json")) {
		m.Type = TypeSentenceEmbedding
		return m
	}
	// Weak signal: architecture / model_type heuristics
	if isTextModel(cfg) {
		m.Type = TypeSentenceEmbedding
		return m
	}
	return m
}

func isTextModel(c hfConfig) bool {
	for _, a := range c.Architectures {
		s := strings.ToLower(a)
		if strings.Contains(s, "bert") || strings.Contains(s, "distil") ||
			strings.Contains(s, "mpnet") || strings.Contains(s, "sentence") ||
			strings.Contains(s, "embed") {
			return true
		}
	}
	switch strings.ToLower(c.ModelType) {
	case "bert", "distilbert", "mpnet", "xlm-roberta", "roberta",
		"e5", "bge", "gte", "nomic", "jina":
		return true
	}
	return false
}

func readHFConfig(dir string) hfConfig {
	var c hfConfig
	for _, rel := range []string{"config.json", "onnx/config.json"} {
		b, err := os.ReadFile(filepath.Join(dir, rel))
		if err != nil {
			continue
		}
		_ = json.Unmarshal(b, &c)
		if c.HiddenSize > 0 || len(c.Architectures) > 0 || c.ModelType != "" {
			return c
		}
	}
	return c
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }
