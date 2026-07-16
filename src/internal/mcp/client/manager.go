package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"everevo/internal/atomic"
	"everevo/internal/storage"
	"everevo/internal/tools"
)

// Manager manages all external MCP server connections.
type Manager struct {
	mu          sync.RWMutex
	connections map[string]*Connection // key = server ID
	persistPath string
}

// NewManager creates a new MCP client manager and loads persisted config.
func NewManager() *Manager {
	dataDir := storage.DataDir()
	path := filepath.Join(dataDir, "mcp_servers.json")
	return &Manager{
		connections: make(map[string]*Connection),
		persistPath: path,
	}
}

// ─── Persistence ────────────────────────────────────────────────

func (m *Manager) load() error {
	data, err := os.ReadFile(m.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no config yet
		}
		return err
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return fmt.Errorf("parse mcp_servers.json: %w", err)
	}
	for i := range store.Servers {
		cfg := store.Servers[i]
		cfg.Status = "disconnected"
		conn := &Connection{Cfg: cfg}
		m.connections[cfg.ID] = conn
	}
	return nil
}

func (m *Manager) save() error {
	// Snapshot connections under a read lock — safe to call even if
	// the caller holds m.mu.Lock() (RLock is compatible with Lock
	// on the same goroutine... no, that deadlocks. So we take a
	// snapshot WITHOUT m.mu — callers are responsible for holding
	// m.mu if they are mutating m.connections concurrently.)
	//
	// Serialize writes with a dedicated mutex so concurrent save()
	// calls (e.g. from N auto-connect goroutines) don't interleave
	// os.WriteFile on the same file.
	saveMu.Lock()
	defer saveMu.Unlock()

	var cfgs []ServerConfig
	for _, c := range m.connections {
		c.mu.Lock()
		cfg := c.Cfg
		cfg.Status = "disconnected"
		cfg.Error = ""
		c.mu.Unlock()
		cfgs = append(cfgs, cfg)
	}
	store := Store{Servers: cfgs}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("mcp save: marshal: %w", err)
	}
	dir := filepath.Dir(m.persistPath)
	os.MkdirAll(dir, 0755)
	if err := atomic.WriteFile(m.persistPath, data, 0644); err != nil {
		return fmt.Errorf("mcp save: write: %w", err)
	}
	return nil
}

// saveMu serializes disk writes across concurrent save() calls.
var saveMu sync.Mutex

// LoadAndConnect loads persisted config and auto-connects all servers.
func (m *Manager) LoadAndConnect() {
	if err := m.load(); err != nil {
		log.Printf("[mcp-client] load config: %v", err)
	}
	for id, conn := range m.connections {
		go func(cid string, c *Connection) {
			if err := m.Connect(cid); err != nil {
				log.Printf("[mcp-client] auto-connect %s: %v", c.Cfg.Name, err)
			}
		}(id, conn)
	}
}

// ─── CRUD ───────────────────────────────────────────────────────

// AddServer adds a new server config and persists.
func (m *Manager) AddServer(cfg ServerConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cfg.ID == "" {
		maxN := 0
		for id := range m.connections {
			var n int
			if _, err := fmt.Sscanf(id, "srv_%d", &n); err == nil && n > maxN {
				maxN = n
			}
		}
		cfg.ID = fmt.Sprintf("srv_%d", maxN+1)
	}
	if _, exists := m.connections[cfg.ID]; exists {
		return fmt.Errorf("server %q already exists", cfg.ID)
	}
	cfg.Status = "disconnected"
	m.connections[cfg.ID] = &Connection{Cfg: cfg}
	return m.save()
}

// RemoveServer removes a server and disconnects if active.
func (m *Manager) RemoveServer(id string) error {
	m.mu.Lock()
	conn, ok := m.connections[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("server %q not found", id)
	}
	if conn.transport != nil {
		conn.transport.Close()
	}
	delete(m.connections, id)
	m.mu.Unlock()

	// Clean up registered tools
	tools.UnregisterExternal(id)
	return m.save()
}

// UpdateServer updates an existing server config (requires reconnect).
func (m *Manager) UpdateServer(cfg ServerConfig) error {
	m.mu.Lock()
	conn, ok := m.connections[cfg.ID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("server %q not found", cfg.ID)
	}
	// Disconnect if connected
	if conn.transport != nil {
		conn.transport.Close()
		conn.transport = nil
	}
	tools.UnregisterExternal(cfg.ID)
	conn.Cfg = cfg
	conn.Cfg.Status = "disconnected"
	conn.Cfg.Error = ""
	conn.Tools = nil
	m.mu.Unlock()
	return m.save()
}

// ListServers returns all configured server statuses.
func (m *Manager) ListServers() []ServerConfig {
	return m.ListServersByLibrary("")
}

// ListServersByLibrary returns servers filtered by library ID. Empty libraryID
// returns all servers (backward-compatible).
func (m *Manager) ListServersByLibrary(libraryID string) []ServerConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []ServerConfig
	for _, c := range m.connections {
		c.mu.Lock()
		cfg := c.Cfg
		if c.Tools != nil {
			cfg.ToolCount = len(c.Tools)
		}
		c.mu.Unlock()
		if libraryID != "" && cfg.LibraryID != libraryID {
			continue
		}
		out = append(out, cfg)
	}
	return out
}

// BackfillLibraryIDs sets LibraryID on all servers that don't have one (or have
// an invalid/dangling one), then saves. Safe to call at startup. validIDs is the
// set of current domain library IDs from the memory store.
func (m *Manager) BackfillLibraryIDs(defaultLibraryID string, validIDs []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	valid := make(map[string]bool, len(validIDs))
	for _, id := range validIDs {
		valid[id] = true
	}
	changed := false
	for _, c := range m.connections {
		c.mu.Lock()
		if c.Cfg.LibraryID == "" || !valid[c.Cfg.LibraryID] {
			if c.Cfg.LibraryID != "" {
				log.Printf("[mcp] MCP Server %q 的 libraryId %q 无效，回填为默认领域", c.Cfg.Name, c.Cfg.LibraryID)
			}
			c.Cfg.LibraryID = defaultLibraryID
			changed = true
		}
		c.mu.Unlock()
	}
	if changed {
		_ = m.save()
	}
}

// ReassignByHeuristic re-assigns MCP servers that are still in the default
// library to more appropriate domains based on naming heuristics. Called at
// startup after BackfillLibraryIDs. validIDs maps domain name → domain ID.
func (m *Manager) ReassignByHeuristic(defaultLibraryID string, validIDs map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Heuristic rules: keyword → domain name
	rules := []struct{ keyword, domainName string }{
		{"search", "搜索"},
		{"searx", "搜索"},
		{"github", "开发"},
		{"git", "开发"},
		{"code", "开发"},
		{"file", "文件管理"},
		{"filesystem", "文件管理"},
		{"fs", "文件管理"},
		{"browser", "浏览器"},
		{"playwright", "浏览器"},
		{"web", "浏览器"},
		{"mcp", "MCP"},
		{"server", "MCP"},
	}
	changed := false
	for _, conn := range m.connections {
		conn.mu.Lock()
		// Only reassign if still in the default library.
		if conn.Cfg.LibraryID != defaultLibraryID && conn.Cfg.LibraryID != "" {
			conn.mu.Unlock()
			continue
		}
		lcName := strings.ToLower(conn.Cfg.Name)
		matched := false
		for _, r := range rules {
			if strings.Contains(lcName, r.keyword) {
				// Try exact domain name match first.
				if id, ok := validIDs[r.domainName]; ok && id != "" {
					conn.Cfg.LibraryID = id
					matched = true
					log.Printf("[mcp] %q -> domain %q (keyword: %s)", conn.Cfg.Name, r.domainName, r.keyword)
					break
				}
				// Fuzzy: find any domain whose name contains the keyword.
				for name, id := range validIDs {
					if strings.Contains(strings.ToLower(name), r.keyword) || strings.Contains(strings.ToLower(r.keyword), strings.ToLower(name)) {
						conn.Cfg.LibraryID = id
						matched = true
						log.Printf("[mcp] %q -> domain %q (fuzzy: %s)", conn.Cfg.Name, name, r.keyword)
						break
					}
				}
				if matched {
					break
				}
			}
		}
		if matched {
			changed = true
		}
		conn.mu.Unlock()
	}
	if changed {
		_ = m.save()
	}
}

// GetServerTools returns the tools discovered from a connected server.
func (m *Manager) GetServerTools(id string) ([]*tools.ToolDef, error) {
	m.mu.RLock()
	conn, ok := m.connections[id]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("server %q not found", id)
	}
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.Cfg.Status != "connected" {
		return nil, fmt.Errorf("server %q not connected", id)
	}
	return conn.Tools, nil
}

// ─── Connect / Disconnect ───────────────────────────────────────

// Connect establishes transport, initializes MCP, and discovers tools.
func (m *Manager) Connect(id string) error {
	m.mu.RLock()
	conn, ok := m.connections[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %q not found", id)
	}

	conn.mu.Lock()
	cfg := conn.Cfg
	conn.mu.Unlock()

	// Mark connecting
	conn.mu.Lock()
	conn.Cfg.Status = "connecting"
	conn.Cfg.Error = ""
	conn.mu.Unlock()

	type connResult struct{ err error }
	done := make(chan connResult, 1)
	go func() { done <- connResult{err: m.doConnect(conn, id, cfg)} }()
	select {
	case r := <-done:
		return r.err
	case <-time.After(120 * time.Second):
		conn.mu.Lock()
		conn.Cfg.Status = "error"
		conn.Cfg.Error = "连接超时（120s）"
		conn.mu.Unlock()
		return fmt.Errorf("connection timed out after 120s")
	}
}

func (m *Manager) doConnect(conn *Connection, id string, cfg ServerConfig) error {
	var t Transport
	var err error

	switch cfg.Transport {
	case "stdio":
		if cfg.Command == "" {
			err = fmt.Errorf("stdio transport requires a command")
			break
		}
		if cfg.Command == "npx" || cfg.Command == "npm" {
			if pkg := extractNPMPackage(cfg.Args); pkg != "" {
				if ee := ensureNpmPackage(pkg); ee != nil {
					log.Printf("[mcp] npm dependency check/install failed for %s: %v", pkg, ee)
					// non-fatal: npx may still resolve the package at runtime
				}
			}
		}
		t, err = newStdioTransport(cfg.Command, cfg.Args, cfg.Env)
	case "http":
		if cfg.URL == "" {
			err = fmt.Errorf("http transport requires a URL")
			break
		}
		t = newHTTPTransport(cfg.URL)
	default:
		err = fmt.Errorf("unknown transport: %s", cfg.Transport)
	}

	if err != nil {
		conn.mu.Lock()
		conn.Cfg.Status = "error"
		conn.Cfg.Error = err.Error()
		conn.mu.Unlock()
		m.save()
		return err
	}

	// Initialize MCP handshake
	initParams := initializeParams{
		ProtocolVersion: "2025-06-18",
		Capabilities:    clientCaps{},
		ClientInfo: clientInfo{
			Name:    "EverEvo",
			Version: "0.2.0",
		},
	}

	result, err := t.Send("initialize", initParams)
	if err != nil {
		t.Close()
		conn.mu.Lock()
		conn.Cfg.Status = "error"
		conn.Cfg.Error = "initialize: " + err.Error()
		conn.mu.Unlock()
		m.save()
		return fmt.Errorf("initialize: %w", err)
	}

	var initRes initializeResult
	if err := json.Unmarshal(result, &initRes); err != nil {
		t.Close()
		conn.mu.Lock()
		conn.Cfg.Status = "error"
		conn.Cfg.Error = "parse init response: " + err.Error()
		conn.mu.Unlock()
		m.save()
		return fmt.Errorf("parse init: %w", err)
	}

	// Send initialized notification (no params)
	t.Send("notifications/initialized", nil)

	// Discover tools
	toolsResult, err := t.Send("tools/list", nil)
	if err != nil {
		t.Close()
		conn.mu.Lock()
		conn.Cfg.Status = "error"
		conn.Cfg.Error = "tools/list: " + err.Error()
		conn.mu.Unlock()
		m.save()
		return fmt.Errorf("tools/list: %w", err)
	}

	var listRes listToolsResult
	if err := json.Unmarshal(toolsResult, &listRes); err != nil {
		t.Close()
		conn.mu.Lock()
		conn.Cfg.Status = "error"
		conn.Cfg.Error = "parse tools: " + err.Error()
		conn.mu.Unlock()
		m.save()
		return fmt.Errorf("parse tools: %w", err)
	}

	// Convert MCP tools to internal ToolDef format
	toolDefs := make([]*tools.ToolDef, 0, len(listRes.Tools))
	for _, mt := range listRes.Tools {
		td := mcpToInternalTool(mt, id, cfg.Name)
		toolDefs = append(toolDefs, td)
	}

	// Register external tools
	tools.UnregisterExternal(id) // clear stale
	for _, td := range toolDefs {
		tools.RegisterExternal(td, id)
	}

	conn.mu.Lock()
	conn.transport = t
	conn.Tools = toolDefs
	conn.Cfg.Status = "connected"
	conn.Cfg.Error = ""
	conn.mu.Unlock()

	m.save()
	log.Printf("[mcp-client] connected to %s (%s) — %d tools", cfg.Name, id, len(toolDefs))
	return nil
}

// Disconnect closes a connection and cleans up tools.
func (m *Manager) Disconnect(id string) error {
	m.mu.RLock()
	conn, ok := m.connections[id]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("server %q not found", id)
	}

	conn.mu.Lock()
	if conn.transport != nil {
		conn.transport.Close()
		conn.transport = nil
	}
	conn.Tools = nil
	conn.Cfg.Status = "disconnected"
	conn.Cfg.Error = ""
	conn.mu.Unlock()

	tools.UnregisterExternal(id)
	m.save()
	return nil
}

// ─── Tool Call ──────────────────────────────────────────────────

// CallTool forwards a tool call to the appropriate external MCP server.
func (m *Manager) CallTool(fullName string, params map[string]any) (*tools.ToolResult, error) {
	// fullName format: "mcp__<server_id>__<tool_name>"
	parts := strings.SplitN(fullName, "__", 3)
	if len(parts) < 3 || parts[0] != "mcp" {
		return nil, fmt.Errorf("invalid external tool name: %s", fullName)
	}
	serverID := parts[1]
	toolName := parts[2]

	m.mu.RLock()
	conn, ok := m.connections[serverID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("server %q not found", serverID)
	}

	conn.mu.Lock()
	t := conn.transport
	conn.mu.Unlock()
	if t == nil {
		return nil, fmt.Errorf("server %q not connected", serverID)
	}

	// MCP servers require `arguments` to be an object even when the tool takes
	// no params (e.g. list_allowed_directories returns -32602 "expected object,
	// received undefined" if omitted). Guarantee a non-nil map.
	if params == nil {
		params = map[string]any{}
	}
	callParams := callToolParams{
		Name:      toolName,
		Arguments: params,
	}
	result, err := t.Send("tools/call", callParams)
	if err != nil {
		return &tools.ToolResult{
			Success: false,
			Error:   err.Error(),
		}, nil
	}

	var ctRes callToolResult
	if err := json.Unmarshal(result, &ctRes); err != nil {
		return &tools.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("parse call result: %v", err),
		}, nil
	}

	// Flatten content blocks to text
	var texts []string
	for _, block := range ctRes.Content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	output := strings.Join(texts, "\n")
	if output == "" {
		output = "OK"
	}

	data, _ := json.Marshal(output)
	return &tools.ToolResult{
		Success: !ctRes.IsError,
		Data:    data,
	}, nil
}

// ─── Recommends ─────────────────────────────────────────────────

// GetRecommends returns the built-in recommended MCP server list.
func GetRecommends() []RecommendInfo {
	return []RecommendInfo{
		{
			Key: "filesystem", Name: "Filesystem",
			Description: "读取、写入、搜索本地文件系统（含上传目录）",
			Transport:   "stdio", Command: "npx",
			Args:     []string{"-y", "@modelcontextprotocol/server-filesystem", ".", "data/uploads", "data/skills", "data/plugins", "data/guides", "data/memory"},
			Category: "filesystem",
		},
		{
			Key: "github", Name: "GitHub",
			Description: "搜索仓库、管理 Issue/PR、查看代码",
			Transport:   "stdio", Command: "npx",
			Args:     []string{"-y", "@modelcontextprotocol/server-github"},
			Category: "dev",
		},
		{
			Key: "puppeteer", Name: "Puppeteer",
			Description: "浏览器自动化：打开网页、截图、点击、提取内容",
			Transport:   "stdio", Command: "npx",
			Args:     []string{"-y", "@anthropic/mcp-server-puppeteer"},
			Category: "browser",
		},
		{
			Key: "sqlite", Name: "SQLite",
			Description: "查询和管理 SQLite 数据库",
			Transport:   "stdio", Command: "npx",
			Args:     []string{"-y", "@anthropic/mcp-server-sqlite"},
			Category: "data",
		},
		{
			Key: "postgres", Name: "PostgreSQL",
			Description: "连接和查询 PostgreSQL 数据库",
			Transport:   "stdio", Command: "npx",
			Args:     []string{"-y", "@modelcontextprotocol/server-postgres"},
			Category: "data",
		},
		{
			Key: "brave-search", Name: "Brave Search",
			Description: "使用 Brave Search API 搜索互联网",
			Transport:   "stdio", Command: "npx",
			Args:     []string{"-y", "@anthropic/mcp-server-brave-search"},
			Category: "data",
		},
	}
}

// ─── helpers ────────────────────────────────────────────────────

// extractNPMPackage pulls the npm package name from npx args.
func extractNPMPackage(args []string) string {
	for i, a := range args {
		if (a == "-y" || a == "--yes") && i+1 < len(args) {
			return args[i+1]
		}
		if !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

// ensureNpmPackage installs an npm package globally if not already present.
// Each npm invocation has its own timeout so a slow registry or network doesn't
// block the caller indefinitely.
func ensureNpmPackage(pkg string) error {
	// Quick check: test whether the package is already globally available.
	// npm ls --depth=0 is fast for installed packages (pure local check).
	{
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "npm", "ls", "-g", pkg, "--depth=0")
		if cmd.Run() == nil {
			return nil // already installed
		}
	}
	log.Printf("[mcp] installing %s (may take a minute)…", pkg)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "npm", "install", "-g", pkg)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("npm install %s failed: %w\n%s", pkg, err, string(out))
	}
	log.Printf("[mcp] %s installed", pkg)
	return nil
}

func mcpToInternalTool(mt mcpToolDef, serverID, serverName string) *tools.ToolDef {
	// Build namespaced name: mcp__<server_id>__<tool_name>
	fullName := "mcp__" + serverID + "__" + mt.Name

	// Parse the raw inputSchema for typed property extraction.
	// We preserve the raw JSON separately so complex schema constructs
	// (anyOf, oneOf, $ref, etc.) survive the round-trip to the LLM API.
	var schema mcpInputSchema
	if len(mt.InputSchema) > 0 {
		if err := json.Unmarshal(mt.InputSchema, &schema); err != nil {
			// Best-effort: fall back to empty schema on parse failure
			schema = mcpInputSchema{}
		}
	}

	props := make(map[string]tools.ToolProp)
	var required []string
	if schema.Properties != nil {
		for k, v := range schema.Properties {
			propType := v.Type
			// Defensive: some MCP servers emit empty type fields, which
			// DeepSeek/OpenAI reject as invalid JSON Schema. Default to
			// "string" so the tool is still usable.
			if propType == "" {
				propType = "string"
			}
			props[k] = tools.ToolProp{
				Type:        propType,
				Description: v.Description,
				Enum:        v.Enum,
				Default:     v.Default,
			}
		}
		required = schema.Required
	}

	title := mt.Title
	if title == "" {
		title = mt.Name
	}

	return &tools.ToolDef{
		Name:        fullName,
		Title:       "[" + serverName + "] " + title,
		Description: mt.Description,
		Category:    "mcp__" + serverID,
		Parameters: &tools.ToolParams{
			Type:       "object",
			Properties: props,
			Required:   required,
		},
		RawParameters: mt.InputSchema,
	}
}
