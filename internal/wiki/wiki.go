// Package wiki indexes the project's llmwiki markdown docs (and, later, a
// product-level user wiki) for retrieval: goldmark-parsed into heading-bounded
// chunks, embedded into a dedicated chromem collection, with page→page links
// stored as a SQLite graph. Recall surfaces relevant docs in the chat system
// prompt — making the design notes visible to the AI.
package wiki

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	_ "modernc.org/sqlite" // pure-Go SQLite (no CGo)

	chromem "github.com/philippgille/chromem-go"

	"everevo/internal/storage"
)

const collection = "wiki_docs"

// Chunk is a heading-bounded section of a wiki page.
type Chunk struct {
	Page    string `json:"page"`
	Heading string `json:"heading"`
	Content string `json:"content"`
}

// Link is a page→page reference (resolved from [text](target.md)).
type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ParseMarkdown walks the goldmark AST, producing heading-bounded chunks and
// extracting [text](target.md) page links. Source-file refs (.go/.ts/.vue,
// internal/ frontend/ docs/ prefixes) are skipped — only .md targets become
// edges.
func ParseMarkdown(pageName, src string) ([]Chunk, []Link) {
	md := goldmark.New()
	reader := text.NewReader([]byte(src))
	doc := md.Parser().Parse(reader)

	var chunks []Chunk
	var links []Link
	var heading string
	var buf bytes.Buffer

	flush := func() {
		body := strings.TrimSpace(buf.String())
		if body != "" {
			chunks = append(chunks, Chunk{Page: pageName, Heading: heading, Content: body})
		}
		buf.Reset()
	}

	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindHeading:
			flush()
			heading = string(n.Text(reader.Source()))
		case ast.KindParagraph:
			buf.Write(n.Text(reader.Source()))
			buf.WriteByte('\n')
		case ast.KindLink:
			if t := resolveLink(string(n.(*ast.Link).Destination)); t != "" {
				links = append(links, Link{From: pageName, To: t})
			}
		}
		return ast.WalkContinue, nil
	})
	flush()
	return chunks, links
}

// resolveLink returns the target page stem for a .md link (doc-relative), else "".
// Source-file refs and URLs are skipped.
func resolveLink(dest string) string {
	if strings.HasPrefix(dest, "http://") || strings.HasPrefix(dest, "https://") {
		return ""
	}
	if strings.HasSuffix(dest, ".go") || strings.HasSuffix(dest, ".ts") || strings.HasSuffix(dest, ".vue") {
		return ""
	}
	for _, p := range []string{"internal/", "frontend/", "app_"} {
		if strings.HasPrefix(dest, p) {
			return ""
		}
	}
	if !strings.HasSuffix(dest, ".md") {
		return ""
	}
	base := dest
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(base, ".md")
}

// Store holds the wiki vector index (chromem) + page graph (SQLite).
type Store struct {
	cdb       *chromem.DB
	col       *chromem.Collection
	sql       *sql.DB
	mu        sync.RWMutex
	libraryID string
}

// LibraryID returns the domain library this wiki store belongs to.
func (s *Store) LibraryID() string { return s.libraryID }

// NewStore opens a per-library wiki chromem DB + SQLite graph.
// libraryID identifies the domain; empty string = legacy global store.
func NewStore(libraryID string) (*Store, error) {
	base, err := storage.AppDataDir()
	if err != nil {
		return nil, err
	}
	wikiDir := filepath.Join(base, "wiki", libraryID)
	if libraryID == "" {
		wikiDir = filepath.Join(base, "wiki")
	}
	cdb, err := chromem.NewPersistentDB(filepath.Join(wikiDir, "chromem"), false)
	if err != nil {
		return nil, err
	}
	col := cdb.GetCollection(collection, nil)
	if col == nil {
		col, err = cdb.CreateCollection(collection, nil, nil)
		if err != nil {
			return nil, err
		}
	}
	sdb, err := sql.Open("sqlite", filepath.Join(wikiDir, "wiki.db"))
	if err != nil {
		return nil, err
	}
	sdb.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;")
	s := &Store{cdb: cdb, col: col, sql: sdb, libraryID: libraryID}
	// P7: workspace isolation column (idempotent — ignore duplicate-column error).
	_, _ = sdb.Exec(`ALTER TABLE wiki_pages ADD COLUMN workspace_id TEXT NOT NULL DEFAULT 'default'`)
	_, _ = sdb.Exec(`ALTER TABLE wiki_links ADD COLUMN workspace_id TEXT NOT NULL DEFAULT 'default'`)

	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.sql.Exec(`
CREATE TABLE IF NOT EXISTS wiki_pages(id TEXT PRIMARY KEY, title TEXT, path TEXT, modified INTEGER, chunk_count INTEGER);
CREATE TABLE IF NOT EXISTS wiki_links(src_page TEXT, dst_page TEXT, PRIMARY KEY(src_page, dst_page));`)
	if err != nil {
		return err
	}
	// User wiki: add source + content columns for user-created pages.
	for _, c := range []struct{ col, def string }{
		{"source", "TEXT NOT NULL DEFAULT 'llmwiki'"},
		{"content", "TEXT NOT NULL DEFAULT ''"},
	} {
		var n int
		_ = s.sql.QueryRow("SELECT COUNT(*) FROM pragma_table_info('wiki_pages') WHERE name = ?", c.col).Scan(&n)
		if n == 0 {
			_, _ = s.sql.Exec("ALTER TABLE wiki_pages ADD COLUMN " + c.col + " " + c.def)
		}
	}
	return nil
}

// Close releases both handles.
func (s *Store) Close() error { return s.sql.Close() }

// Clear wipes the index (used before a full reindex).
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cdb.DeleteCollection(collection); err != nil {
		return err
	}
	col, err := s.cdb.CreateCollection(collection, nil, nil)
	if err != nil {
		return err
	}
	s.col = col
	_, err = s.sql.Exec(`DELETE FROM wiki_pages; DELETE FROM wiki_links`)
	return err
}

// ClearLLMWiki wipes only the llmwiki pages, keeping user-created pages.
func (s *Store) ClearLLMWiki() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cdb.DeleteCollection(collection); err != nil {
		return err
	}
	col, err := s.cdb.CreateCollection(collection, nil, nil)
	if err != nil {
		return err
	}
	s.col = col
	_, err = s.sql.Exec(`DELETE FROM wiki_pages WHERE source = 'llmwiki' OR source = ''; DELETE FROM wiki_links`)
	// Re-index user pages into the fresh collection
	rows, _ := s.sql.Query(`SELECT id, title, content FROM wiki_pages WHERE source = 'user'`)
	if rows != nil {
		defer rows.Close()
		type up struct{ id, title, content string }
		var ups []up
		for rows.Next() {
			var u up
			if rows.Scan(&u.id, &u.title, &u.content) == nil {
				ups = append(ups, u)
			}
		}
		for _, u := range ups {
			chunks, _ := ParseMarkdown(u.id, u.content)
			// chunks added without embedding — reindex will need a new embedding pass
			// but for now metadata is preserved
			_, _ = s.sql.Exec(`INSERT OR REPLACE INTO wiki_pages(id, title, path, modified, chunk_count, source, content)
				VALUES(?, ?, '', ?, ?, 'user', ?)`, u.id, u.title, time.Now().Unix(), len(chunks), u.content)
		}
	}
	return err
}

// IndexPage adds a page's pre-embedded chunks. embeddings must align with chunks.
func (s *Store) IndexPage(pageID, title, path string, modified int64, chunks []Chunk, embeddings [][]float32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(chunks) > 0 {
		docs := make([]chromem.Document, len(chunks))
		for i, c := range chunks {
			docs[i] = chromem.Document{
				ID: fmt.Sprintf("%s_%d", pageID, i), Content: c.Content, Embedding: embeddings[i],
				Metadata: map[string]string{"page": pageID, "heading": c.Heading},
			}
		}
		if err := s.col.AddDocuments(context.Background(), docs, 4); err != nil {
			return err
		}
	}
	_, err := s.sql.Exec(`INSERT OR REPLACE INTO wiki_pages(id, title, path, modified, chunk_count) VALUES(?, ?, ?, ?, ?)`,
		pageID, title, path, modified, len(chunks))
	return err
}

// IndexLinks records page→page edges.
func (s *Store) IndexLinks(links []Link) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range links {
		if _, err := s.sql.Exec(`INSERT OR IGNORE INTO wiki_links(src_page, dst_page) VALUES(?, ?)`, l.From, l.To); err != nil {
			return err
		}
	}
	return nil
}

// Search returns up to k chunks nearest to emb.
func (s *Store) Search(emb []float32, k int) ([]Chunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := s.col.Count()
	if n == 0 || k <= 0 {
		return nil, nil
	}
	if k > n {
		k = n
	}
	res, err := s.col.QueryEmbedding(context.Background(), emb, k, nil, nil)
	if err != nil {
		return nil, err
	}
	out := make([]Chunk, len(res))
	for i, r := range res {
		out[i] = Chunk{Page: r.Metadata["page"], Heading: r.Metadata["heading"], Content: r.Content}
	}
	return out, nil
}

// SavePage inserts or updates a user-created wiki page.
// Chromem doesn't support fine-grained deletion; old chunks accumulate but
// are harmless (same IDs, same content). Full reindex clears everything.
func (s *Store) SavePage(pageID, title, content string, embedFn func([]string) ([][]float32, error)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()

	chunks, _ := ParseMarkdown(pageID, content)
	var texts []string
	for _, c := range chunks {
		texts = append(texts, c.Content)
	}
	if len(texts) > 0 && embedFn != nil {
		embs, err := embedFn(texts)
		if err != nil {
			return err
		}
		docs := make([]chromem.Document, len(chunks))
		for i, c := range chunks {
			docs[i] = chromem.Document{
				ID: fmt.Sprintf("%s_%d", pageID, i), Content: c.Content, Embedding: embs[i],
				Metadata: map[string]string{"page": pageID, "heading": c.Heading},
			}
		}
		if err := s.col.AddDocuments(context.Background(), docs, 4); err != nil {
			return err
		}
	}
	_, err := s.sql.Exec(`INSERT INTO wiki_pages(id, title, path, modified, chunk_count, source, content)
		VALUES(?, ?, '', ?, ?, 'user', ?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, modified=excluded.modified, chunk_count=excluded.chunk_count, content=excluded.content`,
		pageID, title, now, len(texts), content)
	return err
}

// DeletePage removes a user-created wiki page (chromem chunks remain until reindex).
func (s *Store) DeletePage(pageID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.sql.Exec(`DELETE FROM wiki_pages WHERE id = ? AND source = 'user'`, pageID)
	return err
}

// SavePageRaw stores a wiki page in the DB without embedding (no chromem index).
// Useful when the embedding model isn't loaded yet — the page can be re-indexed
// later via WikiReindex / ClearLLMWiki which picks up user pages from the DB.
func (s *Store) SavePageRaw(pageID, title, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	_, err := s.sql.Exec(`INSERT INTO wiki_pages(id, title, path, modified, chunk_count, source, content)
		VALUES(?, ?, '', ?, 0, 'user', ?)
		ON CONFLICT(id) DO UPDATE SET title=excluded.title, modified=excluded.modified, content=excluded.content`,
		pageID, title, now, content)
	return err
}

// GetPageContent returns the raw markdown content of a page.
func (s *Store) GetPageContent(pageID string) (string, error) {
	var content string
	err := s.sql.QueryRow(`SELECT content FROM wiki_pages WHERE id = ?`, pageID).Scan(&content)
	return content, err
}

// Status returns page + chunk counts.
func (s *Store) Status() (pages int, chunks int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_ = s.sql.QueryRow(`SELECT COUNT(*) FROM wiki_pages`).Scan(&pages)
	chunks = int(s.col.Count())
	return
}

// WikiPageInfo is a row from wiki_pages for listing.
type WikiPageInfo struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Path      string `json:"path"`
	ChunkCount int   `json:"chunkCount"`
	Source     string `json:"source"`
}

// ListPages returns all indexed pages.
func (s *Store) ListPages() ([]WikiPageInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows, err := s.sql.Query(`SELECT id, title, path, chunk_count, source FROM wiki_pages ORDER BY path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WikiPageInfo
	for rows.Next() {
		var p WikiPageInfo
		if err := rows.Scan(&p.ID, &p.Title, &p.Path, &p.ChunkCount, &p.Source); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

