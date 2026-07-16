//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"everevo/internal/storage"
	"everevo/internal/taskboard"
)

// ─── Task Board — cross-conversation progress tracking ──────────

// Persistence strategy:
//   1. Primary: JSON file at data/taskboard.json (fast, reliable)
//   2. Backup: wiki page "taskboard" (survives data directory resets)
//
// The board is auto-loaded on startup and persisted on every mutation.

// injectTaskBoard appends the task board prompt to the system message inside
// messagesJSON. Called by chat entry points (ChatProxy, ChatStream, ChatStreamAs)
// so the agent sees current progress across all conversations.
func (a *App) injectTaskBoard(messagesJSON json.RawMessage) json.RawMessage {
	prompt := a.GetTaskBoardPrompt()
	if prompt == "" {
		return messagesJSON
	}
	var msgs []map[string]any
	if err := json.Unmarshal(messagesJSON, &msgs); err != nil {
		return messagesJSON
	}
	for i, m := range msgs {
		if role, _ := m["role"].(string); role == "system" {
			if content, ok := m["content"].(string); ok {
				msgs[i]["content"] = content + "\n\n" + prompt
			}
			break
		}
	}
	result, _ := json.Marshal(msgs)
	return json.RawMessage(result)
}

// loadTaskBoard loads the board from JSON; falls back to wiki; creates empty if neither exists.
func (a *App) loadTaskBoard() {
	a.taskBoard = taskboard.NewBoard()
	a.fileCtl.init()

	dataDir, err := storage.AppDataDir()
	if err != nil {
		log.Printf("[taskboard] ⚠ 无法获取 data 目录: %v", err)
		return
	}
	jsonPath := filepath.Join(dataDir, "taskboard.json")

	data, err := os.ReadFile(jsonPath)
	if err == nil {
		if board, parseErr := taskboard.ParseJSON(data); parseErr == nil {
			a.taskBoard = board
			log.Printf("[taskboard] ✓ 从 %s 加载 %d 个任务", jsonPath, len(board.Tasks))
			return
		} else {
			log.Printf("[taskboard] ⚠ JSON 解析失败: %v，尝试 wiki 恢复", parseErr)
		}
	}

	// If JSON load failed, try wiki as fallback (read-only, markdown not reversable).
	// For now, just start fresh - wiki is backup, not primary.
	log.Printf("[taskboard] 新建空任务板 (%s)", jsonPath)
}

// saveTaskBoard persists the board to JSON and wiki backup.
func (a *App) saveTaskBoard() {
	if a.taskBoard == nil {
		return
	}
	// 1. JSON persistence (fast, primary)
	dataDir, err := storage.AppDataDir()
	if err != nil {
		log.Printf("[taskboard] ⚠ save: %v", err)
		return
	}
	jsonPath := filepath.Join(dataDir, "taskboard.json")
	data, err := a.taskBoard.ToJSON()
	if err != nil {
		log.Printf("[taskboard] ⚠ marshal: %v", err)
		return
	}
	if err := os.WriteFile(jsonPath, data, 0644); err != nil {
		log.Printf("[taskboard] ⚠ write: %v", err)
		return
	}

	// 2. Wiki backup (survives data directory resets; human-readable)
	content := a.taskBoard.ToWiki()
	libID := a.resolveLibraryID("")
	if libID != "" {
		if saveErr := a.WikiSavePage(libID, "taskboard", "EverEvo 任务板", content); saveErr != nil {
			// Non-critical: JSON is the primary store
			log.Printf("[taskboard] ⚠ wiki 备份失败: %v", saveErr)
		}
	}
}

// ─── Wails bindings ──────────────────────────────────────────────

// AddTask adds a new task to the board and persists.
func (a *App) AddTask(title, description, priority string, steps []taskboard.Step, dependsOn []string) map[string]any {
	if a.taskBoard == nil {
		return map[string]any{"ok": false, "error": "任务板未初始化"}
	}
	id := fmt.Sprintf("t_%d", time.Now().UnixMilli())
	task := taskboard.Task{
		ID:          id,
		Title:       title,
		Description: description,
		Priority:    priority,
		Status:      taskboard.StatusPending,
		Steps:       steps,
		DependsOn:   dependsOn,
	}
	a.taskBoard.AddTask(task)
	a.saveTaskBoard()

	t, _ := a.taskBoard.GetTask(id)
	log.Printf("[taskboard] ✓ 新增任务: %s [%s]", title, priority)
	return map[string]any{"ok": true, "task": t}
}

// UpdateTaskStatus updates a task's status and progress, then persists.
func (a *App) UpdateTaskStatus(id, status string, progress int, notes string) map[string]any {
	if a.taskBoard == nil {
		return map[string]any{"ok": false, "error": "任务板未初始化"}
	}
	err := a.taskBoard.UpdateTask(id, taskboard.Task{
		Status:   status,
		Progress: progress,
		Notes:    notes,
	})
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	a.saveTaskBoard()

	t, _ := a.taskBoard.GetTask(id)
	log.Printf("[taskboard] 更新任务 %s → %s (%d%%)", id, status, progress)
	return map[string]any{"ok": true, "task": t}
}

// UpdateTaskSteps updates the steps array for a task.
func (a *App) UpdateTaskSteps(id string, steps []taskboard.Step) map[string]any {
	if a.taskBoard == nil {
		return map[string]any{"ok": false, "error": "任务板未初始化"}
	}
	err := a.taskBoard.UpdateTask(id, taskboard.Task{Steps: steps})
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	a.saveTaskBoard()

	t, _ := a.taskBoard.GetTask(id)
	return map[string]any{"ok": true, "task": t}
}

// ListTasks returns all tasks.
func (a *App) ListTasks() []taskboard.Task {
	if a.taskBoard == nil {
		return nil
	}
	return a.taskBoard.Tasks
}

// GetTask returns a single task by ID.
func (a *App) GetTask(id string) map[string]any {
	if a.taskBoard == nil {
		return map[string]any{"ok": false, "error": "任务板未初始化"}
	}
	t, err := a.taskBoard.GetTask(id)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	return map[string]any{"ok": true, "task": t}
}

// GetTaskBoardPrompt returns the board rendered for system prompt injection.
func (a *App) GetTaskBoardPrompt() string {
	if a.taskBoard == nil {
		return ""
	}
	return a.taskBoard.SystemPrompt()
}

// ─── JSON import/export ──────────────────────────────────────────

// ExportTaskBoard returns the board as JSON string.
func (a *App) ExportTaskBoard() string {
	if a.taskBoard == nil {
		return "{}"
	}
	data, _ := a.taskBoard.ToJSON()
	return string(data)
}

// ImportTaskBoard replaces the board with the given JSON string.
func (a *App) ImportTaskBoard(jsonStr string) map[string]any {
	board, err := taskboard.ParseJSON([]byte(jsonStr))
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	a.taskBoard = board
	a.saveTaskBoard()
	log.Printf("[taskboard] ✓ 导入 %d 个任务", len(board.Tasks))
	return map[string]any{"ok": true, "count": len(board.Tasks)}
}
