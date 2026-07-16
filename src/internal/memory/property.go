package memory

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// EntityProperty is a row in kg_entity_properties — an assertion about an entity
// with optional temporal constraints ("entity X had property Y during [A,B]").
type EntityProperty struct {
	ID           string  `json:"id"`
	EntityID     string  `json:"entityId"`
	Property     string  `json:"property"`
	Value        string  `json:"value"`
	ValueType    string  `json:"valueType"`
	ValidFrom    int64   `json:"validFrom"` // 0 = unknown start
	ValidTo      int64   `json:"validTo"`   // 0 = currently valid
	Confidence   float64 `json:"confidence"`
	SourceType   string  `json:"sourceType,omitempty"`
	SourceChunkID string `json:"sourceChunkId,omitempty"`
	Evidence     string  `json:"evidence,omitempty"`
	RecordedAt   int64   `json:"recordedAt"`
}

// UpsertEntityProperty inserts or replaces an entity property.
// If an existing property with the same entity+property+time range exists, it merges
// (updates value, bumps confidence if same value).
func (s *Store) UpsertEntityProperty(ep EntityProperty) (string, error) {
	if ep.EntityID == "" || ep.Property == "" {
		return "", fmt.Errorf("entity_id and property are required")
	}
	if ep.ID == "" {
		ep.ID = "ep_" + uuid.NewString()
	}
	if ep.RecordedAt == 0 {
		ep.RecordedAt = time.Now().UnixMilli()
	}

	// Dedup: if same entity+property+valid_from exists, update instead of insert.
	var existingID string
	err := s.db.QueryRow(`SELECT id FROM kg_entity_properties
		WHERE entity_id = ? AND property = ? AND (valid_from = ? OR (valid_from IS NULL AND ? = 0))
		AND (valid_to IS NULL OR valid_to = ? OR (valid_to IS NULL AND ? = 0))
		LIMIT 1`,
		ep.EntityID, ep.Property,
		ep.ValidFrom, ep.ValidFrom,
		ep.ValidTo, ep.ValidTo,
	).Scan(&existingID)
	if err == nil && existingID != "" {
		// Update existing: bump confidence if values match, else replace.
		var oldValue string
		var oldConf float64
		if err := s.db.QueryRow(`SELECT value, confidence FROM kg_entity_properties WHERE id = ?`, existingID).Scan(&oldValue, &oldConf); err == nil {
			if oldValue == ep.Value {
				ep.Confidence = max(oldConf, ep.Confidence)
			}
		}
		_, err = s.db.Exec(`UPDATE kg_entity_properties SET value=?, value_type=?, confidence=?,
			valid_to=?, source_type=?, source_chunk_id=?, evidence=?, recorded_at=?
			WHERE id=?`,
			ep.Value, ep.ValueType, ep.Confidence, nullIfZero(ep.ValidTo),
			nullIfStr(ep.SourceType), nullIfStr(ep.SourceChunkID), nullIfStr(ep.Evidence),
			ep.RecordedAt, existingID)
		return existingID, err
	}

	_, err = s.db.Exec(`INSERT INTO kg_entity_properties
		(id, entity_id, property, value, value_type, valid_from, valid_to,
		 confidence, source_type, source_chunk_id, evidence, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ep.ID, ep.EntityID, ep.Property, ep.Value, ep.ValueType,
		nullIfZero(ep.ValidFrom), nullIfZero(ep.ValidTo),
		ep.Confidence, nullIfStr(ep.SourceType), nullIfStr(ep.SourceChunkID),
		nullIfStr(ep.Evidence), ep.RecordedAt)
	return ep.ID, err
}

// GetEntityProperties returns all currently-valid properties for an entity.
func (s *Store) GetEntityProperties(entityID string) ([]EntityProperty, error) {
	rows, err := s.db.Query(`SELECT id, entity_id, property, value, value_type,
		COALESCE(valid_from,0), COALESCE(valid_to,0), confidence,
		COALESCE(source_type,''), COALESCE(source_chunk_id,''), COALESCE(evidence,''), recorded_at
		FROM kg_entity_properties
		WHERE entity_id = ? AND (valid_to IS NULL OR valid_to = 0 OR valid_to > ?)
		ORDER BY property`, entityID, time.Now().UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityProperty
	for rows.Next() {
		var ep EntityProperty
		if err := rows.Scan(&ep.ID, &ep.EntityID, &ep.Property, &ep.Value, &ep.ValueType,
			&ep.ValidFrom, &ep.ValidTo, &ep.Confidence,
			&ep.SourceType, &ep.SourceChunkID, &ep.Evidence, &ep.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

// GetEntityPropertyHistory returns all properties (including expired) for an entity,
// ordered by valid_from descending.
func (s *Store) GetEntityPropertyHistory(entityID string, limit int) ([]EntityProperty, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`SELECT id, entity_id, property, value, value_type,
		COALESCE(valid_from,0), COALESCE(valid_to,0), confidence,
		COALESCE(source_type,''), COALESCE(source_chunk_id,''), COALESCE(evidence,''), recorded_at
		FROM kg_entity_properties
		WHERE entity_id = ?
		ORDER BY COALESCE(valid_from,0) DESC LIMIT ?`, entityID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EntityProperty
	for rows.Next() {
		var ep EntityProperty
		if err := rows.Scan(&ep.ID, &ep.EntityID, &ep.Property, &ep.Value, &ep.ValueType,
			&ep.ValidFrom, &ep.ValidTo, &ep.Confidence,
			&ep.SourceType, &ep.SourceChunkID, &ep.Evidence, &ep.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}

// DeleteEntityProperty removes a single entity property.
func (s *Store) DeleteEntityProperty(id string) error {
	_, err := s.db.Exec(`DELETE FROM kg_entity_properties WHERE id = ?`, id)
	return err
}

// CloseEntityProperty sets valid_to on a property, marking it as no longer current.
func (s *Store) CloseEntityProperty(id string) error {
	now := time.Now().UnixMilli()
	_, err := s.db.Exec(`UPDATE kg_entity_properties SET valid_to = ? WHERE id = ? AND valid_to IS NULL`, now, id)
	return err
}

func nullIfZero(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullIfStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
