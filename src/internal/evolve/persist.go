package evolve

import (
	"encoding/json"
	"os"
	"path/filepath"

	"everevo/internal/storage"
)

// TaskFile returns the path to the evolve tasks JSON file.
func TaskFile() string {
	dir, err := storage.AppDataDir()
	if err != nil {
		exePath, _ := os.Executable()
		return filepath.Join(filepath.Dir(exePath), "data", "evolve_tasks.json")
	}
	return filepath.Join(dir, "evolve_tasks.json")
}

// LoadTasks reads all evolve tasks from disk.
func LoadTasks() ([]Task, error) {
	data, err := os.ReadFile(TaskFile())
	if err != nil {
		return nil, err
	}
	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// SaveTask upserts a task into the persisted list and writes to disk.
func SaveTask(task Task) error {
	tasks, err := LoadTasks()
	if err != nil {
		tasks = []Task{}
	}
	found := false
	for i, t := range tasks {
		if t.ID == task.ID {
			tasks[i] = task
			found = true
			break
		}
	}
	if !found {
		tasks = append(tasks, task)
	}
	return persistTasks(tasks)
}

// PersistTasks writes the full task list to disk.
func PersistTasks(tasks []Task) error {
	return persistTasks(tasks)
}

func persistTasks(tasks []Task) error {
	data, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	taskFile := TaskFile()
	if err := os.MkdirAll(filepath.Dir(taskFile), 0755); err != nil {
		return err
	}
	return os.WriteFile(taskFile, data, 0644)
}

// RestartMarkerPath returns the path to the restart marker file.
func RestartMarkerPath() string {
	exePath, _ := os.Executable()
	return filepath.Join(filepath.Dir(exePath), "data", "restart_marker.json")
}

// WriteRestartMarker writes a restart marker before exit.
func WriteRestartMarker(taskID string) error {
	m := RestartMarker{
		TaskID:    taskID,
		Timestamp: "now", // caller sets real timestamp
		Reason:    "evolve_swap",
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	path := RestartMarkerPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	return os.WriteFile(path, data, 0644)
}

// ReadRestartMarker reads and clears the restart marker.
func ReadRestartMarker() *RestartMarker {
	path := RestartMarkerPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m RestartMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	_ = os.Remove(path)
	return &m
}
