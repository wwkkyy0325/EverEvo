// Package taskboard implements a structured task board that persists across
// conversations via wiki. It auto-injects into the AI's system prompt so
// long-running tasks never get lost between conversations.
package taskboard

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Priority levels.
const (
	P0 = "P0" // critical / blocking
	P1 = "P1" // high
	P2 = "P2" // medium
	P3 = "P3" // low
)

// Status values.
const (
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusBlocked    = "blocked"
)

// Task represents one tracked task on the board.
type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Priority    string   `json:"priority"` // P0/P1/P2/P3
	Status      string   `json:"status"`   // pending/in_progress/done/blocked
	Progress    int      `json:"progress"` // 0-100
	DependsOn   []string `json:"dependsOn"`
	Steps       []Step   `json:"steps"`
	CreatedAt   string   `json:"createdAt"`
	UpdatedAt   string   `json:"updatedAt"`
	CompletedAt string   `json:"completedAt,omitempty"`
	Notes       string   `json:"notes,omitempty"`
}

// Step is a sub-task with a checkbox.
type Step struct {
	Text string `json:"text"`
	Done bool   `json:"done"`
}

// Board is the full task board.
type Board struct {
	Tasks     []Task `json:"tasks"`
	UpdatedAt string `json:"updatedAt"`
}

// SystemPrompt returns the task board rendered for injection into the AI's
// system prompt. Only pending/in_progress/blocked tasks are included.
func (b *Board) SystemPrompt() string {
	groups := map[string][]Task{}
	for _, t := range b.Tasks {
		if t.Status == StatusDone {
			continue
		}
		groups[t.Status] = append(groups[t.Status], t)
	}

	var sb strings.Builder
	sb.WriteString("## 📋 当前任务板\n\n")
	sb.WriteString("以下任务正在跨对话追踪中。完成后使用 taskboard 工具标记为 done。\n\n")

	order := []string{StatusInProgress, StatusBlocked, StatusPending}
	for _, st := range order {
		tasks := groups[st]
		if len(tasks) == 0 {
			continue
		}
		emoji := map[string]string{
			StatusInProgress: "🔴",
			StatusBlocked:    "🟠",
			StatusPending:    "🟡",
		}[st]

		sb.WriteString(fmt.Sprintf("### %s %s\n\n", emoji, statusLabel(st)))
		sort.Slice(tasks, func(i, j int) bool {
			pi := priorityOrder(tasks[i].Priority)
			pj := priorityOrder(tasks[j].Priority)
			if pi != pj {
				return pi < pj
			}
			return tasks[i].CreatedAt < tasks[j].CreatedAt
		})
		for _, t := range tasks {
			prog := ""
			if t.Progress > 0 && t.Progress < 100 {
				prog = fmt.Sprintf(" (%d%%)", t.Progress)
			}
			sb.WriteString(fmt.Sprintf("- **%s** [%s]%s\n", t.Title, t.Priority, prog))
			if t.Description != "" {
				sb.WriteString(fmt.Sprintf("  > %s\n", truncateDesc(t.Description, 120)))
			}
			if len(t.Steps) > 0 {
				for _, s := range t.Steps {
					check := " "
					if s.Done {
						check = "x"
					}
					sb.WriteString(fmt.Sprintf("  - [%s] %s\n", check, s.Text))
				}
			}
			if t.Notes != "" {
				sb.WriteString(fmt.Sprintf("  - 📝 %s\n", truncateDesc(t.Notes, 100)))
			}
		}
		sb.WriteString("\n")
	}

	// Recent completions (last 3)
	var done []Task
	for _, t := range b.Tasks {
		if t.Status == StatusDone {
			done = append(done, t)
		}
	}
	if len(done) > 0 {
		sort.Slice(done, func(i, j int) bool {
			return done[i].CompletedAt > done[j].CompletedAt
		})
		if len(done) > 3 {
			done = done[:3]
		}
		sb.WriteString("### 🟢 最近完成\n\n")
		for _, t := range done {
			sb.WriteString(fmt.Sprintf("- [x] %s\n", t.Title))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ToWiki generates full Markdown for saving to the wiki.
func (b *Board) ToWiki() string {
	var sb strings.Builder
	sb.WriteString("# EverEvo 任务板\n\n")
	sb.WriteString(fmt.Sprintf("> 最后更新: %s\n\n", b.UpdatedAt))

	groups := map[string][]Task{}
	for _, t := range b.Tasks {
		groups[t.Status] = append(groups[t.Status], t)
	}

	order := []string{StatusInProgress, StatusBlocked, StatusPending, StatusDone}
	for _, st := range order {
		tasks := groups[st]
		if len(tasks) == 0 {
			continue
		}
		emoji := map[string]string{
			StatusInProgress: "🔴",
			StatusBlocked:    "🟠",
			StatusPending:    "🟡",
			StatusDone:       "🟢",
		}[st]

		sb.WriteString(fmt.Sprintf("## %s %s\n\n", emoji, statusLabel(st)))

		sort.Slice(tasks, func(i, j int) bool {
			pi := priorityOrder(tasks[i].Priority)
			pj := priorityOrder(tasks[j].Priority)
			if pi != pj {
				return pi < pj
			}
			return tasks[i].CreatedAt < tasks[j].CreatedAt
		})

		for _, t := range tasks {
			sb.WriteString(fmt.Sprintf("### [%s] %s\n\n", t.Priority, t.Title))
			sb.WriteString(fmt.Sprintf("- **ID**: `%s`\n", t.ID))
			sb.WriteString(fmt.Sprintf("- **创建**: %s\n", t.CreatedAt))
			if t.CompletedAt != "" {
				sb.WriteString(fmt.Sprintf("- **完成**: %s\n", t.CompletedAt))
			}
			if len(t.DependsOn) > 0 {
				sb.WriteString(fmt.Sprintf("- **依赖**: %s\n", strings.Join(t.DependsOn, ", ")))
			}
			if t.Description != "" {
				sb.WriteString(fmt.Sprintf("\n%s\n", t.Description))
			}
			if len(t.Steps) > 0 {
				sb.WriteString("\n**步骤:**\n")
				for _, s := range t.Steps {
					check := " "
					if s.Done {
						check = "x"
					}
					sb.WriteString(fmt.Sprintf("- [%s] %s\n", check, s.Text))
				}
			}
			if t.Notes != "" {
				sb.WriteString(fmt.Sprintf("\n**备注:** %s\n", t.Notes))
			}
			sb.WriteString("\n---\n\n")
		}
	}

	return sb.String()
}

// NewBoard creates an empty board.
func NewBoard() *Board {
	return &Board{
		Tasks:     []Task{},
		UpdatedAt: time.Now().Format(time.RFC3339),
	}
}

// AddTask adds a new task.
func (b *Board) AddTask(t Task) {
	t.CreatedAt = time.Now().Format(time.RFC3339)
	t.UpdatedAt = t.CreatedAt
	b.Tasks = append(b.Tasks, t)
	b.UpdatedAt = time.Now().Format(time.RFC3339)
}

// UpdateTask merges fields from updated into the existing task with the same ID.
func (b *Board) UpdateTask(id string, updated Task) error {
	for i, t := range b.Tasks {
		if t.ID == id {
			if updated.Title != "" {
				b.Tasks[i].Title = updated.Title
			}
			if updated.Description != "" {
				b.Tasks[i].Description = updated.Description
			}
			if updated.Priority != "" {
				b.Tasks[i].Priority = updated.Priority
			}
			if updated.Status != "" {
				b.Tasks[i].Status = updated.Status
				if updated.Status == StatusDone && b.Tasks[i].CompletedAt == "" {
					b.Tasks[i].CompletedAt = time.Now().Format(time.RFC3339)
				}
			}
			if updated.Progress >= 0 {
				b.Tasks[i].Progress = updated.Progress
			}
			if updated.Notes != "" {
				if b.Tasks[i].Notes != "" {
					b.Tasks[i].Notes += "\n" + updated.Notes
				} else {
					b.Tasks[i].Notes = updated.Notes
				}
			}
			if len(updated.Steps) > 0 {
				b.Tasks[i].Steps = updated.Steps
			}
			if len(updated.DependsOn) > 0 {
				b.Tasks[i].DependsOn = updated.DependsOn
			}
			b.Tasks[i].UpdatedAt = time.Now().Format(time.RFC3339)
			b.UpdatedAt = time.Now().Format(time.RFC3339)
			return nil
		}
	}
	return fmt.Errorf("task %q not found", id)
}

// GetTask returns a task by ID.
func (b *Board) GetTask(id string) (*Task, error) {
	for _, t := range b.Tasks {
		if t.ID == id {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("task %q not found", id)
}

// ParseJSON parses board JSON.
func ParseJSON(data []byte) (*Board, error) {
	var b Board
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// ToJSON serializes the board to JSON.
func (b *Board) ToJSON() ([]byte, error) {
	b.UpdatedAt = time.Now().Format(time.RFC3339)
	return json.MarshalIndent(b, "", "  ")
}

// ─── helpers ──────────────────────────────────────────────────────

func statusLabel(s string) string {
	switch s {
	case StatusInProgress:
		return "进行中"
	case StatusBlocked:
		return "阻塞"
	case StatusPending:
		return "待开始"
	case StatusDone:
		return "已完成"
	}
	return s
}

func priorityOrder(p string) int {
	switch p {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	case "P3":
		return 3
	}
	return 99
}

func truncateDesc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
