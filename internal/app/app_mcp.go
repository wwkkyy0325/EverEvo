//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"log"

	"everevo/internal/config"
	"everevo/internal/mcp"
	mcpclient "everevo/internal/mcp/client"
	"everevo/internal/tools"
)

// ─── MCP ResourceProvider implementation ────────────────────────

func (a *App) ListModelsJSON() (json.RawMessage, error)   { return json.Marshal(a.ListModels()) }
func (a *App) ListDownloadedModelsJSON() (json.RawMessage, error) { return json.Marshal(a.ListDownloadedModels()) }
func (a *App) ListToolModelsJSON() (json.RawMessage, error) { return json.Marshal(a.ListToolModels()) }
func (a *App) ListPluginsJSON() (json.RawMessage, error) {
	list, err := a.ListPlugins()
	if err != nil { return nil, err }
	return json.Marshal(list)
}
func (a *App) GetPluginStatusJSON(name string) (json.RawMessage, error) {
	return json.Marshal(a.GetPluginStatus(name))
}
func (a *App) ListKBsJSON() (json.RawMessage, error) {
	list, err := a.ListKnowledgeBases("")
	if err != nil { return nil, err }
	return json.Marshal(list)
}
func (a *App) GetSysInfoJSON() (json.RawMessage, error)     { return json.Marshal(a.GetSysInfo()) }
func (a *App) GetDynamicInfoJSON() (json.RawMessage, error)  { return json.Marshal(a.GetDynamicInfo()) }
func (a *App) GetBackendsJSON() (json.RawMessage, error)     { return json.Marshal(a.GetBackends()) }

// ─── MCP Server 管理 API ────────────────────────────────────────

// GetMCPStatus returns the MCP server status.
func (a *App) GetMCPStatus() mcp.Status {
	if a.mcpServer == nil { return mcp.Status{Running: false} }
	return a.mcpServer.Status()
}

// ─── MCP Client 管理 API ─────────────────────────────────────────

// ListMCPServers returns all configured external MCP servers.
// Pass empty libraryId to list all servers (backward-compatible).
func (a *App) ListMCPServers(libraryId string) []mcpclient.ServerConfig {
	if a.mcpClient == nil { return []mcpclient.ServerConfig{} }
	return a.mcpClient.ListServersByLibrary(libraryId)
}

// AddMCPServer adds and persists a new MCP server config. LibraryID is required.
func (a *App) AddMCPServer(cfg mcpclient.ServerConfig) error {
	if err := a.validateLibraryID(cfg.LibraryID); err != nil {
		return fmt.Errorf("添加 MCP Server 失败: %w", err)
	}
	if err := a.mcpClient.AddServer(cfg); err != nil {
		return err
	}
	a.emitChanged("mcp:changed", "update", cfg.Name)
	return nil
}

// RemoveMCPServer removes an MCP server and disconnects it.
func (a *App) RemoveMCPServer(id string) error {
	if err := a.mcpClient.RemoveServer(id); err != nil {
		return err
	}
	a.emitChanged("mcp:changed", "update", id)
	return nil
}

// ConnectMCPServer connects to an MCP server and discovers its tools.
func (a *App) ConnectMCPServer(id string) error {
	if err := a.mcpClient.Connect(id); err != nil {
		return err
	}
	a.emitChanged("mcp:changed", "update", id)
	return nil
}

// DisconnectMCPServer disconnects from an MCP server.
func (a *App) DisconnectMCPServer(id string) error {
	if err := a.mcpClient.Disconnect(id); err != nil {
		return err
	}
	a.emitChanged("mcp:changed", "update", id)
	return nil
}

// GetMCPServerTools returns tools from a connected MCP server.
func (a *App) GetMCPServerTools(id string) ([]*tools.ToolDef, error) {
	return a.mcpClient.GetServerTools(id)
}

// ListMCPRecommends returns the recommended MCP server list.
func (a *App) ListMCPRecommends() []mcpclient.RecommendInfo {
	return mcpclient.GetRecommends()
}

// StartMCPServer starts the MCP server (idempotent — ok if already running).
func (a *App) StartMCPServer() error {
	if a.mcpServer == nil {
		a.mcpServer = mcp.NewServer(a, "0.1.0")
	}
	port := a.getMCPPort()
	if port <= 0 {
		port = 19800
	}
	if err := a.mcpServer.Start(port); err != nil {
		return err
	}
	st := a.mcpServer.Status()
	log.Printf("[mcp] StartMCPServer → %s", st.URL)
	return nil
}

// StopMCPServer stops the MCP server.
func (a *App) StopMCPServer() error { return a.mcpServer.Stop() }

// getMCPPort returns the global MCP port, or default.
func (a *App) getMCPPort() int {
	if a.cfg.LLM.MCPPort > 0 {
		return a.cfg.LLM.MCPPort
	}
	return 19800
}

// SetMCPPort updates the global MCP port and restarts the server.
func (a *App) SetMCPPort(port int) error {
	a.cfg.LLM.MCPPort = port
	if err := config.Save(a.cfg); err != nil {
		return err
	}
	if a.mcpServer.IsRunning() {
		a.mcpServer.Stop()
	}
	return a.mcpServer.Start(port)
}
