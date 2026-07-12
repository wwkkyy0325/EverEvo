package collab

import (
	"sync"
	"time"
)

// Entry kinds classify blackboard content for rendering and filtering.
const (
	KindText     = "text"
	KindArtifact = "artifact"
	KindDecision = "decision"
	KindTodo     = "todo"
)

// Entry is one key on a blackboard.
type Entry struct {
	Key       string    `json:"key"`
	Value     string    `json:"value"`
	Author    string    `json:"author"`    // agentID that wrote it
	Kind      string    `json:"kind"`      // text | artifact | decision | todo
	UpdatedAt time.Time `json:"updatedAt"`
}

// maxEntriesPerBoard caps a single blackboard to prevent runaway growth from
// LLM-authored writes.
const maxEntriesPerBoard = 256

// PersistFn is called by the blackboard after every mutation so the host app
// can mirror entries to durable storage (SQLite). nil means persistence is off.
type PersistFn func(boardID, key, value, author, kind string, updatedAt time.Time)

// Blackboard is an in-memory, multi-writer scratchpad shared by the members of
// a collaboration session. Writes publish an event on the bus so subscribed
// agents can react, and optionally persist to SQLite via PersistFn.
type Blackboard struct {
	ID        string
	mu        sync.RWMutex
	entries   map[string]Entry
	bus       *EventBus
	persistFn PersistFn
}

// NewBlackboard creates a blackboard wired to publish change events.
func NewBlackboard(id string, bus *EventBus) *Blackboard {
	return &Blackboard{
		ID:      id,
		entries: map[string]Entry{},
		bus:     bus,
	}
}

// topic returns the bus topic for this board's change events.
func (bb *Blackboard) topic() string { return "blackboard." + bb.ID + ".changed" }

// Set writes or replaces a key. Returns false if the board is full and the key
// is new (existing keys can always be updated).
func (bb *Blackboard) Set(key, value, author, kind string) bool {
	if kind == "" {
		kind = KindText
	}
	bb.mu.Lock()
	_, exists := bb.entries[key]
	if !exists && len(bb.entries) >= maxEntriesPerBoard {
		bb.mu.Unlock()
		return false
	}
	e := Entry{Key: key, Value: value, Author: author, Kind: kind, UpdatedAt: time.Now()}
	now := e.UpdatedAt
	bb.entries[key] = e
	bb.mu.Unlock()

	if bb.persistFn != nil {
		bb.persistFn(bb.ID, key, value, author, kind, now)
	}
	bb.bus.Publish(bb.topic(), Event{Source: author, Type: "set", Payload: e})
	return true
}

// Get returns a key's entry and whether it existed.
func (bb *Blackboard) Get(key string) (Entry, bool) {
	bb.mu.RLock()
	defer bb.mu.RUnlock()
	e, ok := bb.entries[key]
	return e, ok
}

// Update applies a function to an existing entry's value (no-op if absent).
func (bb *Blackboard) Update(key string, fn func(string) string, author string) bool {
	bb.mu.Lock()
	e, ok := bb.entries[key]
	if !ok {
		bb.mu.Unlock()
		return false
	}
	e.Value = fn(e.Value)
	e.Author = author
	e.UpdatedAt = time.Now()
	now := e.UpdatedAt
	bb.entries[key] = e
	bb.mu.Unlock()

	if bb.persistFn != nil {
		bb.persistFn(bb.ID, e.Key, e.Value, author, e.Kind, now)
	}
	bb.bus.Publish(bb.topic(), Event{Source: author, Type: "update", Payload: e})
	return true
}

// Delete removes a key.
func (bb *Blackboard) Delete(key string, author string) bool {
	bb.mu.Lock()
	_, ok := bb.entries[key]
	if ok {
		delete(bb.entries, key)
	}
	bb.mu.Unlock()
	if ok {
		if bb.persistFn != nil {
			// PersistFn doesn't have a "delete" semantic, so we tell host to
			// delete by calling with empty value and kind "".
			bb.persistFn(bb.ID, key, "", author, "", time.Now())
		}
		bb.bus.Publish(bb.topic(), Event{Source: author, Type: "delete", Payload: map[string]string{"key": key}})
	}
	return ok
}

// List returns a snapshot of all entries (sorted by caller if needed).
func (bb *Blackboard) List() []Entry {
	bb.mu.RLock()
	defer bb.mu.RUnlock()
	out := make([]Entry, 0, len(bb.entries))
	for _, e := range bb.entries {
		out = append(out, e)
	}
	return out
}

// Size returns the entry count.
func (bb *Blackboard) Size() int {
	bb.mu.RLock()
	defer bb.mu.RUnlock()
	return len(bb.entries)
}

// SetPersistFn wires a callback that the host app uses to mirror entries to
// durable storage (e.g. SQLite). Call with nil to disable persistence.
func (bb *Blackboard) SetPersistFn(fn PersistFn) {
	bb.mu.Lock()
	bb.persistFn = fn
	bb.mu.Unlock()
}

// LoadEntries bulk-loads entries from durable storage (e.g. on session
// restore). Existing in-memory entries are replaced.
func (bb *Blackboard) LoadEntries(entries []Entry) {
	bb.mu.Lock()
	bb.entries = make(map[string]Entry, len(entries))
	for _, e := range entries {
		bb.entries[e.Key] = e
	}
	bb.mu.Unlock()
}
