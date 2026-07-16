package memory

import (
	"time"

	"github.com/google/uuid"
)

// KGEvent is a row in kg_events — an extracted event with temporal ordering.
type KGEvent struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Description    string  `json:"description,omitempty"`
	EventType      string  `json:"eventType,omitempty"` // "legal_action"|"plot_event"|"historical_event"
	TimeStart      int64   `json:"timeStart"`            // Unix ms, 0 = unknown
	TimeEnd        int64   `json:"timeEnd"`              // Unix ms, 0 = instantaneous
	TimeExpression string  `json:"timeExpression,omitempty"`
	TimelineOrder  int     `json:"timelineOrder"`
	Duration       string  `json:"duration,omitempty"` // ISO 8601: "P3D"
	Confidence     float64 `json:"confidence"`
	SourceChunkID  string  `json:"sourceChunkId,omitempty"`
	CreatedAt      int64   `json:"createdAt"`
}

// UpsertEvent inserts or replaces an event.
func (s *Store) UpsertEvent(ev KGEvent) (string, error) {
	if ev.Title == "" {
		return "", Err("event title is required")
	}
	if ev.ID == "" {
		ev.ID = "ev_" + uuid.NewString()
	}
	if ev.CreatedAt == 0 {
		ev.CreatedAt = time.Now().UnixMilli()
	}

	_, err := s.db.Exec(`INSERT OR REPLACE INTO kg_events
		(id, title, description, event_type, time_start, time_end,
		 time_expression, timeline_order, duration, confidence, source_chunk_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.Title, nullIfStr(ev.Description), nullIfStr(ev.EventType),
		nullIfZero(ev.TimeStart), nullIfZero(ev.TimeEnd),
		nullIfStr(ev.TimeExpression), ev.TimelineOrder, nullIfStr(ev.Duration),
		ev.Confidence, nullIfStr(ev.SourceChunkID), ev.CreatedAt)
	return ev.ID, err
}

// GetEventsForSource returns events ordered by timeline_order for a source chunk.
func (s *Store) GetEventsForSource(sourceChunkID string) ([]KGEvent, error) {
	return s.queryEvents("source_chunk_id = ? ORDER BY timeline_order", sourceChunkID)
}

// GetEventsForEntity returns events the entity participates in, ordered by time.
// Uses the kg_participation join (future: kg_edges with participates_in predicate).
func (s *Store) GetEventsForEntity(entityID string, limit int) ([]KGEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT DISTINCT e.id, e.title, COALESCE(e.description,''),
		COALESCE(e.event_type,''), COALESCE(e.time_start,0), COALESCE(e.time_end,0),
		COALESCE(e.time_expression,''), e.timeline_order, COALESCE(e.duration,''),
		e.confidence, COALESCE(e.source_chunk_id,''), e.created_at
		FROM kg_events e
		JOIN kg_edges ed ON (ed.dst_id = e.id AND ed.type = 'participates_in')
		WHERE ed.src_id = ? AND ed.valid_to IS NULL
		ORDER BY e.timeline_order, e.time_start LIMIT ?`, entityID, limit)
	if err != nil {
		// Fallback: no participation edges yet — search by entity name in title/desc.
		return s.queryEvents("title LIKE (SELECT '%' || COALESCE(name_raw,name) || '%' FROM kg_nodes WHERE id = ? LIMIT 1) ORDER BY timeline_order, time_start LIMIT ?", entityID, limit)
	}
	defer rows.Close()
	var out []KGEvent
	for rows.Next() {
		var ev KGEvent
		if err := rows.Scan(&ev.ID, &ev.Title, &ev.Description, &ev.EventType,
			&ev.TimeStart, &ev.TimeEnd, &ev.TimeExpression, &ev.TimelineOrder,
			&ev.Duration, &ev.Confidence, &ev.SourceChunkID, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// QueryEventTimeline returns events ordered by timeline_order (optionally filtered by entity).
func (s *Store) QueryEventTimeline(entityID string, from, to int, limit int) ([]KGEvent, error) {
	if limit <= 0 {
		limit = 20
	}
	where := "1=1"
	args := []any{}
	if entityID != "" {
		where = "id IN (SELECT e.id FROM kg_events e JOIN kg_edges ed ON ed.dst_id=e.id WHERE ed.src_id=? AND ed.type='participates_in' AND ed.valid_to IS NULL)"
		args = append(args, entityID)
	}
	if from > 0 {
		where += " AND timeline_order >= ?"
		args = append(args, from)
	}
	if to > 0 {
		where += " AND timeline_order <= ?"
		args = append(args, to)
	}
	args = append(args, limit)
	return s.queryEvents(where+" ORDER BY timeline_order, COALESCE(time_start,0) LIMIT ?", args...)
}

// GetEvent returns a single event by ID.
func (s *Store) GetEvent(id string) (*KGEvent, error) {
	var ev KGEvent
	err := s.db.QueryRow(`SELECT id, title, COALESCE(description,''),
		COALESCE(event_type,''), COALESCE(time_start,0), COALESCE(time_end,0),
		COALESCE(time_expression,''), timeline_order, COALESCE(duration,''),
		confidence, COALESCE(source_chunk_id,''), created_at
		FROM kg_events WHERE id = ?`, id).Scan(
		&ev.ID, &ev.Title, &ev.Description, &ev.EventType,
		&ev.TimeStart, &ev.TimeEnd, &ev.TimeExpression, &ev.TimelineOrder,
		&ev.Duration, &ev.Confidence, &ev.SourceChunkID, &ev.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

func (s *Store) queryEvents(where string, args ...any) ([]KGEvent, error) {
	rows, err := s.db.Query(`SELECT id, title, COALESCE(description,''),
		COALESCE(event_type,''), COALESCE(time_start,0), COALESCE(time_end,0),
		COALESCE(time_expression,''), timeline_order, COALESCE(duration,''),
		confidence, COALESCE(source_chunk_id,''), created_at
		FROM kg_events WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KGEvent
	for rows.Next() {
		var ev KGEvent
		if err := rows.Scan(&ev.ID, &ev.Title, &ev.Description, &ev.EventType,
			&ev.TimeStart, &ev.TimeEnd, &ev.TimeExpression, &ev.TimelineOrder,
			&ev.Duration, &ev.Confidence, &ev.SourceChunkID, &ev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}
