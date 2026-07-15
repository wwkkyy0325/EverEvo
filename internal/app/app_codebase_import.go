//go:build windows

package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"everevo/internal/rag"
)

// CodebaseImportResult holds the statistics from a codebase import run.
type CodebaseImportResult struct {
	Packages  int   `json:"packages"`
	Files     int   `json:"files"`
	RagChunks int   `json:"ragChunks"`
	KgNodes   int   `json:"kgNodes"`
	KgEdges   int   `json:"kgEdges"`
	WikiPages int   `json:"wikiPages"`
	Errors    int   `json:"errors"`
	ElapsedMs int64 `json:"elapsedMs"`
}

// CodebaseImport scans the project's internal/ directory, parses Go source
// files, and generates wiki pages + knowledge graph edges + RAG vector chunks.
func (a *App) CodebaseImport(libraryID string) (*CodebaseImportResult, error) {
	if a.memoryStore == nil {
		return nil, fmt.Errorf("memory store 未就绪")
	}
	if libraryID == "" {
		libID, _ := a.memoryStore.DefaultLibrary()
		libraryID = libID
	}

	root := filepath.Join(projectRoot(), "internal")
	if _, err := os.Stat(root); err != nil {
		return nil, fmt.Errorf("internal/ 目录不存在: %s", root)
	}

	dir := a.memoryStore.EmbeddingModelDir()

	result := &CodebaseImportResult{}
	start := time.Now()
	log.Printf("[codebase] 开始扫描 %s", root)

	// ── Pass: collect parsed Go files ──
	type fileInfo struct {
		path    string
		pkg     string
		imports []string
		types   []string
		content string
	}
	var files []fileInfo
	pkgFiles := map[string][]fileInfo{}
	fset := token.NewFileSet()

	filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		result.Files++
		af, parseErr := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if parseErr != nil {
			result.Errors++
			return nil
		}
		pkg := af.Name.Name
		f := fileInfo{path: path, pkg: pkg}
		for _, imp := range af.Imports {
			f.imports = append(f.imports, strings.Trim(imp.Path.Value, `"`))
		}
		ast.Inspect(af, func(n ast.Node) bool {
			if t, ok := n.(*ast.TypeSpec); ok && t.Name.IsExported() {
				f.types = append(f.types, pkg+"."+t.Name.Name)
			}
			return true
		})
		if content, err := os.ReadFile(path); err == nil {
			f.content = string(content)
		}
		files = append(files, f)
		pkgFiles[pkg] = append(pkgFiles[pkg], f)
		return nil
	})

	// ── 1. Wiki pages per package ──
	for pkg, pfiles := range pkgFiles {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("# %s\n\n%d 文件", pkg, len(pfiles)))
		imps := map[string]bool{}
		types := map[string]bool{}
		for _, f := range pfiles {
			for _, imp := range f.imports {
				if strings.Contains(imp, "everevo/") {
					imps[imp] = true
				}
			}
			for _, t := range f.types {
				types[t] = true
			}
		}
		if len(types) > 0 {
			sb.WriteString("\n\n## 导出类型\n")
			for t := range types {
				sb.WriteString(fmt.Sprintf("- `%s`\n", t))
			}
		}
		if len(imps) > 0 {
			sb.WriteString("\n## 内部依赖\n")
			for imp := range imps {
				sb.WriteString(fmt.Sprintf("- `%s`\n", imp))
			}
		}
		sb.WriteString("\n## 文件\n")
		for _, f := range pfiles {
			rel, _ := filepath.Rel(root, f.path)
			sb.WriteString(fmt.Sprintf("- `%s`\n", rel))
		}
		a.memoryStore.WikiSaveRaw("codebase/pkg/"+pkg, sb.String(), libraryID)
		result.WikiPages++
	}

	// ── 2. KG nodes for types ──
	seenNodes := map[string]bool{}
	for _, f := range files {
		for _, t := range f.types {
			if seenNodes[t] {
				continue
			}
			seenNodes[t] = true
			_ = a.memoryStore.AddGraphNodeRaw(t, "type", libraryID)
			result.KgNodes++
		}
	}

	// ── 3. KG edges for imports ──
	seenEdges := map[string]bool{}
	for _, f := range files {
		for _, imp := range f.imports {
			if !strings.Contains(imp, "everevo/") {
				continue
			}
			parts := strings.Split(imp, "/")
			toPkg := parts[len(parts)-1]
			edgeKey := f.pkg + "→" + toPkg
			if seenEdges[edgeKey] || f.pkg == toPkg {
				continue
			}
			seenEdges[edgeKey] = true
			_ = a.memoryStore.AddGraphEdgeRaw(f.pkg, "imports", toPkg, libraryID)
			result.KgEdges++
		}
	}

	// ── 4. RAG vector chunks ──
	if dir != "" {
		const chunkSize = 3072
		for _, f := range files {
			if len(f.content) == 0 {
				continue
			}
			c := f.content
			chunkIdx := 0
			for offset := 0; offset < len(c); chunkIdx++ {
				end := offset + chunkSize
				if end > len(c) {
					end = len(c)
				}
				chunk := c[offset:end]
				offset = end
				emb, embErr := rag.EmbedQuery(dir, chunk)
				if embErr != nil {
					continue
				}
				chunkID := fmt.Sprintf("code_%s_%d", filepath.Base(f.path), chunkIdx)
				_ = a.memoryStore.AddFactMemory(chunkID, chunk, "source", "normal", libraryID, "[]", emb)
				result.RagChunks++
			}
		}
	}

	result.Packages = len(pkgFiles)
	result.ElapsedMs = time.Since(start).Milliseconds()
	log.Printf("[codebase] done: %d pkg, %d files, %d wiki, %d nodes, %d edges, %d chunks, %d err (%dms)",
		result.Packages, result.Files, result.WikiPages, result.KgNodes, result.KgEdges, result.RagChunks, result.Errors, result.ElapsedMs)
	return result, nil
}

func projectRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}
