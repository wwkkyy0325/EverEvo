package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGo)

	"everevo/internal/storage"
)

// Session is a persisted chat conversation.
type Session struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	AgentID   string `json:"agentId"`
	Summary   string `json:"summary"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Message is a single turn inside a Session.
type Message struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Seq       int    `json:"seq"`
	Role      string `json:"role"` // user | assistant | tool | system
	Content   string `json:"content"`
	ToolJSON  string `json:"toolJson"` // tool_calls / tool results (opaque JSON)
	CreatedAt int64  `json:"createdAt"`
}

// Store wraps a SQLite database holding chat sessions and messages (and, in P2,
// the temporal knowledge graph). SQLite is accessed via the pure-Go modernc
// driver — no CGo — keeping the Wails build single-toolchain.
type Store struct {
	db     *sql.DB
	vector *VectorStore // may be nil → degraded (SQLite-only) mode
}

// NewStore opens (or creates) the memory SQLite DB under data/memory/memory.db
// and runs idempotent schema migrations.
func NewStore() (*Store, error) {
	base, err := storage.AppDataDir()
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(base, "memory", "memory.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("打开记忆数据库失败: %w", err)
	}
	// WAL + busy_timeout is the SQLite sweet spot for a single-process desktop app.
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("设置 WAL 失败: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("记忆库迁移失败: %w", err)
	}
	// Vector layer is best-effort: if chromem fails to init, memory degrades to
	// SQLite-only persistence (no semantic recall). The app layer binds the
	// embedding model via SetEmbeddingModel.
	if vs, vErr := NewVectorStore(); vErr != nil {
		log.Printf("[memory] 向量层初始化失败，降级为仅持久化: %v", vErr)
	} else {
		s.vector = vs
	}
	return s, nil
}

// migrate creates tables idempotently. P0 ships sessions + messages + memory_items;
// P2 adds the temporal knowledge graph (kg_nodes / kg_edges).
func (s *Store) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS sessions (
	id         TEXT PRIMARY KEY,
	title      TEXT NOT NULL,
	agent_id   TEXT NOT NULL DEFAULT '',
	summary    TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS messages (
	id         TEXT PRIMARY KEY,
	session_id TEXT NOT NULL,
	seq        INTEGER NOT NULL,
	role       TEXT NOT NULL,
	content    TEXT NOT NULL DEFAULT '',
	tool_json  TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_messages_session ON messages(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_sessions_updated ON sessions(updated_at DESC);
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS memory_items (
	id         TEXT PRIMARY KEY,
	kind       TEXT NOT NULL,            -- turn | fact
	content    TEXT NOT NULL,            -- turn: userText; fact: fact text
	reply      TEXT NOT NULL DEFAULT '',  -- turn: assistant reply
	category   TEXT NOT NULL DEFAULT '',  -- fact: preference|fact|event|relationship
	session_id TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_memory_kind ON memory_items(kind);
CREATE INDEX IF NOT EXISTS idx_memory_created ON memory_items(created_at DESC);
-- P2: temporal knowledge graph. kg_ prefix keeps these visually distinct.
CREATE TABLE IF NOT EXISTS kg_nodes (
	id           TEXT PRIMARY KEY,
	type         TEXT NOT NULL DEFAULT '',   -- entity type: person/place/project/...
	name         TEXT NOT NULL,              -- normalized (lowercase+trim) → disambiguation key
	name_raw     TEXT NOT NULL DEFAULT '',   -- original surface form
	props        TEXT NOT NULL DEFAULT '{}', -- JSON
	embedding_id TEXT NOT NULL DEFAULT '',   -- chromem doc id (kind=entity)
	created_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_kg_nodes_name ON kg_nodes(name);
CREATE TABLE IF NOT EXISTS kg_edges (
	id          TEXT PRIMARY KEY,
	src_id      TEXT NOT NULL,
	dst_id      TEXT NOT NULL,
	type        TEXT NOT NULL,               -- relation: likes/works_at/owns/...
	props       TEXT NOT NULL DEFAULT '{}',
	valid_from  INTEGER NOT NULL,            -- fact lifetime start
	valid_to    INTEGER,                     -- NULL = currently valid (bi-temporal)
	recorded_at INTEGER NOT NULL,            -- when the system learned it
	session_id  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_kg_edges_src ON kg_edges(src_id);
CREATE INDEX IF NOT EXISTS idx_kg_edges_dst ON kg_edges(dst_id);
CREATE INDEX IF NOT EXISTS idx_kg_edges_valid ON kg_edges(valid_to);
-- P7: domain libraries (AI-managed knowledge domains, replaces workspaces).
CREATE TABLE IF NOT EXISTS domain_libraries (
	id          TEXT PRIMARY KEY,
	name        TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	tags        TEXT NOT NULL DEFAULT '[]',
	auto_created INTEGER NOT NULL DEFAULT 0,
	created_at  INTEGER NOT NULL
);
-- P7 legacy: workspace table kept for migration compatibility.
CREATE TABLE IF NOT EXISTS everevo_workspaces (
	id         TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	created_at INTEGER NOT NULL
);
-- P5: core memory (identity/preferences/constraints) — permanent, never decayed/TTL'd.
CREATE TABLE IF NOT EXISTS user_facts (
	id           TEXT PRIMARY KEY,
	key          TEXT NOT NULL,
	value        TEXT NOT NULL,
	category     TEXT NOT NULL DEFAULT '',
	importance   TEXT NOT NULL DEFAULT 'high',
	locked       INTEGER NOT NULL DEFAULT 0,
	source       TEXT NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	last_access  INTEGER NOT NULL,
	access_count INTEGER NOT NULL DEFAULT 0
);
-- P8: cross-domain entity links (semantic anchors between domain KGs).
CREATE TABLE IF NOT EXISTS entity_links (
	id          TEXT PRIMARY KEY,
	src_node_id TEXT NOT NULL,
	dst_node_id TEXT NOT NULL,
	link_type   TEXT NOT NULL,
	confidence  REAL NOT NULL DEFAULT 0.5,
	source      TEXT NOT NULL DEFAULT 'auto',
	created_at  INTEGER NOT NULL
);
-- P8: evolution metrics (agent self-improvement tracking).
CREATE TABLE IF NOT EXISTS evolution_metrics (
	domain_id   TEXT NOT NULL,
	date        TEXT NOT NULL,
	total_turns INTEGER NOT NULL DEFAULT 0,
	reflected_turns INTEGER NOT NULL DEFAULT 0,
	experience_recalls INTEGER NOT NULL DEFAULT 0,
	cross_domain_links INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (domain_id, date)
);
-- P9: dream candidates (Light→REM→Deep pipeline staging).
CREATE TABLE IF NOT EXISTS dream_candidates (
	id          TEXT PRIMARY KEY,
	source_id   TEXT NOT NULL,
	source_type TEXT NOT NULL,
	stage       TEXT NOT NULL DEFAULT 'light',
	score       REAL NOT NULL DEFAULT 0,
	insight     TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL
);
-- P8: experience items (reflection loop — distilled insights from conversations).
CREATE TABLE IF NOT EXISTS experience_items (
	id           TEXT PRIMARY KEY,
	workspace_id TEXT NOT NULL DEFAULT 'default',
	kind         TEXT NOT NULL,           -- insight | lesson | strategy | error_pattern
	content      TEXT NOT NULL,           -- distilled experience text
	context      TEXT NOT NULL DEFAULT '',-- scenario that triggered this insight
	confidence   REAL NOT NULL DEFAULT 1.0,
	use_count    INTEGER NOT NULL DEFAULT 0,
	last_used    INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS collab_sessions (
	id                TEXT PRIMARY KEY,
	goal              TEXT NOT NULL DEFAULT '',
	orchestrator_id   TEXT NOT NULL DEFAULT '',
	blackboard_id     TEXT NOT NULL DEFAULT '',
	status            TEXT NOT NULL DEFAULT 'active',
	created_at        INTEGER NOT NULL,
	updated_at        INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS collab_members (
	session_id  TEXT NOT NULL,
	agent_id    TEXT NOT NULL,
	role        TEXT NOT NULL DEFAULT 'member',
	joined_at   INTEGER NOT NULL,
	PRIMARY KEY (session_id, agent_id)
);
CREATE TABLE IF NOT EXISTS bb_entries (
	board_id    TEXT NOT NULL,
	key         TEXT NOT NULL,
	value       TEXT NOT NULL DEFAULT '',
	author      TEXT NOT NULL DEFAULT '',
	kind        TEXT NOT NULL DEFAULT 'text',
	updated_at  INTEGER NOT NULL,
	PRIMARY KEY (board_id, key)
);
CREATE TABLE IF NOT EXISTS activity_log (
	id           TEXT PRIMARY KEY,
	ts           INTEGER NOT NULL,
	kind         TEXT NOT NULL,
	topic        TEXT NOT NULL DEFAULT '',
	source       TEXT NOT NULL DEFAULT '',
	source_name  TEXT NOT NULL DEFAULT '',
	session_id   TEXT NOT NULL DEFAULT '',
	summary      TEXT NOT NULL DEFAULT '',
	payload      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_activity_ts ON activity_log(ts);
CREATE INDEX IF NOT EXISTS idx_activity_kind ON activity_log(kind);
CREATE INDEX IF NOT EXISTS idx_activity_session ON activity_log(session_id);
	-- Bidirectional chunk registry: links chromem vector documents back to their
	-- source documents, enabling parent/child hierarchy and sibling traversal for
	-- hierarchical retrieval (NuVector / LlamaIndex AutoMergingRetriever pattern).
	CREATE TABLE IF NOT EXISTS chunk_registry (
		chunk_id     TEXT PRIMARY KEY,          -- chromem document ID (e.g. "pageA_3")
		source_type  TEXT NOT NULL,             -- "wiki" | "rag_kb" | "ingest"
		source_id    TEXT NOT NULL,             -- wiki page ID or KB ID
		chunk_index  INTEGER NOT NULL,          -- ordinal within source
		parent_id    TEXT,                      -- parent chunk ID (hierarchical)
		prev_id      TEXT,                      -- previous sibling chunk ID
		next_id      TEXT,                      -- next sibling chunk ID
		content_hash TEXT,                      -- SHA256 for dedup
		byte_start   INTEGER,                   -- byte offset in original document
		byte_end     INTEGER,
		chunk_type   TEXT NOT NULL DEFAULT 'leaf', -- "root" | "parent" | "leaf"
		created_at   INTEGER NOT NULL,
		UNIQUE(source_type, source_id, chunk_index)
	);
	CREATE INDEX IF NOT EXISTS idx_chunk_registry_source ON chunk_registry(source_type, source_id);
	CREATE INDEX IF NOT EXISTS idx_chunk_registry_parent ON chunk_registry(parent_id);
	-- Temporal property layer: assertions about entities with time constraints ("entity X had property Y during [A,B]").
	CREATE TABLE IF NOT EXISTS kg_entity_properties (
		id              TEXT PRIMARY KEY,
		entity_id       TEXT NOT NULL,
		property        TEXT NOT NULL,           -- e.g. "company", "position", "age", "location"
		value           TEXT NOT NULL,           -- JSON-compatible value
		value_type      TEXT NOT NULL DEFAULT 'string', -- "string"|"number"|"date_range"|"boolean"
		valid_from      INTEGER,                 -- Unix ms, NULL = unknown start
		valid_to        INTEGER,                 -- Unix ms, NULL = currently valid
		confidence      REAL NOT NULL DEFAULT 1.0,
		source_type     TEXT,                    -- "llm_extraction"|"user_input"|"inference"
		source_chunk_id TEXT,                    -- provenance trace
		evidence        TEXT,                    -- original text evidence snippet
		recorded_at     INTEGER NOT NULL,
		FOREIGN KEY (entity_id) REFERENCES kg_nodes(id)
	);
	CREATE INDEX IF NOT EXISTS idx_ep_entity ON kg_entity_properties(entity_id);
	CREATE INDEX IF NOT EXISTS idx_ep_time ON kg_entity_properties(entity_id, valid_from, valid_to);
	-- Spatial layer: geographic/spatial attributes for entities and events.
	CREATE TABLE IF NOT EXISTS kg_spatial (
		id              TEXT PRIMARY KEY,
		entity_id       TEXT,
		event_id        TEXT,
		spatial_type    TEXT NOT NULL,           -- "point"|"address"|"region"|"named_location"
		coordinates     TEXT,                    -- "POINT(lng lat)" or GeoJSON
		address         TEXT,
		region          TEXT,                    -- e.g. "北京市朝阳区"
		named_location  TEXT,                    -- e.g. "霍格沃茨大厅"
		valid_from      INTEGER,
		valid_to        INTEGER,
		confidence      REAL NOT NULL DEFAULT 1.0,
		source_chunk_id TEXT,
		recorded_at     INTEGER NOT NULL,
		CHECK (entity_id IS NOT NULL OR event_id IS NOT NULL)
	);
	CREATE INDEX IF NOT EXISTS idx_spatial_entity ON kg_spatial(entity_id);
	-- Event table: extracted plot/legal/historical events with temporal ordering.
	CREATE TABLE IF NOT EXISTS kg_events (
		id              TEXT PRIMARY KEY,
		title           TEXT NOT NULL,
		description     TEXT,
		event_type      TEXT,                    -- "legal_action"|"plot_event"|"historical_event"
		time_start      INTEGER,                 -- Unix ms
		time_end        INTEGER,                 -- Unix ms, NULL = instantaneous
		time_expression TEXT,                    -- original text: "三天后" / "2020年1月"
		timeline_order  INTEGER,                 -- logical ordinal within source text
		duration        TEXT,                    -- ISO 8601 duration: "P3D" / "PT2H"
		confidence      REAL NOT NULL DEFAULT 1.0,
		source_chunk_id TEXT,
		created_at      INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_events_time ON kg_events(time_start, time_end);
	CREATE INDEX IF NOT EXISTS idx_events_order ON kg_events(timeline_order);
	-- Concept taxonomy tree: IS_A / PART_OF / broader/narrower hierarchy for domain concepts.
	CREATE TABLE IF NOT EXISTS kg_concept_tree (
		id              TEXT PRIMARY KEY,
		concept         TEXT NOT NULL,
		parent_id       TEXT,
		tree_type       TEXT NOT NULL DEFAULT 'IS_A',
		level           INTEGER NOT NULL DEFAULT 0,
		definition      TEXT,
		domain          TEXT,
		synonyms        TEXT NOT NULL DEFAULT '[]',  -- JSON array (SKOS altLabels)
		source_chunk_id TEXT,
		created_at      INTEGER NOT NULL,
		FOREIGN KEY (parent_id) REFERENCES kg_concept_tree(id)
	);
	CREATE INDEX IF NOT EXISTS idx_concept_parent ON kg_concept_tree(parent_id);
	CREATE INDEX IF NOT EXISTS idx_concept_domain ON kg_concept_tree(domain);
`
	if _, err := s.db.Exec(ddl); err != nil {
		return err
	}
	// P7: workspace/library isolation + cross-library tags.
	for _, c := range []struct{ table, col, def string }{
		{"memory_items", "workspace_id", "TEXT NOT NULL DEFAULT 'default'"},
		{"user_facts", "workspace_id", "TEXT NOT NULL DEFAULT 'default'"},
		{"kg_nodes", "workspace_id", "TEXT NOT NULL DEFAULT 'default'"},
		{"kg_edges", "workspace_id", "TEXT NOT NULL DEFAULT 'default'"},
		{"memory_items", "cross_tags", "TEXT NOT NULL DEFAULT '[]'"},
		{"kg_edges", "cross_tags", "TEXT NOT NULL DEFAULT '[]'"},
		{"kg_edges", "weight", "INTEGER NOT NULL DEFAULT 1"},
	} {
		if err := s.addColumnIfMissing(c.table, c.col, c.def); err != nil {
			return err
		}
	}
	// One-time dedup: consolidate duplicate valid edges into weighted singles.
	if s.GetMeta("kg_edge_dedup_done") == "" {
		_ = s.dedupEdges()
		_ = s.SetMeta("kg_edge_dedup_done", "1")
	}
	// P9: usage tracking on domain libraries.
	if err := s.addColumnIfMissing("domain_libraries", "use_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// P10: domain-as-container — icon + sort_order for UI organization.
	for _, c := range []struct{ table, col, def string }{
		{"domain_libraries", "icon", "TEXT NOT NULL DEFAULT '📚'"},
		{"domain_libraries", "sort_order", "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := s.addColumnIfMissing(c.table, c.col, c.def); err != nil {
			return err
		}
	}
	// P5: add decay columns to memory_items for existing DBs (SQLite has no
	// ADD COLUMN IF NOT EXISTS, so guard via pragma_table_info).
	for _, c := range []struct{ col, def string }{
		{"last_access", "INTEGER NOT NULL DEFAULT 0"},
		{"access_count", "INTEGER NOT NULL DEFAULT 0"},
		{"importance", "TEXT NOT NULL DEFAULT 'normal'"},
		{"recall_count", "INTEGER NOT NULL DEFAULT 0"},
		{"query_diversity", "INTEGER NOT NULL DEFAULT 0"},
		{"cross_domain_hits", "INTEGER NOT NULL DEFAULT 0"},
		{"concept_tags", "TEXT NOT NULL DEFAULT '[]'"},
	} {
		if err := s.addColumnIfMissing("memory_items", c.col, c.def); err != nil {
			return err
		}
	}
	// Phase 3: KG property system — extend nodes and edges with rich attributes.
	for _, c := range []struct{ col, def string }{
		{"aliases", "TEXT NOT NULL DEFAULT ''"},
		{"description", "TEXT NOT NULL DEFAULT ''"},
		{"entity_uri", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing("kg_nodes", c.col, c.def); err != nil {
			return err
		}
	}
	for _, c := range []struct{ col, def string }{
		{"polarity", "TEXT NOT NULL DEFAULT 'neutral'"},
		{"intensity", "REAL NOT NULL DEFAULT 0.5"},
		{"level", "TEXT NOT NULL DEFAULT ''"},
		{"confidence", "REAL NOT NULL DEFAULT 1.0"},
		{"source_chunk_id", "TEXT NOT NULL DEFAULT ''"},
		{"evidence", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing("kg_edges", c.col, c.def); err != nil {
			return err
		}
	}
	return nil
}

// addColumnIfMissing adds a column to a table if it isn't already present.
func (s *Store) addColumnIfMissing(table, col, def string) error {
	var n int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, col).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, col, def)); err != nil {
			return err
		}
	}
	return nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB returns the underlying SQLite connection for subsystems that share the
// same database (e.g. async task manager).
func (s *Store) DB() *sql.DB { return s.db }

// ─── Sessions ─────────────────────────────────────────────────────

// CreateSession inserts a new session row.
func (s *Store) CreateSession(id, title, agentID string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO sessions(id, title, agent_id, summary, created_at, updated_at)
		VALUES(?, ?, ?, '', ?, ?)`, id, title, agentID, now, now)
	return err
}

// ListSessions returns all sessions, newest first.
func (s *Store) ListSessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT id, title, agent_id, summary, created_at, updated_at
		FROM sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var sess Session
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.AgentID, &sess.Summary, &sess.CreatedAt, &sess.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// GetSession returns one session by id.
func (s *Store) GetSession(id string) (*Session, error) {
	var sess Session
	err := s.db.QueryRow(`SELECT id, title, agent_id, summary, created_at, updated_at
		FROM sessions WHERE id = ?`, id).
		Scan(&sess.ID, &sess.Title, &sess.AgentID, &sess.Summary, &sess.CreatedAt, &sess.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// RenameSession updates a session's title.
func (s *Store) RenameSession(id, title string) error {
	_, err := s.db.Exec(`UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`,
		title, time.Now().UnixMilli(), id)
	return err
}

// UpdateSummary stores a rolled-up summary for a session (P0 unused; P1 hooks
// the summarizer here).
func (s *Store) UpdateSummary(id, summary string) error {
	_, err := s.db.Exec(`UPDATE sessions SET summary = ? WHERE id = ?`, summary, id)
	return err
}

// DeleteSession removes a session and all of its messages (cascade).
func (s *Store) DeleteSession(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM messages WHERE session_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// touchSession bumps updated_at; called inside the message-append transaction.
func touchSession(tx *sql.Tx, id string, now int64) error {
	_, err := tx.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, now, id)
	return err
}

// ─── Messages ─────────────────────────────────────────────────────

// AppendMessage inserts a message with an auto-incremented per-session seq and
// touches the session's updated_at. Returns the stored message.
func (s *Store) AppendMessage(sessionID, id, role, content, toolJSON string) (*Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	var seq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE session_id = ?`, sessionID).Scan(&seq); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	now := time.Now().UnixMilli()
	if _, err := tx.Exec(`INSERT INTO messages(id, session_id, seq, role, content, tool_json, created_at)
		VALUES(?, ?, ?, ?, ?, ?, ?)`, id, sessionID, seq, role, content, toolJSON, now); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := touchSession(tx, sessionID, now); err != nil {
		_ = tx.Rollback()
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &Message{
		ID:        id,
		SessionID: sessionID,
		Seq:       seq,
		Role:      role,
		Content:   content,
		ToolJSON:  toolJSON,
		CreatedAt: now,
	}, nil
}

// ListMessages returns a session's messages ordered by seq.
func (s *Store) ListMessages(sessionID string) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, session_id, seq, role, content, tool_json, created_at
		FROM messages WHERE session_id = ? ORDER BY seq ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content, &m.ToolJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMessagesRecent returns the last `limit` messages for a session in
// chronological order. Used by the frontend to load only the most recent
// messages on session switch rather than the entire history.
func (s *Store) ListMessagesRecent(sessionID string, limit int) ([]Message, error) {
	rows, err := s.db.Query(`SELECT id, session_id, seq, role, content, tool_json, created_at
		FROM messages WHERE session_id = ?
		ORDER BY seq DESC
		LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Seq, &m.Role, &m.Content, &m.ToolJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Reverse to chronological order (ASC).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// ClearMessages deletes all messages of a session but keeps the session row.
func (s *Store) ClearMessages(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	return err
}

// UpdateMessageToolJSON merges newToolJSON into the existing tool_json column
// rather than replacing it. This prevents data loss when multiple writes target
// the same message (e.g., first write has tool_calls+reasoning, second write adds
// tool_results — the merge preserves reasoning).
func (s *Store) UpdateMessageToolJSON(msgID, newToolJSON string) error {
	// Read existing.
	var existing string
	err := s.db.QueryRow(`SELECT tool_json FROM messages WHERE id = ?`, msgID).Scan(&existing)
	if err != nil {
		return err
	}
	merged := mergeJSON(existing, newToolJSON)
	_, err = s.db.Exec(`UPDATE messages SET tool_json = ? WHERE id = ?`, merged, msgID)
	return err
}

// mergeJSON shallow-merges newJSON into existing JSON string. New keys overwrite
// existing keys of the same name; existing keys not in newJSON are preserved.
// Both strings must be valid JSON objects (or empty).
func mergeJSON(existing, newJSON string) string {
	if existing == "" || existing == "null" {
		return newJSON
	}
	if newJSON == "" || newJSON == "null" {
		return existing
	}
	// Parse both as map[string]any, merge, re-serialize.
	var em, nm map[string]any
	if err := json.Unmarshal([]byte(existing), &em); err != nil {
		return newJSON // existing is corrupt — replace
	}
	if err := json.Unmarshal([]byte(newJSON), &nm); err != nil {
		return existing // new is corrupt — keep existing
	}
	for k, v := range nm {
		em[k] = v
	}
	out, err := json.Marshal(em)
	if err != nil {
		return existing
	}
	return string(out)
}

// CountMessages returns the message count of a session (for summary cadence).
func (s *Store) CountMessages(sessionID string) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE session_id = ?`, sessionID).Scan(&n)
	return n
}

// CountAllUserMessages returns total user messages across all sessions.
func (s *Store) CountAllUserMessages() int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE role = 'user'`).Scan(&n)
	return n
}

// ─── Meta (key-value) ─────────────────────────────────────────────

// SetMeta persists a key-value pair (used for the bound embedding model dir).
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// GetMeta reads a key; returns "" if absent.
func (s *Store) GetMeta(key string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM meta WHERE key = ?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// DeleteMeta removes a metadata key-value pair.
func (s *Store) DeleteMeta(key string) error {
	_, err := s.db.Exec(`DELETE FROM meta WHERE key = ?`, key)
	return err
}

// ListMeta returns all metadata keys.
func (s *Store) ListMeta() []string {
	rows, err := s.db.Query(`SELECT key FROM meta`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

// EmbeddingModelDir returns the bound embedding model directory ("" if unset).
func (s *Store) EmbeddingModelDir() string { return s.GetMeta("embeddingModelDir") }

// SetEmbeddingModel binds the embedding model directory used for semantic memory.
func (s *Store) SetEmbeddingModel(dir string) error { return s.SetMeta("embeddingModelDir", dir) }

// MemoryPolicy holds the hardware-adaptive retention knobs (computed by the app
// from host RAM/disk, stored in meta). Episodic recall + TTL sweep read these.
type MemoryPolicy struct {
	Tier         string  `json:"tier"`         // low | standard | high
	HalfLifeDays int     `json:"halfLifeDays"` // recency half-life
	TTLDays      int     `json:"ttlDays"`      // episodic items older than this are sweep candidates
	RecallK      int     `json:"recallK"`      // top-k after decay re-rank
	ItemCap      int     `json:"itemCap"`      // soft cap on memory_items
	CoreCap      int     `json:"coreCap"`      // soft cap on user_facts
	Alpha        float64 `json:"alpha"`        // semantic vs recency weight (0..1)
}

// DefaultMemoryPolicy is the standard tier (used until the app computes one).
func DefaultMemoryPolicy() MemoryPolicy {
	return MemoryPolicy{Tier: "standard", HalfLifeDays: 14, TTLDays: 90, RecallK: 3, ItemCap: 2000, CoreCap: 200, Alpha: 0.7}
}

// Policy returns the stored memory policy (or the default if unset/unparsable).
func (s *Store) Policy() MemoryPolicy {
	raw := s.GetMeta("memoryPolicy")
	if raw == "" {
		return DefaultMemoryPolicy()
	}
	var p MemoryPolicy
	if json.Unmarshal([]byte(raw), &p) == nil && p.Tier != "" {
		return p
	}
	return DefaultMemoryPolicy()
}

// SetPolicyJSON stores a serialized memory policy (called by the app after
// computing it from host hardware).
func (s *Store) SetPolicyJSON(raw string) error { return s.SetMeta("memoryPolicy", raw) }

// ─── Semantic memory (vector + manifest) ──────────────────────────

// TurnHit is a recalled user question with its associated assistant reply.
type TurnHit struct {
	Content    string  `json:"content"`
	Reply      string  `json:"reply"`
	ItemID     string  `json:"-"` // join key to memory_items (not serialized)
	Similarity float32 `json:"similarity"`
	Score      float32 `json:"score"` // decay-adjusted score (set by re-rank)
}

// FactHit is a recalled extracted fact.
type FactHit struct {
	Content    string  `json:"content"`
	Category   string  `json:"category"`
	ItemID     string  `json:"-"`
	Similarity float32 `json:"similarity"`
	Score      float32 `json:"score"`
}

// EntityHit is a recalled graph entity — the vector seed for graph expansion.
type EntityHit struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Type       string  `json:"type"`
	Similarity float32 `json:"similarity"`
}

// MemoryItem is a manifest row for UI listing / counts / clear.
type MemoryItem struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"` // turn | fact
	Content     string `json:"content"`
	Reply       string `json:"reply"`
	Category    string `json:"category"`
	SessionID   string `json:"sessionId"`
	CreatedAt   int64  `json:"createdAt"`
	WorkspaceID string `json:"workspaceId,omitempty"`
}

// HasVector reports whether the vector layer is available.
func (s *Store) HasVector() bool { return s.vector != nil }

// AddTurnMemory writes a user-question turn to both the SQLite manifest and the
// chromem collection (question vectorized; reply carried in metadata).
// libraryID is stored as workspace_id for per-domain isolation.
func (s *Store) AddTurnMemory(itemID, userText, reply, sessionID, libraryID string, userEmb []float32) error {
	now := time.Now().UnixMilli()
	if libraryID == "" {
		libraryID = s.realDefaultLibrary()
	}
	if _, err := s.db.Exec(`INSERT INTO memory_items(id, kind, content, reply, category, session_id, created_at, workspace_id)
		VALUES(?, 'turn', ?, ?, '', ?, ?, ?)`, itemID, userText, reply, sessionID, now, libraryID); err != nil {
		return err
	}
	if s.vector != nil {
		if err := s.vector.AddTurn(itemID, userText, reply, sessionID, itemID, libraryID, userEmb); err != nil {
			return err
		}
	}
	return nil
}

// factDedupThreshold is the cosine similarity at which two facts are treated as
// the same fact (a paraphrase). Short factual sentences cluster tightly with a
// sentence-embedding model, so 0.90 catches "用户喜欢 Go" vs "用户喜爱的语言是 Go".
const factDedupThreshold = 0.90

// AddFactMemory writes an extracted fact to both stores. importance tags the
// row for decay-rate adjustment (low → forgets faster); high-importance facts
// should go to user_facts instead (handled by the caller).
func (s *Store) AddFactMemory(itemID, content, category, importance, libraryID, crossTags string, emb []float32) error {
	if importance == "" {
		importance = "normal"
	}
	if libraryID == "" {
		libraryID = s.realDefaultLibrary()
	}
	// Dedup (mirrors AddUserFact): the extractor re-runs every N turns and tends
	// to emit the same fact 3-5× with minor wording changes. Skip exact and
	// semantic duplicates so each distinct fact is stored once.
	var existing int
	s.db.QueryRow(`SELECT COUNT(*) FROM memory_items WHERE workspace_id = ? AND kind = 'fact' AND content = ?`,
		libraryID, content).Scan(&existing)
	if existing > 0 {
		return nil // exact duplicate — no-op
	}
	if s.vector != nil && len(emb) > 0 {
		if hits, _ := s.vector.QueryFacts(emb, 1, libraryID); len(hits) > 0 && hits[0].Similarity >= factDedupThreshold {
			return nil // semantic duplicate (paraphrase) — no-op
		}
	}
	now := time.Now().UnixMilli()
	if _, err := s.db.Exec(`INSERT INTO memory_items(id, kind, content, reply, category, session_id, created_at, importance, workspace_id, cross_tags)
		VALUES(?, 'fact', ?, '', ?, '', ?, ?, ?, ?)`, itemID, content, category, now, importance, libraryID, crossTags); err != nil {
		return err
	}
	if s.vector != nil {
		if err := s.vector.AddFact(itemID, content, category, itemID, libraryID, emb); err != nil {
			return err
		}
	}
	return nil
}


// DedupAllFacts runs a one-time pass that deduplicates historical fact memory
// items by exact content match within the same library. Called at startup.
// Returns the number of duplicates removed.
func (s *Store) DedupAllFacts() error {
	if s.GetMeta("fact_consolidation_done") != "" {
		return nil
	}
	rows, err := s.db.Query(`SELECT id, content, workspace_id FROM memory_items WHERE kind='fact' ORDER BY created_at ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type fact struct {
		id, content, lib string
	}
	var facts []fact
	for rows.Next() {
		var f fact
		if err := rows.Scan(&f.id, &f.content, &f.lib); err != nil {
			return err
		}
		facts = append(facts, f)
	}
	seen := map[string]string{} // key: libID+"|"+content → id (keep first)
	removed := 0
	for _, f := range facts {
		key := f.lib + "|" + f.content
		if keepID, exists := seen[key]; exists {
			// Delete duplicate, keep the first one.
			s.db.Exec(`DELETE FROM memory_items WHERE id = ?`, f.id)
			if s.vector != nil {
				_ = s.vector.Delete(f.id)
			}
			// Update the kept one's access count to reflect consolidation.
			s.db.Exec(`UPDATE memory_items SET access_count = access_count + 1 WHERE id = ?`, keepID)
			removed++
		} else {
			seen[key] = f.id
		}
	}
	if removed > 0 {
		log.Printf("[memory] 历史事实去重: 移除 %d 条重复记录", removed)
	}
	_ = s.SetMeta("fact_consolidation_done", "1")
	return nil
}

// DedupUserFacts consolidates duplicate user_facts rows by key, keeping the
// most recent entry (by last_access then created_at) and merging access counts.
// Called at startup; safe to run multiple times.
func (s *Store) DedupUserFacts() (removed int, _ error) {
	rows, err := s.db.Query(`SELECT id, key, value, category, importance, locked, source, created_at, last_access, access_count FROM user_facts ORDER BY key, created_at DESC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type uf struct {
		id                        string
		key, value, category, imp string
		locked                    int
		source                    string
		createdAt, lastAccess     int64
		accessCount               int
	}
	var facts []uf
	for rows.Next() {
		var f uf
		if err := rows.Scan(&f.id, &f.key, &f.value, &f.category, &f.imp, &f.locked, &f.source, &f.createdAt, &f.lastAccess, &f.accessCount); err != nil {
			return 0, err
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// Group by key (case-insensitive + trimmed)
	seen := map[string]*uf{} // normalized_key → best entry
	for i := range facts {
		f := &facts[i]
		norm := strings.ToLower(strings.TrimSpace(f.key))
		if best, exists := seen[norm]; exists {
			// Merge: keep the entry with highest access_count; sum access counts
			if f.accessCount > best.accessCount || (f.accessCount == best.accessCount && f.lastAccess > best.lastAccess) {
				// Promote this one as best; merge access count from old best
				f.accessCount += best.accessCount
				f.lastAccess = max(f.lastAccess, best.lastAccess)
				s.db.Exec(`DELETE FROM user_facts WHERE id = ?`, best.id)
				removed++
				seen[norm] = f
			} else {
				best.accessCount += f.accessCount
				best.lastAccess = max(best.lastAccess, f.lastAccess)
				s.db.Exec(`UPDATE user_facts SET access_count = ?, last_access = ? WHERE id = ?`, best.accessCount, best.lastAccess, best.id)
				s.db.Exec(`DELETE FROM user_facts WHERE id = ?`, f.id)
				removed++
			}
		} else {
			seen[norm] = f
		}
	}
	if removed > 0 {
		log.Printf("[memory] 核心记忆去重: 移除 %d 条重复记录 (key 维度)", removed)
	}
	return removed, nil
}

// DeleteMemoryItem removes one item from memory_items by ID and deletes its
// orphan vector (prevents stale chromem docs that outlive their SQLite row).
func (s *Store) DeleteMemoryItem(id string) error {
	_, err := s.db.Exec(`DELETE FROM memory_items WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if s.vector != nil {
		_ = s.vector.Delete(id) // best-effort orphan cleanup
	}
	return nil
}
// UserFact is a permanent core-memory row (identity/preference/constraint) —
// never decayed or TTL'd.
type UserFact struct {
	ID         string `json:"id"`
	Key        string `json:"key"`
	Value      string `json:"value"`
	Category   string `json:"category"`
	Importance string `json:"importance"`
	Locked     bool   `json:"locked"`
	Source     string `json:"source"`
	CreatedAt  int64  `json:"createdAt"`
}

// AddUserFact inserts a core-memory row scoped to a domain library.
// workspaceID empty → real default library (not the "default" sentinel).
func (s *Store) AddUserFact(id, key, value, category, importance, source, workspaceID string) error {
	if importance == "" {
		importance = "high"
	}
	if workspaceID == "" {
		workspaceID = s.realDefaultLibrary()
	}
	// Dedup: skip if an identical key+value already exists in this workspace.
	// Stops the LLM from re-extracting "用户名为 wky" 5 times into 5 rows.
	var existing int
	s.db.QueryRow(`SELECT COUNT(*) FROM user_facts WHERE workspace_id = ? AND key = ? AND value = ?`, workspaceID, key, value).Scan(&existing)
	if existing > 0 {
		return nil // duplicate of an existing core fact — no-op
	}
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO user_facts(id, key, value, category, importance, locked, source, created_at, last_access, access_count, workspace_id)
		VALUES(?, ?, ?, ?, ?, 0, ?, ?, ?, 0, ?)`, id, key, value, category, importance, source, now, now, workspaceID)
	return err
}

// ListUserFacts returns core-memory rows (newest first).
// workspaceID ""   → global facts only (legacy sentinels + real default library).
// workspaceID "*"  → all facts (no filter, for admin views).
// workspaceID "xxx"→ domain-scoped facts only.
func (s *Store) ListUserFacts(workspaceID string) ([]UserFact, error) {
	var rows *sql.Rows
	var err error
	switch {
	case workspaceID == "":
		// Global facts: legacy sentinels + facts stored under the real default
		// library UUID (inserted by AddUserFact with resolved workspaceID).
		rows, err = s.db.Query(`SELECT id, key, value, category, importance, locked, source, created_at FROM user_facts WHERE workspace_id = '' OR workspace_id = 'default' OR workspace_id IS NULL OR workspace_id = ? ORDER BY created_at DESC`, s.realDefaultLibrary())
	case workspaceID == "*":
		// Admin view: all facts regardless of domain.
		rows, err = s.db.Query(`SELECT id, key, value, category, importance, locked, source, created_at FROM user_facts ORDER BY created_at DESC`)
	default:
		// Domain-scoped: only this domain's facts.
		rows, err = s.db.Query(`SELECT id, key, value, category, importance, locked, source, created_at FROM user_facts WHERE workspace_id = ? ORDER BY created_at DESC`, workspaceID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserFact
	for rows.Next() {
		var f UserFact
		var locked int
		if err := rows.Scan(&f.ID, &f.Key, &f.Value, &f.Category, &f.Importance, &locked, &f.Source, &f.CreatedAt); err != nil {
			return nil, err
		}
		f.Locked = locked != 0
		out = append(out, f)
	}
	return out, rows.Err()
}

// LockUserFact sets/clears the locked flag (locked rows are never modified by sweeps).
func (s *Store) LockUserFact(id string, locked bool) error {
	v := 0
	if locked {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE user_facts SET locked = ? WHERE id = ?`, v, id)
	return err
}

// ─── Workspaces (P7) ──────────────────────────────────────────────

// DefaultWorkspace returns the first workspace, creating a "核心领域" (core domain) one if none exist.
func (s *Store) DefaultWorkspace() (string, error) {
	var id string
	if err := s.db.QueryRow(`SELECT id FROM everevo_workspaces LIMIT 1`).Scan(&id); err == nil && id != "" {
		return id, nil
	}
	id = fmt.Sprintf("ws_%x", time.Now().UnixNano())
	name := "核心领域"
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO everevo_workspaces(id, name, created_at) VALUES(?, ?, ?)`, id, name, now)
	return id, err
}

// WorkspaceList returns all workspaces.
func (s *Store) WorkspaceList() ([]struct {
	ID        string
	Name      string
	CreatedAt int64
}, error) {
	rows, err := s.db.Query(`SELECT id, name, created_at FROM everevo_workspaces ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		ID        string
		Name      string
		CreatedAt int64
	}
	for rows.Next() {
		var ws struct {
			ID        string
			Name      string
			CreatedAt int64
		}
		if err := rows.Scan(&ws.ID, &ws.Name, &ws.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ws)
	}
	return out, rows.Err()
}

// WorkspaceCreate adds a workspace.
func (s *Store) WorkspaceCreate(name string) (string, error) {
	id := fmt.Sprintf("ws_%x", time.Now().UnixNano())
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO everevo_workspaces(id, name, created_at) VALUES(?, ?, ?)`, id, name, now)
	return id, err
}

// WorkspaceDelete removes a workspace row. The caller is responsible for cascade-
// deleting or reassigning the workspace's data (app layer).
func (s *Store) WorkspaceDelete(id string) error {
	_, err := s.db.Exec(`DELETE FROM everevo_workspaces WHERE id = ?`, id)
	return err
}

// ─── Domain Libraries (P7) — AI-managed knowledge domains ──────

// DefaultLibrary returns the first library, creating a "核心领域" (core domain) one if none exist.
func (s *Store) DefaultLibrary() (string, error) {
	var id string
	if err := s.db.QueryRow(`SELECT id FROM domain_libraries LIMIT 1`).Scan(&id); err == nil && id != "" {
		return id, nil
	}
	id = fmt.Sprintf("lib_%x", time.Now().UnixNano())
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO domain_libraries(id, name, description, auto_created, created_at) VALUES(?, '核心领域', '', 0, ?)`, id, now)
	return id, err
}

// realDefaultLibrary returns the real DefaultLibrary ID (never the literal
// "default"). Used to avoid scattering the sentinel "default" string across
// memory_items, which breaks per-library queries. Falls back to "default"
// only if the DB itself is unavailable.
func (s *Store) realDefaultLibrary() string {
	id, err := s.DefaultLibrary()
	if err != nil || id == "" {
		return "default"
	}
	return id
}

// LastTurnLibrary returns the workspace_id of the most recent turn memory item.
// Used by the extraction scheduler so extracted facts land in the SAME library
// as the conversation that produced them (not always the core library).
func (s *Store) LastTurnLibrary() string {
	var ws string
	if err := s.db.QueryRow(`SELECT workspace_id FROM memory_items WHERE kind='turn' ORDER BY created_at DESC LIMIT 1`).Scan(&ws); err == nil && ws != "" {
		return ws
	}
	return s.realDefaultLibrary()
}

// ListLibraryIDs returns all valid domain library IDs. Used by other subsystems
// to validate and fix dangling LibraryID references at startup.
func (s *Store) ListLibraryIDs() []string {
	rows, err := s.db.Query(`SELECT id FROM domain_libraries`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// IsValidLibrary returns true if the given library ID exists in domain_libraries.
func (s *Store) IsValidLibrary(id string) bool {
	var ok int
	err := s.db.QueryRow(`SELECT 1 FROM domain_libraries WHERE id = ?`, id).Scan(&ok)
	return err == nil && ok == 1
}

// LibraryList returns all domain libraries.
func (s *Store) LibraryList() ([]struct {
	ID          string
	Name        string
	Description string
	Icon        string
	Tags        string
	AutoCreated bool
	UseCount    int
	SortOrder   int
	CreatedAt   int64
}, error) {
	rows, err := s.db.Query(`SELECT id, name, description, COALESCE(icon,'📚'), tags, auto_created, COALESCE(use_count,0), COALESCE(sort_order,0), created_at FROM domain_libraries ORDER BY sort_order ASC, use_count DESC, auto_created ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		ID          string
		Name        string
		Description string
		Icon        string
		Tags        string
		AutoCreated bool
		UseCount    int
		SortOrder   int
		CreatedAt   int64
	}
	for rows.Next() {
		var lib struct {
			ID          string
			Name        string
			Description string
			Icon        string
			Tags        string
			AutoCreated bool
			UseCount    int
			SortOrder   int
			CreatedAt   int64
		}
		var ac int
		if err := rows.Scan(&lib.ID, &lib.Name, &lib.Description, &lib.Icon, &lib.Tags, &ac, &lib.UseCount, &lib.SortOrder, &lib.CreatedAt); err != nil {
			return nil, err
		}
		lib.AutoCreated = ac != 0
		out = append(out, lib)
	}
	return out, rows.Err()
}

// LibraryCreate adds a domain library and returns its id.
func (s *Store) LibraryCreate(name, description, icon string, autoCreated bool) (string, error) {
	id := fmt.Sprintf("lib_%x", time.Now().UnixNano())
	ac := 0
	if autoCreated {
		ac = 1
	}
	if icon == "" {
		icon = "📚"
	}
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`INSERT INTO domain_libraries(id, name, description, icon, auto_created, created_at) VALUES(?, ?, ?, ?, ?, ?)`,
		id, name, description, icon, ac, now)
	return id, err
}

// LibraryUpdate updates a domain library's mutable fields.
func (s *Store) LibraryUpdate(id, name, description, icon string) error {
	_, err := s.db.Exec(`UPDATE domain_libraries SET name=?, description=?, icon=? WHERE id=?`,
		name, description, icon, id)
	return err
}

// LibraryDelete removes a library row. The caller should cascade data first.
func (s *Store) LibraryDelete(id string) error {
	_, err := s.db.Exec(`DELETE FROM domain_libraries WHERE id = ?`, id)
	return err
}

// LibraryMerge re-points all knowledge from dropID to keepID and deletes dropID.

// BumpLibraryUse increments the usage counter for a domain library.
func (s *Store) BumpLibraryUse(id string) {
	s.db.Exec(`UPDATE domain_libraries SET use_count = COALESCE(use_count,0) + 1 WHERE id = ?`, id)
}
func (s *Store) LibraryMerge(keepID, dropID string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, table := range []string{"memory_items", "user_facts", "kg_nodes", "kg_edges"} {
		if _, err := tx.Exec(fmt.Sprintf(`UPDATE %s SET workspace_id = ? WHERE workspace_id = ?`, table), keepID, dropID); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM domain_libraries WHERE id = ?`, dropID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// PruneEmptyAutoLibraries deletes domain libraries that have no associated data
// (no memory items, user facts, or KG nodes/edges) and are either auto-created
// or have never been used (useCount=0, excluding the default/first domain).
// Returns the number of libraries removed. Safe to call at startup.
func (s *Store) PruneEmptyAutoLibraries() (removed int) {
	// Get the default (first) library ID — never prune it.
	defaultID, _ := s.DefaultLibrary()
	// Find candidates: auto_created + zero use, OR manual + zero use + not default.
	rows, err := s.db.Query(`SELECT id, auto_created FROM domain_libraries WHERE (auto_created = 1 OR COALESCE(use_count,0) = 0)`)
	if err != nil {
		return 0
	}
	defer rows.Close()
	type cand struct{ id string; auto bool }
	var candidates []cand
	for rows.Next() {
		var c cand
		var ac int
		if err := rows.Scan(&c.id, &ac); err == nil {
			c.auto = ac != 0
			candidates = append(candidates, c)
		}
	}
	for _, c := range candidates {
		// Never prune the default library.
		if c.id == defaultID {
			continue
		}
		// Only prune manual domains if useCount is 0.
		if !c.auto {
			var uc int
			s.db.QueryRow(`SELECT COALESCE(use_count,0) FROM domain_libraries WHERE id = ?`, c.id).Scan(&uc)
			if uc > 0 {
				continue
			}
		}
		// Check each data table — if any has rows, skip.
		hasData := false
		for _, table := range []string{"memory_items", "user_facts", "kg_nodes", "kg_edges", "experience_items"} {
			var n int
			if err := s.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE workspace_id = ?", table), c.id).Scan(&n); err == nil && n > 0 {
				hasData = true
				break
			}
		}
		if hasData {
			continue
		}
		// No data in any table — safe to delete.
		if _, err := s.db.Exec(`DELETE FROM domain_libraries WHERE id = ?`, c.id); err == nil {
			removed++
			log.Printf("[memory] 清理未使用领域: %s", c.id)
		}
	}
	if removed > 0 {
		log.Printf("[memory] 清理 %d 个未使用领域", removed)
	}
	return removed
}

// ─── Experience Items (P8) — reflection distilled insights ─────

// AddExperience stores a distilled insight from the reflection loop.
func (s *Store) AddExperience(id, workspaceID, kind, content, context string, confidence float64, now int64) error {
	_, err := s.db.Exec(
		`INSERT INTO experience_items(id, workspace_id, kind, content, context, confidence, use_count, last_used, created_at)
		 VALUES(?,?,?,?,?,?,0,0,?)`,
		id, workspaceID, kind, content, context, confidence, now)
	return err
}

// DeleteExperience removes a single experience item by ID.
func (s *Store) DeleteExperience(id string) error {
	_, err := s.db.Exec(`DELETE FROM experience_items WHERE id = ?`, id)
	return err
}

// ListExperience returns recent experience items, optionally filtered by workspace.
func (s *Store) ListExperience(workspaceID string, limit int) ([]ExperienceItem, error) {
	if limit <= 0 { limit = 20 }
	var rows *sql.Rows; var err error
	if workspaceID == "" {
		rows, err = s.db.Query(`SELECT id,workspace_id,kind,content,context,confidence,use_count,last_used,created_at FROM experience_items ORDER BY confidence DESC LIMIT ?`, limit)
	} else {
		rows, err = s.db.Query(`SELECT id,workspace_id,kind,content,context,confidence,use_count,last_used,created_at FROM experience_items WHERE workspace_id=? ORDER BY confidence DESC LIMIT ?`, workspaceID, limit)
	}
	if err != nil { return nil, err }
	defer rows.Close()
	var out []ExperienceItem
	for rows.Next() {
		var e ExperienceItem
		if err := rows.Scan(&e.ID, &e.WorkspaceID, &e.Kind, &e.Content, &e.Context, &e.Confidence, &e.UseCount, &e.LastUsed, &e.CreatedAt); err != nil { return nil, err }
		out = append(out, e)
	}
	return out, rows.Err()
}

// ─── Activity Log — unified AI-work timeline (collab observability) ────

// ActivityRow is one recorded AI-work event (agent run / message / tool call /
// workflow execution / plan / blackboard change). The unified timeline backs
// both the live workbench and the history/replay view.
type ActivityRow struct {
	ID         string `json:"id"`
	Ts         int64  `json:"ts"`
	Kind       string `json:"kind"`       // agent_start | agent_done | agent_message | tool_call | workflow_start | workflow_node | workflow_done | session | plan | blackboard
	Topic      string `json:"topic"`      // raw event topic
	Source     string `json:"source"`     // agentId / execId
	SourceName string `json:"sourceName"` // resolved display name (agent name / workflow name)
	SessionID  string `json:"sessionId"`  // collab session or ""
	Summary    string `json:"summary"`    // one-line human description
	Payload    string `json:"payload"`    // original Event JSON, for replay/detail
}

// ActivityFilter parameterizes ListActivity. Zero-value fields are ignored.
type ActivityFilter struct {
	Kind      string `json:"kind,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	Source    string `json:"source,omitempty"`
	Since     int64  `json:"since,omitempty"`  // ts >=
	Before    int64  `json:"before,omitempty"` // ts < (cursor for paging)
	Limit     int    `json:"limit,omitempty"`
}

// activitySeq guarantees unique log IDs even when many events share the same
// millisecond (avoids the PRIMARY KEY collision that a pure-timestamp ID hits in
// tight bursts).
var activitySeq uint64

// LogActivity appends one AI-work event to the unified timeline. ID/Ts are
// filled if zero. Best-effort: the app layer queues writes off the event bus.
func (s *Store) LogActivity(r ActivityRow) error {
	if r.ID == "" {
		r.ID = fmt.Sprintf("act_%d_%d", r.Ts, atomic.AddUint64(&activitySeq, 1))
	}
	if r.Ts == 0 {
		r.Ts = time.Now().UnixMilli()
		r.ID = fmt.Sprintf("act_%d_%d", r.Ts, atomic.AddUint64(&activitySeq, 1))
	}
	_, err := s.db.Exec(`INSERT INTO activity_log(id, ts, kind, topic, source, source_name, session_id, summary, payload)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Ts, r.Kind, r.Topic, r.Source, r.SourceName, r.SessionID, r.Summary, r.Payload)
	return err
}

// ListActivity returns timeline rows newest-first, optionally filtered.
func (s *Store) ListActivity(f ActivityFilter) ([]ActivityRow, error) {
	if f.Limit <= 0 {
		f.Limit = 200
	}
	q := `SELECT id, ts, kind, topic, source, source_name, session_id, summary, payload FROM activity_log WHERE 1=1`
	args := []any{}
	if f.Kind != "" {
		q += " AND kind=?"
		args = append(args, f.Kind)
	}
	if f.SessionID != "" {
		q += " AND session_id=?"
		args = append(args, f.SessionID)
	}
	if f.Source != "" {
		q += " AND source=?"
		args = append(args, f.Source)
	}
	if f.Since > 0 {
		q += " AND ts>=?"
		args = append(args, f.Since)
	}
	if f.Before > 0 {
		q += " AND ts<?"
		args = append(args, f.Before)
	}
	q += " ORDER BY ts DESC LIMIT ?"
	args = append(args, f.Limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ActivityRow, 0) // empty not nil → JSON [] not null
	for rows.Next() {
		var r ActivityRow
		if err := rows.Scan(&r.ID, &r.Ts, &r.Kind, &r.Topic, &r.Source, &r.SourceName, &r.SessionID, &r.Summary, &r.Payload); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// BumpExperience increments the use count and last_used timestamp.
func (s *Store) BumpExperience(id string, now int64) error {
	_, err := s.db.Exec(`UPDATE experience_items SET use_count=use_count+1, last_used=? WHERE id=?`, now, id)
	return err
}

// ExperienceItem is one distilled insight from the reflection loop.
type ExperienceItem struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspaceId"`
	Kind        string  `json:"kind"`
	Content     string  `json:"content"`
	Context     string  `json:"context"`
	Confidence  float64 `json:"confidence"`
	UseCount    int     `json:"useCount"`
	LastUsed    int64   `json:"lastUsed"`
	CreatedAt   int64   `json:"createdAt"`
}

// ─── Entity Links (P8) — cross-domain semantic anchors ──────────

// LinkEntitiesAcrossLibraries finds entities with matching names across two
// libraries and creates entity_links for them. Returns the number of links created.
func (s *Store) LinkEntitiesAcrossLibraries(libA, libB string) (int, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.name, a.type, b.id, b.name, b.type
		FROM kg_nodes a
		JOIN kg_nodes b ON LOWER(a.name) = LOWER(b.name) AND a.id != b.id
		WHERE a.workspace_id = ? AND b.workspace_id = ?
		AND NOT EXISTS (
			SELECT 1 FROM entity_links el
			WHERE (el.src_node_id = a.id AND el.dst_node_id = b.id)
			   OR (el.src_node_id = b.id AND el.dst_node_id = a.id)
		)`, libA, libB)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	now := time.Now().UnixMilli()
	count := 0
	for rows.Next() {
		var srcID, srcName, srcType, dstID, dstName, dstType string
		if err := rows.Scan(&srcID, &srcName, &srcType, &dstID, &dstName, &dstType); err != nil {
			continue
		}
		id := fmt.Sprintf("el_%x", now+int64(count))
		linkType := "sameAs"
		if srcType != dstType {
			linkType = "relatedTo"
		}
		_, err := s.db.Exec(
			`INSERT INTO entity_links(id, src_node_id, dst_node_id, link_type, confidence, source, created_at)
			 VALUES(?,?,?,?,?,?,?)`,
			id, srcID, dstID, linkType, 0.85, "auto", now)
		if err == nil {
			count++
		}
	}
	return count, rows.Err()
}

// ListEntityLinks returns all cross-domain entity links for visualization.
func (s *Store) ListEntityLinks() ([]EntityLink, error) {
	rows, err := s.db.Query(`SELECT el.id, el.src_node_id, el.dst_node_id, el.link_type, el.confidence,
		sn.name, sn.workspace_id, dn.name, dn.workspace_id
		FROM entity_links el
		JOIN kg_nodes sn ON sn.id = el.src_node_id
		JOIN kg_nodes dn ON dn.id = el.dst_node_id
		ORDER BY el.confidence DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityLink
	for rows.Next() {
		var el EntityLink
		if err := rows.Scan(&el.ID, &el.SrcNodeID, &el.DstNodeID, &el.LinkType, &el.Confidence,
			&el.SrcName, &el.SrcLibrary, &el.DstName, &el.DstLibrary); err != nil {
			continue
		}
		out = append(out, el)
	}
	return out, rows.Err()
}

type EntityLink struct {
	ID         string  `json:"id"`
	SrcNodeID  string  `json:"srcNodeId"`
	DstNodeID  string  `json:"dstNodeId"`
	LinkType   string  `json:"linkType"`
	Confidence float64 `json:"confidence"`
	SrcName    string  `json:"srcName"`
	SrcLibrary string  `json:"srcLibrary"`
	DstName    string  `json:"dstName"`
	DstLibrary string  `json:"dstLibrary"`
}

// ─── Conflict Detection (P8) ──────────────────────────────────────

// DetectConflicts finds entity_links where the linked entities have contradictory
// knowledge (edges with opposite semantics or conflicting facts).
func (s *Store) DetectConflicts() ([]Conflict, error) {
	rows, err := s.db.Query(`
		SELECT el.id, el.src_node_id, el.dst_node_id, el.link_type,
			sn.name, sn.workspace_id, dn.name, dn.workspace_id
		FROM entity_links el
		JOIN kg_nodes sn ON sn.id = el.src_node_id
		JOIN kg_nodes dn ON dn.id = el.dst_node_id
		WHERE el.confidence < 0.6`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Conflict
	for rows.Next() {
		var c Conflict
		if err := rows.Scan(&c.LinkID, &c.SrcNodeID, &c.DstNodeID, &c.LinkType,
			&c.SrcName, &c.SrcLib, &c.DstName, &c.DstLib); err != nil {
			continue
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type Conflict struct {
	LinkID    string `json:"linkId"`
	SrcNodeID string `json:"srcNodeId"`
	DstNodeID string `json:"dstNodeId"`
	LinkType  string `json:"linkType"`
	SrcName   string `json:"srcName"`
	SrcLib    string `json:"srcLib"`
	DstName   string `json:"dstName"`
	DstLib    string `json:"dstLib"`
}

// ─── Evolution Metrics (P8) ───────────────────────────────────────

// RecordMetrics upserts a daily metrics row for a domain.
func (s *Store) RecordMetrics(domainID, date string, turns, reflected, recalls, links int) error {
	_, err := s.db.Exec(`INSERT INTO evolution_metrics(domain_id, date, total_turns, reflected_turns, experience_recalls, cross_domain_links)
		VALUES(?,?,?,?,?,?) ON CONFLICT(domain_id, date) DO UPDATE SET
		total_turns=total_turns+?, reflected_turns=reflected_turns+?,
		experience_recalls=experience_recalls+?, cross_domain_links=cross_domain_links+?`,
		domainID, date, turns, reflected, recalls, links,
		turns, reflected, recalls, links)
	return err
}

// DeleteUserFact removes a core-memory row.
func (s *Store) DeleteUserFact(id string) error {
	_, err := s.db.Exec(`DELETE FROM user_facts WHERE id = ?`, id)
	return err
}

// QueryMemory runs a two-pass recall: kind=turn (question↔question) and
// kind=fact, each top-k, scoped to libraryID. Embedding is computed once by
// the caller. libraryID "" → global (all libraries).
func (s *Store) QueryMemory(emb []float32, k int, libraryID string) (turns []TurnHit, facts []FactHit, err error) {
	if s.vector == nil {
		return nil, nil, nil
	}
	turns, err = s.vector.QueryTurns(emb, k, libraryID)
	if err != nil {
		return nil, nil, err
	}
	facts, err = s.vector.QueryFacts(emb, k, libraryID)
	if err != nil {
		return nil, nil, err
	}
	// P5: recency-decay re-rank + access warmth refresh.
	p := s.Policy()
	rk := p.RecallK
	if rk <= 0 {
		rk = k
	}
	now := time.Now().UnixMilli()
	turns = s.decayRankTurns(turns, p, now, rk)
	facts = s.decayRankFacts(facts, p, now, rk)
	return turns, facts, nil
}

// memMeta carries the recency/importance columns used for decay re-rank.
type memMeta struct {
	lastAccess int64
	createdAt  int64
	importance string
}

func toAny(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// memoryMeta fetches last_access/created_at/importance for the given item ids.
func (s *Store) memoryMeta(ids []string) map[string]memMeta {
	out := map[string]memMeta{}
	if len(ids) == 0 {
		return out
	}
	rows, err := s.db.Query(`SELECT id, last_access, created_at, importance FROM memory_items WHERE id IN (`+placeholders(len(ids))+`)`, toAny(ids)...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var m memMeta
		if err := rows.Scan(&id, &m.lastAccess, &m.createdAt, &m.importance); err == nil {
			out[id] = m
		}
	}
	return out
}

// decayScore = α·cos + (1-α)·0.5^(ageDays/halfLife); low-importance ages 2× faster.
func decayScore(sim float32, lastAccess, createdAt int64, importance string, p MemoryPolicy, now int64) float32 {
	ref := lastAccess
	if ref == 0 {
		ref = createdAt
	}
	ageDays := float64(now-ref) / 86400000
	if importance == "low" {
		ageDays *= 2
	}
	hl := p.HalfLifeDays
	if hl <= 0 {
		hl = 14
	}
	recency := math.Pow(0.5, ageDays/float64(hl))
	cos := sim
	if cos < 0 {
		cos = 0
	}
	alpha := p.Alpha
	if alpha <= 0 {
		alpha = 0.7
	}
	return float32(alpha)*cos + float32(1-alpha)*float32(recency)
}

// decayRankTurns scores + sorts + caps turn hits, then refreshes access warmth.
func (s *Store) decayRankTurns(hits []TurnHit, p MemoryPolicy, now int64, k int) []TurnHit {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ItemID)
	}
	meta := s.memoryMeta(ids)
	// drop orphans (chromem docs whose SQLite row was TTL-deleted)
	kept := hits[:0]
	for _, h := range hits {
		if _, ok := meta[h.ItemID]; ok {
			kept = append(kept, h)
		}
	}
	hits = kept
	for i := range hits {
		m := meta[hits[i].ItemID]
		hits[i].Score = decayScore(hits[i].Similarity, m.lastAccess, m.createdAt, m.importance, p, now)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	keep := make([]string, 0, len(hits))
	for _, h := range hits {
		keep = append(keep, h.ItemID)
	}
	s.bumpAccess(keep, now)
	return hits
}

// decayRankFacts is the fact analog of decayRankTurns.
func (s *Store) decayRankFacts(hits []FactHit, p MemoryPolicy, now int64, k int) []FactHit {
	ids := make([]string, 0, len(hits))
	for _, h := range hits {
		ids = append(ids, h.ItemID)
	}
	meta := s.memoryMeta(ids)
	// drop orphans (chromem docs whose SQLite row was TTL-deleted)
	kept := hits[:0]
	for _, h := range hits {
		if _, ok := meta[h.ItemID]; ok {
			kept = append(kept, h)
		}
	}
	hits = kept
	for i := range hits {
		m := meta[hits[i].ItemID]
		hits[i].Score = decayScore(hits[i].Similarity, m.lastAccess, m.createdAt, m.importance, p, now)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if k > 0 && len(hits) > k {
		hits = hits[:k]
	}
	keep := make([]string, 0, len(hits))
	for _, h := range hits {
		keep = append(keep, h.ItemID)
	}
	s.bumpAccess(keep, now)
	return hits
}

// bumpAccess refreshes last_access + bumps access_count (LRU warmth) for the
// recalled items.
func (s *Store) bumpAccess(ids []string, now int64) {
	if len(ids) == 0 {
		return
	}
	args := []any{now}
	args = append(args, toAny(ids)...)
	_, _ = s.db.Exec(`UPDATE memory_items SET last_access = ?, access_count = access_count + 1 WHERE id IN (`+placeholders(len(ids))+`)`, args...)
}

// SweepExpiredPolicy deletes episodic memory_items older than TTLDays whose decay
// score has fallen below 0.05 (using sim=1 as the worst case — only truly stale
// items get swept). Returns the deleted ids. user_facts is never touched.
// chromem docs become harmless orphans; the decay re-rank filters them out by
// the SQLite row being gone.

// BumpImportance adaptively adjusts importance based on recall frequency
// (Ebbinghaus effect: recalled items get stronger; unrecalled items decay).
// ScoreMemory computes a 6-dimension weighted score for a memory item.
// Dimensions: relevance(30%), frequency(24%), diversity(15%), recency(15%),
// consolidation(10%), richness(6%).
func ScoreMemory(recallCount, accessCount, queryDiversity, crossDomainHits int, lastAccess, createdAt int64, conceptTags string, now int64) float64 {
	totalRecalls := float64(max(1, recallCount))
	totalAccess := float64(max(1, accessCount))
	totalDiversity := float64(max(1, queryDiversity))

	relevance := float64(recallCount) / totalRecalls * 0.30
	frequency := float64(accessCount) / totalAccess * 0.24
	diversity := float64(queryDiversity) / totalDiversity * 0.15

	ref := lastAccess
	if ref == 0 {
		ref = createdAt
	}
	ageDays := float64(now-ref) / 86400000.0
	recency := (1.0 / (1.0 + ageDays*0.1)) * 0.15

	consolidation := 0.03
	if crossDomainHits > 0 {
		consolidation = 0.10
	}

	var tags []string
	json.Unmarshal([]byte(conceptTags), &tags)
	richness := float64(len(tags)) / max(1.0, float64(len(tags))) * 0.06
	if len(tags) == 0 {
		richness = 0
	}

	return relevance + frequency + diversity + recency + consolidation + richness
}

// BumpScore updates scoring fields on recall and applies Ebbinghaus decay.
func (s *Store) BumpScore(ids []string, recalled bool, now int64) {
	multiplier := 0.85
	if recalled { multiplier = 1.2 }
	for _, id := range ids {
		s.db.Exec(`UPDATE memory_items SET last_access=?, access_count=access_count+1,
			recall_count=CASE WHEN ? >= 1.2 THEN recall_count+1 ELSE recall_count END,
			importance=CASE
				WHEN importance='critical' THEN 'critical'
				WHEN importance='high' AND ? < 0.85 THEN 'normal'
				WHEN importance='normal' AND ? >= 1.2 THEN 'high'
				WHEN importance='normal' AND ? < 0.85 THEN 'low'
				WHEN importance='low' AND ? >= 1.2 THEN 'normal'
				ELSE importance
			END WHERE id=?`,
			now, multiplier, multiplier, multiplier, multiplier, id)
	}
}

// PromoteByScore re-ranks all memory_items using the 6-dimension score and
// adjusts importance accordingly. Returns counts for promoted/demoted/deleted.
func (s *Store) PromoteByScore(keepCap int) (promoted, demoted, deleted int) {
	now := time.Now().UnixMilli()
	rows, err := s.db.Query(`SELECT id, recall_count, access_count, query_diversity,
		cross_domain_hits, last_access, created_at, concept_tags, importance
		FROM memory_items ORDER BY importance DESC`)
	if err != nil {
		return
	}
	defer rows.Close()
	type scored struct {
		id       string
		score    float64
		imp      string
		recalls  int
		accesses int
	}
	var list []scored
	for rows.Next() {
		var id, imp, tags string
		var rc, ac, qd, cdh int
		var la, ca int64
		if rows.Scan(&id, &rc, &ac, &qd, &cdh, &la, &ca, &tags, &imp) != nil {
			continue
		}
		sc := ScoreMemory(rc, ac, qd, cdh, la, ca, tags, now)
		list = append(list, scored{id, sc, imp, rc, ac})
	}
	// Sort by score descending (stable sort for deterministic behavior).
	sort.SliceStable(list, func(i, j int) bool { return list[i].score > list[j].score })

	// Safety: only delete when significantly over capacity (1.5x buffer).
	// Only delete items with score below minimum threshold AND no usage history.
	const minScoreThreshold = 0.15
	const capacityBuffer = 1.5
	softLimit := int(float64(keepCap) * capacityBuffer)

	for i, v := range list {
		// Promote high scorers.
		if v.score >= 0.6 && v.imp == "normal" {
			s.db.Exec(`UPDATE memory_items SET importance='high' WHERE id=?`, v.id)
			promoted++
		} else if v.score >= 0.6 && v.imp == "low" {
			s.db.Exec(`UPDATE memory_items SET importance='normal' WHERE id=?`, v.id)
			promoted++
		} else if v.score < 0.2 && v.imp == "normal" {
			s.db.Exec(`UPDATE memory_items SET importance='low' WHERE id=?`, v.id)
			demoted++
		}

		// Only delete if ALL conditions are met:
		// 1. Beyond soft capacity limit (not hard cap)
		// 2. Score below minimum threshold
		// 3. Never been recalled or accessed
		// 4. Not critical importance
		if i >= softLimit &&
			v.score < minScoreThreshold &&
			v.recalls == 0 &&
			v.accesses == 0 &&
			v.imp != "critical" {
			s.db.Exec(`DELETE FROM memory_items WHERE id=?`, v.id)
			deleted++
		}
	}
	return
}

// TrimMemoryCapacity enforces a hard cap on total memory_items. Least-important,
// oldest-accessed items are deleted first. Called during the daily sweep.
func (s *Store) TrimMemoryCapacity(hardCap int) (int, error) {
	var total int
	s.db.QueryRow(`SELECT COUNT(*) FROM memory_items`).Scan(&total)
	if total <= hardCap {
		return 0, nil
	}
	excess := total - hardCap
	res, err := s.db.Exec(`DELETE FROM memory_items WHERE id IN (
		SELECT id FROM memory_items ORDER BY
		CASE importance
			WHEN 'critical' THEN 0 WHEN 'high' THEN 1
			WHEN 'normal' THEN 2 WHEN 'low' THEN 3 ELSE 4
		END ASC, last_access ASC LIMIT ?)`, excess)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
func (s *Store) SweepExpiredPolicy() ([]string, error) {
	p := s.Policy()
	now := time.Now().UnixMilli()
	cutoff := now - int64(p.TTLDays)*86400000
	rows, err := s.db.Query(`SELECT id, last_access, created_at, importance FROM memory_items WHERE created_at < ?`, cutoff)
	if err != nil {
		return nil, err
	}
	var victims []string
	for rows.Next() {
		var id string
		var la, ca int64
		var imp string
		if err := rows.Scan(&id, &la, &ca, &imp); err != nil {
			continue
		}
		if decayScore(1.0, la, ca, imp, p, now) < 0.05 {
			victims = append(victims, id)
		}
	}
	rows.Close()
	for _, id := range victims {
		_, _ = s.db.Exec(`DELETE FROM memory_items WHERE id = ?`, id)
	}
	return victims, nil
}

// AddEntity writes a graph-entity vector (kind=entity). No-op when the vector
// layer is unavailable (graph degrades to SQLite-only — no seed-by-similarity).
func (s *Store) AddEntity(nodeID, name, entityType, workspaceID string, emb []float32) error {
	if s.vector == nil {
		return nil
	}
	return s.vector.AddEntity(nodeID, name, entityType, workspaceID, emb)
}

// EntitySearch returns up to k entity seeds matching the embedding, scoped to
// libraryID (legacy 'default' entities included).
func (s *Store) EntitySearch(emb []float32, k int, libraryID string) ([]EntityHit, error) {
	if s.vector == nil {
		return nil, nil
	}
	return s.vector.QueryEntities(emb, k, libraryID)
}

// ListMemoryItems returns the k most recent manifest rows (any kind),
// optionally filtered by workspace_id.
func (s *Store) ListMemoryItems(k int, workspaceID string) ([]MemoryItem, error) {
	if k <= 0 {
		k = 20
	}
	var rows *sql.Rows
	var err error
	if workspaceID != "" {
		// Include legacy 'default' rows so pre-isolation data is still visible
		// in any library view (consistent with kg_nodes behavior).
		rows, err = s.db.Query(`SELECT id, kind, content, reply, category, session_id, created_at, workspace_id
			FROM memory_items WHERE workspace_id = ? OR workspace_id = ? OR workspace_id = 'default' OR workspace_id IS NULL OR workspace_id = '' ORDER BY created_at DESC LIMIT ?`, workspaceID, s.realDefaultLibrary(), k)
	} else {
		rows, err = s.db.Query(`SELECT id, kind, content, reply, category, session_id, created_at, workspace_id
			FROM memory_items ORDER BY created_at DESC LIMIT ?`, k)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryItem
	for rows.Next() {
		var m MemoryItem
		if err := rows.Scan(&m.ID, &m.Kind, &m.Content, &m.Reply, &m.Category, &m.SessionID, &m.CreatedAt, &m.WorkspaceID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ListMemoryItemsSince returns manifest rows created after ts (exclusive),
// oldest first — used for incremental extraction (only turns since the last run).
func (s *Store) ListMemoryItemsSince(ts int64) ([]MemoryItem, error) {
	rows, err := s.db.Query(`SELECT id, kind, content, reply, category, session_id, created_at, workspace_id
		FROM memory_items WHERE created_at > ? ORDER BY created_at ASC`, ts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryItem
	for rows.Next() {
		var m MemoryItem
		if err := rows.Scan(&m.ID, &m.Kind, &m.Content, &m.Reply, &m.Category, &m.SessionID, &m.CreatedAt, &m.WorkspaceID); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ─── Collaboration sessions (persistence) ─────────────────────────

// CollabSessionRow is the persisted shape of a collaboration session.
type CollabSessionRow struct {
	ID, Goal, OrchestratorID, BlackboardID, Status string
	CreatedAt, UpdatedAt                            int64
	Members                                         []CollabMemberRow
}

// CollabMemberRow is one member of a collaboration session.
type CollabMemberRow struct {
	AgentID, Role string
	JoinedAt      int64
}

// SaveCollabSession upserts a collaboration session and its members.
func (s *Store) SaveCollabSession(r CollabSessionRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO collab_sessions(id, goal, orchestrator_id, blackboard_id, status, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET goal=excluded.goal, orchestrator_id=excluded.orchestrator_id,
		blackboard_id=excluded.blackboard_id, status=excluded.status, updated_at=excluded.updated_at`,
		r.ID, r.Goal, r.OrchestratorID, r.BlackboardID, r.Status, r.CreatedAt, r.UpdatedAt); err != nil {
		_ = tx.Rollback()
		return err
	}
	// Replace members.
	if _, err := tx.Exec(`DELETE FROM collab_members WHERE session_id = ?`, r.ID); err != nil {
		_ = tx.Rollback()
		return err
	}
	for _, m := range r.Members {
		if _, err := tx.Exec(`INSERT INTO collab_members(session_id, agent_id, role, joined_at) VALUES(?,?,?,?)`,
			r.ID, m.AgentID, m.Role, m.JoinedAt); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// ListCollabSessions returns all persisted collaboration sessions (with members).
func (s *Store) ListCollabSessions() ([]CollabSessionRow, error) {
	rows, err := s.db.Query(`SELECT id, goal, orchestrator_id, blackboard_id, status, created_at, updated_at FROM collab_sessions ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	var out []CollabSessionRow
	for rows.Next() {
		var r CollabSessionRow
		if err := rows.Scan(&r.ID, &r.Goal, &r.OrchestratorID, &r.BlackboardID, &r.Status, &r.CreatedAt, &r.UpdatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, r)
	}
	rows.Close()
	// Load members for each.
	for i := range out {
		mrows, err := s.db.Query(`SELECT agent_id, role, joined_at FROM collab_members WHERE session_id = ?`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for mrows.Next() {
			var m CollabMemberRow
			if err := mrows.Scan(&m.AgentID, &m.Role, &m.JoinedAt); err == nil {
				out[i].Members = append(out[i].Members, m)
			}
		}
		mrows.Close()
	}
	return out, nil
}

// DeleteCollabSession removes a collaboration session and its members.
func (s *Store) DeleteCollabSession(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM collab_members WHERE session_id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err := tx.Exec(`DELETE FROM collab_sessions WHERE id = ?`, id); err != nil {
		_ = tx.Rollback()
		return err
	}
	// Clean up blackboard entries for this session.
	boardID := "bb_" + id
	if _, err := tx.Exec(`DELETE FROM bb_entries WHERE board_id = ?`, boardID); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// ─── Blackboard entries (persistence) ───────────────────────────

// BBSaveEntry upserts a blackboard entry.
func (s *Store) BBSaveEntry(boardID, key, value, author, kind string, updatedAt int64) error {
	_, err := s.db.Exec(`INSERT INTO bb_entries(board_id, key, value, author, kind, updated_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(board_id, key) DO UPDATE SET
		value=excluded.value, author=excluded.author, kind=excluded.kind, updated_at=excluded.updated_at`,
		boardID, key, value, author, kind, updatedAt)
	return err
}

// BBDeleteEntry removes a single blackboard entry.
func (s *Store) BBDeleteEntry(boardID, key string) error {
	_, err := s.db.Exec(`DELETE FROM bb_entries WHERE board_id = ? AND key = ?`, boardID, key)
	return err
}

// BBLoadEntries returns all entries for a given blackboard.
func (s *Store) BBLoadEntries(boardID string) ([]BBEntry, error) {
	rows, err := s.db.Query(`SELECT board_id, key, value, author, kind, updated_at
		FROM bb_entries WHERE board_id = ? ORDER BY updated_at DESC`, boardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BBEntry
	for rows.Next() {
		var e BBEntry
		if err := rows.Scan(&e.BoardID, &e.Key, &e.Value, &e.Author, &e.Kind, &e.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// BBClearBoard removes all entries for a blackboard.
func (s *Store) BBClearBoard(boardID string) error {
	_, err := s.db.Exec(`DELETE FROM bb_entries WHERE board_id = ?`, boardID)
	return err
}

// BBEntry is a persisted blackboard entry.
type BBEntry struct {
	BoardID   string `json:"boardId"`
	Key       string `json:"key"`
	Value     string `json:"value"`
	Author    string `json:"author"`
	Kind      string `json:"kind"`
	UpdatedAt int64  `json:"updatedAt"`
}

// CountMemory returns the turn and fact counts, optionally filtered by workspace_id.
func (s *Store) CountMemory(workspaceID string) (turns int, facts int) {
	if workspaceID != "" {
		// Match ListMemoryItems: include legacy 'default' rows so counts agree.
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM memory_items WHERE kind='turn' AND (workspace_id = ? OR workspace_id = ? OR workspace_id = 'default' OR workspace_id IS NULL OR workspace_id = '')`, workspaceID, s.realDefaultLibrary()).Scan(&turns)
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM memory_items WHERE kind='fact' AND (workspace_id = ? OR workspace_id = ? OR workspace_id = 'default' OR workspace_id IS NULL OR workspace_id = '')`, workspaceID, s.realDefaultLibrary()).Scan(&facts)
	} else {
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM memory_items WHERE kind='turn'`).Scan(&turns)
		_ = s.db.QueryRow(`SELECT COUNT(*) FROM memory_items WHERE kind='fact'`).Scan(&facts)
	}
	return
}

// ListLowImportanceItems returns the k least important, oldest memory items.
func (s *Store) ListLowImportanceItems(k int) ([]MemoryItem, error) {
	rows, err := s.db.Query(`SELECT id, kind, content, reply, category FROM memory_items
		WHERE importance='low' OR importance='normal'
		ORDER BY CASE importance WHEN 'low' THEN 0 WHEN 'normal' THEN 1 END ASC,
		last_access ASC LIMIT ?`, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryItem
	for rows.Next() {
		var m MemoryItem
		if err := rows.Scan(&m.ID, &m.Kind, &m.Content, &m.Reply, &m.Category); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, rows.Err()
}


// MigrateDefaultWorkspace updates legacy 'default' and NULL workspace_id values to
// the actual library ID across all scoped tables. SQLite ADD COLUMN with DEFAULT
// does NOT backfill existing rows — they stay NULL until explicitly updated.
func (s *Store) MigrateDefaultWorkspace(libID string) {
	tables := []string{"kg_nodes", "kg_edges", "memory_items", "user_facts", "experience_items"}
	for _, table := range tables {
		retryExec(s.db, `UPDATE `+table+` SET workspace_id=? WHERE workspace_id='default' OR workspace_id IS NULL OR workspace_id=''`, libID)
	}
}

// retryExec runs an Exec with up to 3 retries on SQLITE_BUSY (lock contention).
func retryExec(db *sql.DB, query string, args ...any) {
	for attempt := 0; attempt < 3; attempt++ {
		_, err := db.Exec(query, args...)
		if err == nil {
			return
		}
		if isSQLiteBusy(err) && attempt < 2 {
			time.Sleep(time.Duration(50*(1<<attempt)) * time.Millisecond)
			continue
		}
		log.Printf("[memory] retryExec failed after %d attempts: %v (query: %s)", attempt+1, err, query[:min(80, len(query))])
		return
	}
}

func isSQLiteBusy(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "database is locked") ||
		strings.Contains(err.Error(), "SQLITE_BUSY"))
}
// ─── Dream Candidates (P9) ──────────────────────────────────────

func (s *Store) AddDreamCandidate(id, sourceID, sourceType, stage string, now int64) error {
	_, err := s.db.Exec(`INSERT OR REPLACE INTO dream_candidates(id,source_id,source_type,stage,score,insight,created_at) VALUES(?,?,?,?,0,'',?)`, id, sourceID, sourceType, stage, now)
	return err
}

func (s *Store) PromoteDreamStage(from, to string) (int, error) {
	res, err := s.db.Exec(`UPDATE dream_candidates SET stage=? WHERE stage=?`, to, from)
	if err != nil { return 0, err }
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (s *Store) ClearDreamCandidates() { s.db.Exec(`DELETE FROM dream_candidates`) }

// ClearMemory wipes the manifest and the vector collection.
func (s *Store) ClearMemory() error {
	if _, err := s.db.Exec(`DELETE FROM memory_items`); err != nil {
		return err
	}
	if s.vector != nil {
		if err := s.vector.Clear(); err != nil {
			return err
		}
	}
	return nil
}

// MigrateModel re-embeds every memory item with a new model and rebinds.
// embedBatch must load the new model once and embed in bulk (e.g. rag.EmbedChunks).
// Safe order: read all → embed all → only then clear + rewrite → rebind. If any
// embed fails, existing vectors are left untouched.
func (s *Store) MigrateModel(newDir string, embedBatch func([]string) ([][]float32, error)) error {
	if s.vector == nil {
		return fmt.Errorf("向量层未就绪")
	}
	rows, err := s.db.Query(`SELECT id, kind, content, reply, category, session_id FROM memory_items ORDER BY created_at`)
	if err != nil {
		return err
	}
	var items []MemoryItem
	for rows.Next() {
		var it MemoryItem
		if err := rows.Scan(&it.ID, &it.Kind, &it.Content, &it.Reply, &it.Category, &it.SessionID); err != nil {
			rows.Close()
			return err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(items) == 0 {
		return s.SetEmbeddingModel(newDir) // no data — just rebind
	}
	texts := make([]string, len(items))
	for i, it := range items {
		texts[i] = it.Content
	}
	vecs, err := embedBatch(texts)
	if err != nil {
		return fmt.Errorf("重新嵌入失败: %w", err)
	}
	if len(vecs) != len(items) {
		return fmt.Errorf("嵌入数量不匹配: %d vs %d", len(vecs), len(items))
	}
	if err := s.vector.Clear(); err != nil {
		return err
	}
	for i, it := range items {
		ws := it.WorkspaceID
		if ws == "" {
			ws = "default"
		}
		if it.Kind == "turn" {
			if err := s.vector.AddTurn(it.ID, it.Content, it.Reply, it.SessionID, it.ID, ws, vecs[i]); err != nil {
				log.Printf("[memory] 迁移写入失败 %s: %v", it.ID, err)
			}
		} else {
			if err := s.vector.AddFact(it.ID, it.Content, it.Category, it.ID, ws, vecs[i]); err != nil {
				log.Printf("[memory] 迁移写入失败 %s: %v", it.ID, err)
			}
		}
	}
	return s.SetEmbeddingModel(newDir)
}

// ─── Chunk Registry (bidirectional index: vector ↔ source document) ────────

// ChunkRegistryEntry is a row in the chunk_registry table.
type ChunkRegistryEntry struct {
	ChunkID     string `json:"chunkId"`
	SourceType  string `json:"sourceType"`
	SourceID    string `json:"sourceId"`
	ChunkIndex  int    `json:"chunkIndex"`
	ParentID    string `json:"parentId,omitempty"`
	PrevID      string `json:"prevId,omitempty"`
	NextID      string `json:"nextId,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
	ByteStart   int    `json:"byteStart"`
	ByteEnd     int    `json:"byteEnd"`
	ChunkType   string `json:"chunkType"`
	CreatedAt   int64  `json:"createdAt"`
}

// RegisterChunk inserts or replaces a chunk registry entry.
func (s *Store) RegisterChunk(e ChunkRegistryEntry) error {
	if e.ChunkID == "" || e.SourceType == "" || e.SourceID == "" {
		return fmt.Errorf("chunk_registry: chunk_id, source_type, and source_id are required")
	}
	if e.ChunkType == "" {
		e.ChunkType = "leaf"
	}
	if e.CreatedAt == 0 {
		e.CreatedAt = time.Now().UnixMilli()
	}
	_, err := s.db.Exec(`INSERT OR REPLACE INTO chunk_registry
		(chunk_id, source_type, source_id, chunk_index, parent_id, prev_id, next_id,
		 content_hash, byte_start, byte_end, chunk_type, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ChunkID, e.SourceType, e.SourceID, e.ChunkIndex,
		nullIfEmpty(e.ParentID), nullIfEmpty(e.PrevID), nullIfEmpty(e.NextID),
		nullIfEmpty(e.ContentHash), e.ByteStart, e.ByteEnd, e.ChunkType, e.CreatedAt)
	return err
}

// RegisterChunks bulk-inserts chunk registry entries in a transaction.
func (s *Store) RegisterChunks(entries []ChunkRegistryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, e := range entries {
		if e.ChunkType == "" {
			e.ChunkType = "leaf"
		}
		if e.CreatedAt == 0 {
			e.CreatedAt = time.Now().UnixMilli()
		}
		_, err := tx.Exec(`INSERT OR REPLACE INTO chunk_registry
			(chunk_id, source_type, source_id, chunk_index, parent_id, prev_id, next_id,
			 content_hash, byte_start, byte_end, chunk_type, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.ChunkID, e.SourceType, e.SourceID, e.ChunkIndex,
			nullIfEmpty(e.ParentID), nullIfEmpty(e.PrevID), nullIfEmpty(e.NextID),
			nullIfEmpty(e.ContentHash), e.ByteStart, e.ByteEnd, e.ChunkType, e.CreatedAt)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetChunk returns a single chunk registry entry by chunk_id.
func (s *Store) GetChunk(chunkID string) (*ChunkRegistryEntry, error) {
	var e ChunkRegistryEntry
	var parentID, prevID, nextID, contentHash sql.NullString
	var bStart, bEnd int64
	err := s.db.QueryRow(`SELECT chunk_id, source_type, source_id, chunk_index,
		parent_id, prev_id, next_id, content_hash, byte_start, byte_end, chunk_type, created_at
		FROM chunk_registry WHERE chunk_id = ?`, chunkID).Scan(
		&e.ChunkID, &e.SourceType, &e.SourceID, &e.ChunkIndex,
		&parentID, &prevID, &nextID, &contentHash,
		&bStart, &bEnd, &e.ChunkType, &e.CreatedAt)
	if err != nil {
		return nil, err
	}
	e.ParentID = parentID.String
	e.PrevID = prevID.String
	e.NextID = nextID.String
	e.ContentHash = contentHash.String
	e.ByteStart = int(bStart)
	e.ByteEnd = int(bEnd)
	return &e, nil
}

// GetChunksBySource returns all chunk entries for a source, ordered by chunk_index.
func (s *Store) GetChunksBySource(sourceType, sourceID string) ([]ChunkRegistryEntry, error) {
	rows, err := s.db.Query(`SELECT chunk_id, source_type, source_id, chunk_index,
		COALESCE(parent_id,''), COALESCE(prev_id,''), COALESCE(next_id,''),
		COALESCE(content_hash,''), COALESCE(byte_start,0), COALESCE(byte_end,0), chunk_type, created_at
		FROM chunk_registry WHERE source_type = ? AND source_id = ?
		ORDER BY chunk_index`, sourceType, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ChunkRegistryEntry
	for rows.Next() {
		var e ChunkRegistryEntry
		if err := rows.Scan(&e.ChunkID, &e.SourceType, &e.SourceID, &e.ChunkIndex,
			&e.ParentID, &e.PrevID, &e.NextID, &e.ContentHash,
			&e.ByteStart, &e.ByteEnd, &e.ChunkType, &e.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetChunkSiblings returns the parent, prev, and next siblings for a chunk,
// enabling the hierarchical retrieval expansion pattern.
func (s *Store) GetChunkSiblings(chunkID string) (parent *ChunkRegistryEntry, prev *ChunkRegistryEntry, next *ChunkRegistryEntry, _ error) {
	entry, err := s.GetChunk(chunkID)
	if err != nil {
		return nil, nil, nil, err
	}
	if entry.ParentID != "" {
		parent, _ = s.GetChunk(entry.ParentID)
	}
	if entry.PrevID != "" {
		prev, _ = s.GetChunk(entry.PrevID)
	}
	if entry.NextID != "" {
		next, _ = s.GetChunk(entry.NextID)
	}
	return parent, prev, next, nil
}

// ParentForLeafs checks whether top-K leaf results share the same parent.
// If ≥threshold fraction of chunkIDs share a common parent, returns that parent
// (AutoMergingRetriever pattern). Otherwise returns nil.
func (s *Store) ParentForLeafs(chunkIDs []string, threshold float64) *ChunkRegistryEntry {
	if len(chunkIDs) == 0 || threshold <= 0 || threshold > 1 {
		return nil
	}
	parentCount := make(map[string]int)
	for _, id := range chunkIDs {
		e, err := s.GetChunk(id)
		if err != nil || e.ParentID == "" {
			continue
		}
		parentCount[e.ParentID]++
	}
	for parentID, count := range parentCount {
		if float64(count)/float64(len(chunkIDs)) >= threshold {
			p, _ := s.GetChunk(parentID)
			return p
		}
	}
	return nil
}

// DeleteChunksBySource removes all chunk registry entries for a source.
func (s *Store) DeleteChunksBySource(sourceType, sourceID string) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM chunk_registry WHERE source_type = ? AND source_id = ?`,
		sourceType, sourceID)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
