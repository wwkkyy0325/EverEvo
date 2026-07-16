// Package internal provides the audit logging implementation for security.
package internal

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"everevo/internal/storage"
)

// Entry is a record of a sensitive operation.
type Entry struct {
	Time      time.Time `json:"time"`
	Operation string    `json:"operation"`
	Target    string    `json:"target"`
	Allowed   bool      `json:"allowed"`
	Reason    string    `json:"reason,omitempty"`
}

func auditPath() string {
	return filepath.Join(storage.DataDir(), "audit.log")
}

// Log appends an audit entry to the log with rotation at 1 MB.
func Log(entry Entry) {
	entry.Time = time.Now()
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("[security] audit marshal: %v", err)
		return
	}

	ap := auditPath()

	if info, err := os.Stat(ap); err == nil && info.Size() > 1*1024*1024 {
		oldPath := ap + ".old"
		os.Remove(oldPath)
		os.Rename(ap, oldPath)
	}

	f, err := os.OpenFile(ap, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[security] audit open: %v", err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		log.Printf("[security] audit write: %v", err)
	}
}
