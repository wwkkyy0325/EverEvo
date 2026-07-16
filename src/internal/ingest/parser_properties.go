//go:build windows

package ingest

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ── Property extraction output types ────────────────────────────────────

// ExtractedProperties holds the result of LLM property/event/spatial extraction
// from a document or text segment.
type ExtractedProperties struct {
	EntityProperties []ExtractedEntityProp `json:"entity_properties"`
	SpatialInfo      []ExtractedSpatial    `json:"spatial_info"`
	Events           []ExtractedEvent      `json:"events"`
	Relations        []ExtractedRelation   `json:"relations,omitempty"`
}

// ExtractedEntityProp is a temporal property assertion about an entity.
type ExtractedEntityProp struct {
	EntityName         string  `json:"entity_name"`
	EntityType         string  `json:"entity_type"` // "person"|"organization"|"location"|"concept"|...
	Property           string  `json:"property"`     // "position"|"company"|"age"|"amount"|...
	Value              string  `json:"value"`
	ValueType          string  `json:"value_type"` // "string"|"number"|"date_range"|"boolean"
	ValidFrom          string  `json:"valid_from,omitempty"`  // "2020-01-01" or ""
	ValidTo            string  `json:"valid_to,omitempty"`    // "2023-06-30" or ""
	TemporalExpression string  `json:"temporal_expression,omitempty"`
	Confidence         float64 `json:"confidence"`
	Evidence           string  `json:"evidence"`
}

// ExtractedSpatial is a geographic/spatial attribute.
type ExtractedSpatial struct {
	EntityName    string  `json:"entity_name"`
	EventTitle    string  `json:"event_title,omitempty"`
	SpatialType   string  `json:"spatial_type"` // "point"|"address"|"region"|"named_location"
	Coordinates   string  `json:"coordinates,omitempty"`
	Address       string  `json:"address,omitempty"`
	Region        string  `json:"region,omitempty"`
	NamedLocation string  `json:"named_location,omitempty"`
	ValidFrom     string  `json:"valid_from,omitempty"`
	ValidTo       string  `json:"valid_to,omitempty"`
	Confidence    float64 `json:"confidence"`
	Evidence      string  `json:"evidence"`
}

// ExtractedEvent is a temporal event extracted from narrative or legal text.
type ExtractedEvent struct {
	ID              string             `json:"id,omitempty"`
	Title           string             `json:"title"`
	Description     string             `json:"description,omitempty"`
	EventType       string             `json:"event_type"` // "legal_action"|"plot_event"|"historical_event"|"transaction"
	TimeStart       string             `json:"time_start,omitempty"` // "2023-03-15" or ""
	TimeEnd         string             `json:"time_end,omitempty"`
	TimeExpression  string             `json:"time_expression,omitempty"`
	Duration        string             `json:"duration,omitempty"` // "3 days" / "P3D"
	TimelineOrder   int                `json:"timeline_order"`
	Participants    []EventParticipant `json:"participants,omitempty"`
	Confidence      float64            `json:"confidence"`
	Evidence        string             `json:"evidence"`
}

// EventParticipant is an entity's role in an event.
type EventParticipant struct {
	EntityName string `json:"entity_name"`
	Role       string `json:"role"` // "买方"|"卖方"|"protagonist"|"witness"|...
}

// ExtractedRelation is a subject-predicate-object triple.
type ExtractedRelation struct {
	Subject    string   `json:"subject"`
	Predicate  string   `json:"predicate"`
	Object     string   `json:"object"`
	Replaces   bool     `json:"replaces"`
	Polarity   string   `json:"polarity,omitempty"` // "positive"|"negative"|"neutral"
	Confidence float64  `json:"confidence"`
	Evidence   string   `json:"evidence"`
	Domains    []string `json:"domains,omitempty"`
}

// ── Prompt templates ────────────────────────────────────────────────────

const propertyExtractionSystem = `You are a knowledge graph extraction engine. Extract entities, their temporal properties,
spatial information, events, and relationships from the text.

Rules:
1. For each entity, capture changing properties over TIME — if "张三 was CEO from 2020 to 2023", extract that as a temporal property.
2. Record evidence snippets verbatim for provenance.
3. Give confidence scores: 1.0 = explicitly stated, 0.8 = clearly implied, 0.6 = inferred, <0.5 = speculative.
4. For events, determine their chronological order (timeline_order) and extract time expressions.
5. Capture spatial info: addresses, regions, named locations with their entity associations.
6. Output ONLY valid JSON matching the schema. No markdown fences, no commentary.`

const propertyExtractionSchema = `
Output JSON schema:
{
  "entity_properties": [
    {
      "entity_name": "张三",
      "entity_type": "person",
      "property": "position",
      "value": "总经理",
      "value_type": "string",
      "valid_from": "2020-01-01",
      "valid_to": "2023-06-30",
      "temporal_expression": "2020年1月至2023年6月",
      "confidence": 0.95,
      "evidence": "张三于2020年1月出任公司总经理，2023年6月离职"
    }
  ],
  "spatial_info": [
    {
      "entity_name": "北京总部",
      "spatial_type": "address",
      "address": "北京市朝阳区建国路100号",
      "confidence": 0.95,
      "evidence": "公司注册地址为北京市朝阳区建国路100号"
    }
  ],
  "events": [
    {
      "title": "合同签订",
      "description": "甲乙双方签订买卖合同",
      "event_type": "legal_action",
      "time_start": "2023-03-15",
      "time_expression": "2023年3月15日",
      "timeline_order": 1,
      "participants": [
        {"entity_name": "甲公司", "role": "买方"},
        {"entity_name": "乙公司", "role": "卖方"}
      ],
      "confidence": 0.98,
      "evidence": "双方于2023年3月15日签订本合同"
    }
  ],
  "relations": [
    {
      "subject": "张三",
      "predicate": "任职于",
      "object": "甲公司",
      "replaces": true,
      "polarity": "positive",
      "confidence": 0.95,
      "evidence": "张三现任甲公司总经理"
    }
  ]
}`

// ── Extractor ───────────────────────────────────────────────────────────

// ExtractProperties runs LLM-based property/event/spatial extraction on text.
// caller is the LLM invocation function (wired by app layer).
func ExtractProperties(text string, caller LLMCaller) (*ExtractedProperties, error) {
	if caller == nil {
		return nil, fmt.Errorf("ingest: LLM caller not configured for property extraction")
	}
	if len(strings.TrimSpace(text)) == 0 {
		return nil, fmt.Errorf("ingest: empty text for property extraction")
	}

	userPrompt := fmt.Sprintf("Extract entity properties, spatial info, events, and relations:\n\n```\n%s\n```", truncateForLLM(text, 16000))

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		response, err := caller(propertyExtractionSystem, userPrompt)
		if err != nil {
			lastErr = fmt.Errorf("LLM call failed (attempt %d): %w", attempt+1, err)
			continue
		}

		response = stripMarkdownFences(response)

		var ep ExtractedProperties
		if err := json.Unmarshal([]byte(response), &ep); err != nil {
			lastErr = fmt.Errorf("JSON parse failed (attempt %d): %w\nResponse: %.500s", attempt+1, err, response)
			userPrompt = fmt.Sprintf("Extract entity properties, events, and relations.\nPrevious response was invalid JSON: %v\n\n```\n%s\n```", err, truncateForLLM(text, 16000))
			continue
		}

		// Quick validation: should have at least something
		if len(ep.EntityProperties) == 0 && len(ep.Events) == 0 && len(ep.Relations) == 0 {
			// Not necessarily an error — some text has no extractable properties
			return &ep, nil
		}

		return &ep, nil
	}

	return nil, fmt.Errorf("property extraction failed after %d attempts: %w", maxRetries+1, lastErr)
}
