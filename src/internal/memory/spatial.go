package memory

import (
	"time"

	"github.com/google/uuid"
)

// SpatialRecord is a row in kg_spatial — a geographic/spatial attribute.
type SpatialRecord struct {
	ID            string `json:"id"`
	EntityID      string `json:"entityId,omitempty"`
	EventID       string `json:"eventId,omitempty"`
	SpatialType   string `json:"spatialType"` // "point"|"address"|"region"|"named_location"
	Coordinates   string `json:"coordinates,omitempty"`
	Address       string `json:"address,omitempty"`
	Region        string `json:"region,omitempty"`
	NamedLocation string `json:"namedLocation,omitempty"`
	ValidFrom     int64  `json:"validFrom"`
	ValidTo       int64  `json:"validTo"`
	Confidence    float64 `json:"confidence"`
	SourceChunkID string `json:"sourceChunkId,omitempty"`
	RecordedAt    int64  `json:"recordedAt"`
}

// UpsertSpatial inserts or replaces a spatial record.
func (s *Store) UpsertSpatial(sr SpatialRecord) (string, error) {
	if sr.EntityID == "" && sr.EventID == "" {
		return "", Err("entity_id or event_id is required")
	}
	if sr.ID == "" {
		sr.ID = "sp_" + uuid.NewString()
	}
	if sr.RecordedAt == 0 {
		sr.RecordedAt = time.Now().UnixMilli()
	}

	_, err := s.db.Exec(`INSERT OR REPLACE INTO kg_spatial
		(id, entity_id, event_id, spatial_type, coordinates, address, region, named_location,
		 valid_from, valid_to, confidence, source_chunk_id, recorded_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sr.ID, nullIfStr(sr.EntityID), nullIfStr(sr.EventID), sr.SpatialType,
		nullIfStr(sr.Coordinates), nullIfStr(sr.Address), nullIfStr(sr.Region), nullIfStr(sr.NamedLocation),
		nullIfZero(sr.ValidFrom), nullIfZero(sr.ValidTo),
		sr.Confidence, nullIfStr(sr.SourceChunkID), sr.RecordedAt)
	return sr.ID, err
}

// GetSpatialForEntity returns all spatial records for an entity.
func (s *Store) GetSpatialForEntity(entityID string) ([]SpatialRecord, error) {
	return s.querySpatial("entity_id = ?", entityID)
}

// GetSpatialForEvent returns all spatial records for an event.
func (s *Store) GetSpatialForEvent(eventID string) ([]SpatialRecord, error) {
	return s.querySpatial("event_id = ?", eventID)
}

// GetSpatialInRegion returns spatial records matching a region name (LIKE search).
func (s *Store) GetSpatialInRegion(region string, limit int) ([]SpatialRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.querySpatial("region LIKE ? OR named_location LIKE ? LIMIT ?",
		"%"+region+"%", "%"+region+"%", limit)
}

func (s *Store) querySpatial(where string, args ...any) ([]SpatialRecord, error) {
	rows, err := s.db.Query(`SELECT id, COALESCE(entity_id,''), COALESCE(event_id,''),
		spatial_type, COALESCE(coordinates,''), COALESCE(address,''),
		COALESCE(region,''), COALESCE(named_location,''),
		COALESCE(valid_from,0), COALESCE(valid_to,0),
		confidence, COALESCE(source_chunk_id,''), recorded_at
		FROM kg_spatial WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SpatialRecord
	for rows.Next() {
		var sr SpatialRecord
		if err := rows.Scan(&sr.ID, &sr.EntityID, &sr.EventID,
			&sr.SpatialType, &sr.Coordinates, &sr.Address,
			&sr.Region, &sr.NamedLocation,
			&sr.ValidFrom, &sr.ValidTo,
			&sr.Confidence, &sr.SourceChunkID, &sr.RecordedAt); err != nil {
			return nil, err
		}
		out = append(out, sr)
	}
	return out, rows.Err()
}

func Err(msg string) error { return &strErr{msg} }

type strErr struct{ msg string }

func (e *strErr) Error() string { return e.msg }
