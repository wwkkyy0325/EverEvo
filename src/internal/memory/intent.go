package memory

import (
	"sort"
	"time"
)

// ─── Intent Transition Graph (CID-GraphRAG style) ─────────────────────

// IntentNode represents a detected user intent in a conversation turn.
type IntentNode struct {
	ID        string  `json:"id"`
	Intent    string  `json:"intent"`    // "definition"|"factual"|"temporal"|"relational"|"spatial"|"conceptual"|"general"
	Query     string  `json:"query"`    // original user query
	SessionID string  `json:"sessionId"`
	TurnIndex int     `json:"turnIndex"`
	Confidence float64 `json:"confidence"`
	CreatedAt  int64  `json:"createdAt"`
}

// IntentTransition is a directed edge in the intent transition graph.
type IntentTransition struct {
	FromIntent string  `json:"fromIntent"`
	ToIntent   string  `json:"toIntent"`
	Count      int     `json:"count"`
	Probability float64 `json:"probability"`
}

// IntentGraph holds the full intent transition model learned from conversations.
type IntentGraph struct {
	Transitions []IntentTransition `json:"transitions"`
	NodeCounts  map[string]int     `json:"nodeCounts"` // intent → total occurrences
	TotalTurns  int                `json:"totalTurns"`
	UpdatedAt   int64              `json:"updatedAt"`
}

// ─── Intent Classification (rule-based, fast) ────────────────────────

// ClassifyQueryIntent performs fast rule-based intent classification.
// Returns the primary intent and confidence.
func ClassifyQueryIntent(query string) (intent string, confidence float64) {
	if query == "" {
		return "general", 0.5
	}

	signals := map[string]float64{
		"definition":  0.0,
		"factual":     0.0,
		"temporal":    0.0,
		"relational":  0.0,
		"spatial":     0.0,
		"conceptual":  0.0,
		"general":     0.1, // baseline
	}

	// Definition signals
	for _, kw := range []string{"什么是", "定义", "什么叫", "是指", "含义", "meaning of", "define", "what is"} {
		if contains(query, kw) {
			signals["definition"] += 0.25
		}
	}

	// Temporal signals
	for _, kw := range []string{"何时", "什么时候", "时间", "年份", "日期", "之前", "之后", "顺序", "when", "timeline", "before", "after"} {
		if contains(query, kw) {
			signals["temporal"] += 0.2
		}
	}

	// Spatial signals
	for _, kw := range []string{"哪里", "哪儿", "地点", "位置", "地址", "where", "located", "address"} {
		if contains(query, kw) {
			signals["spatial"] += 0.25
		}
	}

	// Relational signals
	for _, kw := range []string{"关系", "关联", "联系", "之间", "与", "和", "谁", "relation", "between", "connected"} {
		if contains(query, kw) {
			signals["relational"] += 0.2
		}
	}

	// Conceptual signals (high-level / thematic)
	for _, kw := range []string{"概述", "总结", "概括", "主题", "主要", "overview", "summary", "main", "theme"} {
		if contains(query, kw) {
			signals["conceptual"] += 0.25
		}
	}

	// Factual signals (specific details)
	for _, kw := range []string{"多少", "几个", "具体", "详细", "how many", "how much", "specifically", "detail"} {
		if contains(query, kw) {
			signals["factual"] += 0.25
		}
	}

	// Find highest-scoring intent
	bestIntent := "general"
	bestScore := signals["general"]
	for intent, score := range signals {
		if score > bestScore {
			bestScore = score
			bestIntent = intent
		}
	}

	// Normalize confidence
	conf := bestScore
	if conf > 1.0 {
		conf = 1.0
	}
	if conf < 0.3 {
		conf = 0.3
		bestIntent = "general"
	}

	return bestIntent, conf
}

// ─── Intent Transition Graph Building ────────────────────────────────

// BuildIntentGraph analyzes historical sessions to build an intent transition graph.
// This learns patterns like: definition queries often follow conceptual queries,
// temporal queries often precede relational queries, etc.
func (s *Store) BuildIntentGraph(minSessions int) (*IntentGraph, error) {
	graph := &IntentGraph{
		NodeCounts: make(map[string]int),
		UpdatedAt:  time.Now().UnixMilli(),
	}

	// Get recent sessions with messages
	sessions, err := s.getRecentSessions(minSessions)
	if err != nil {
		return nil, err
	}

	transitions := make(map[string]map[string]int) // fromIntent → toIntent → count

	for _, sess := range sessions {
		messages, _ := s.getSessionMessages(sess.ID, 100)
		var prevIntent string
		for _, msg := range messages {
			if msg.Role != "user" {
				continue
			}
			intent, _ := ClassifyQueryIntent(msg.Content)
			graph.NodeCounts[intent]++
			graph.TotalTurns++

			if prevIntent != "" {
				if transitions[prevIntent] == nil {
					transitions[prevIntent] = make(map[string]int)
				}
				transitions[prevIntent][intent]++
			}
			prevIntent = intent
		}
	}

	// Convert to probability-based transitions
	for from, toMap := range transitions {
		total := 0
		for _, count := range toMap {
			total += count
		}
		for to, count := range toMap {
			graph.Transitions = append(graph.Transitions, IntentTransition{
				FromIntent:  from,
				ToIntent:    to,
				Count:       count,
				Probability: float64(count) / float64(total),
			})
		}
	}

	// Sort by probability descending
	sort.Slice(graph.Transitions, func(i, j int) bool {
		return graph.Transitions[i].Probability > graph.Transitions[j].Probability
	})

	return graph, nil
}

// PredictNextIntent predicts the most likely next intent given the current intent
// and the learned transition graph.
func (g *IntentGraph) PredictNextIntent(currentIntent string) (string, float64) {
	if g == nil || len(g.Transitions) == 0 {
		return "general", 0.5
	}
	bestIntent := "general"
	bestProb := 0.0
	for _, t := range g.Transitions {
		if t.FromIntent == currentIntent && t.Probability > bestProb {
			bestProb = t.Probability
			bestIntent = t.ToIntent
		}
	}
	if bestProb < 0.1 {
		return "general", 0.5
	}
	return bestIntent, bestProb
}

// SuggestRetrievalStrategy returns the recommended retrieval strategy for an intent.
func SuggestRetrievalStrategy(intent string) []string {
	strategies := map[string][]string{
		"definition":  {"concept_tree", "wiki_sections", "rag_kb"},
		"factual":     {"rag_kb", "wiki_sections"},
		"temporal":    {"entity_timeline", "kg_events", "rag_kb"},
		"relational":  {"kg_graph", "entity_edges", "rag_kb"},
		"spatial":     {"kg_spatial", "wiki_sections"},
		"conceptual":  {"wiki_sections", "concept_tree", "community_summaries"},
		"general":     {"rag_kb", "wiki_sections", "kg_graph", "entity_properties"},
	}
	if s, ok := strategies[intent]; ok {
		return s
	}
	return strategies["general"]
}

// ─── Helpers ────────────────────────────────────────────────────────

func (s *Store) getRecentSessions(minCount int) ([]struct{ ID string }, error) {
	rows, err := s.db.Query(`SELECT id FROM sessions ORDER BY updated_at DESC LIMIT ?`, minCount*2)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct{ ID string }
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, struct{ ID string }{id})
	}
	if len(out) < minCount {
		return out, nil // return what we have
	}
	return out, rows.Err()
}

func (s *Store) getSessionMessages(sessionID string, limit int) ([]struct {
	Role    string
	Content string
}, error) {
	rows, err := s.db.Query(`SELECT role, content FROM messages
		WHERE session_id = ? ORDER BY seq LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []struct {
		Role    string
		Content string
	}
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			continue
		}
		out = append(out, struct {
			Role    string
			Content string
		}{role, content})
	}
	return out, rows.Err()
}

func contains(s, substr string) bool {
	// Case-insensitive contains
	lower := ""
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			lower += string(r + 32)
		} else {
			lower += string(r)
		}
	}
	subLower := ""
	for _, r := range substr {
		if r >= 'A' && r <= 'Z' {
			subLower += string(r + 32)
		} else {
			subLower += string(r)
		}
	}
	return len(lower) >= len(subLower) && containsStr(lower, subLower)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

