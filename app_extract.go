//go:build windows

package main

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"everevo/internal/rag"
)

// Unified extraction scheduler — single entry point from MemoryMessageAppend.
// All periodic jobs (facts, graph, summarize, reflect, link) fire from here
// based on the same counter (total user messages across all sessions).

const (
	scheduleFacts      = 5
	scheduleSummarize  = 10
	scheduleReflect    = 20
	scheduleLink       = 50
)

// scheduler is called on every user message. It fires extraction jobs at
// configurable intervals, all driven by the same counter.
func (a *App) scheduler() {
	if a.memoryStore == nil {
		return
	}
	totalUserMsgs := a.memoryStore.CountAllUserMessages()
	if totalUserMsgs == 0 {
		return
	}

	// Track last trigger per job type via meta keys.
	lastCount := func(key string) int {
		s := a.memoryStore.GetMeta(key)
		if s == "" { return 0 }
		var n int
		fmt.Sscanf(s, "%d", &n)
		return n
	}
	shouldFire := func(key string, every int) bool {
		last := lastCount(key)
		return totalUserMsgs-last >= every
	}
	markFired := func(key string) {
		_ = a.memoryStore.SetMeta(key, fmt.Sprintf("%d", totalUserMsgs))
	}

	// Collect dialogue from recent messages across all sessions.
	collectDialogue := func(maxLines int) string {
		var sb strings.Builder
		sessions, _ := a.memoryStore.ListSessions()
		collected := 0
		for _, sess := range sessions {
			msgs, err := a.memoryStore.ListMessagesRecent(sess.ID, 20)
			if err != nil { continue }
			for _, m := range msgs {
				if m.Content == "" || (m.Role != "user" && m.Role != "assistant") {
					continue
				}
				if m.Role == "user" { sb.WriteString("用户: ")
				} else { sb.WriteString("助手: ") }
				sb.WriteString(m.Content)
				sb.WriteString("\n")
				collected++
			}
			if collected >= maxLines { break }
		}
		return strings.TrimSpace(sb.String())
	}

	// ── Facts + Graph (every N user messages) ──
	if shouldFire("schedFacts", scheduleFacts) {
		markFired("schedFacts")
		dialogue := collectDialogue(60)
		if dialogue == "" { return }
		p, err := a.resolveExtractionProvider()
		if err != nil {
			log.Printf("[sched] facts: no provider (%v)", err)
			return
		}
		// Facts
		go func() {
			facts, err := a.callExtractFacts(p, dialogue)
			if err != nil {
				log.Printf("[sched] facts: %v", err)
				return
			}
			dir := a.memoryStore.EmbeddingModelDir()
			// Land extracted facts in the SAME library as the conversation that
			// produced them (the most recent turn's workspace), NOT always core.
			libID := a.memoryStore.LastTurnLibrary()
			for _, f := range facts {
				if strings.TrimSpace(f.Content) == "" { continue }
				if f.Importance == "high" {
					a.memoryStore.AddUserFact(uuid.NewString(), f.Category, f.Content, f.Category, "high", "extract", libID)
					continue
				}
				if dir != "" {
					emb, _ := rag.EmbedQuery(dir, f.Content)
					a.memoryStore.AddFactMemory(uuid.NewString(), f.Content, f.Category, f.Importance, libID, "[]", emb)
				}
			}
			if len(facts) > 0 {
				log.Printf("[sched] %d facts extracted", len(facts))
				a.emitChanged("memory:changed", "extract", "")
			}
		}()
		// Graph
		go func() {
			entities, relations, err := a.callExtractGraph(p, dialogue)
			if err != nil {
				log.Printf("[sched] graph: %v", err)
				return
			}
			if len(entities)+len(relations) == 0 { return }
			dir := a.memoryStore.EmbeddingModelDir()
			var embedFn func(string) ([]float32, error)
			if dir != "" {
				embedFn = func(text string) ([]float32, error) { return rag.EmbedQuery(dir, text) }
			}
			libID, _ := a.memoryStore.DefaultLibrary()
			a.memoryStore.IngestGraph(entities, relations, "", libID, embedFn)
			log.Printf("[sched] graph: %d entities + %d relations", len(entities), len(relations))
			a.emitChanged("memory:changed", "extract", "")
		}()
	}

	// ── Summarize session (every N user messages) ──
	if shouldFire("schedSummarize", scheduleSummarize) {
		markFired("schedSummarize")
		sessions, _ := a.memoryStore.ListSessions()
		for _, sess := range sessions {
			go a.maybeSummarize(sess.ID)
		}
	}

	// ── Reflect (every N user messages) ──
	if shouldFire("schedReflect", scheduleReflect) {
		markFired("schedReflect")
		dialogue := collectDialogue(40)
		if dialogue == "" { return }
		go a.maybeReflectFrom(dialogue)
	}

	// ── Cross-domain entity linking (every N user messages) ──
	if shouldFire("schedLink", scheduleLink) {
		markFired("schedLink")
		go func() {
			dir := a.memoryStore.EmbeddingModelDir()
			if dir == "" { return }
			go a.maybeLinkEntities(dir)
		}()
	}
}

// maybeReflectFrom runs the reflection loop on a specific dialogue block.
func (a *App) maybeReflectFrom(dialogue string) {
	if a.memoryStore == nil { return }
	p, err := a.resolveExtractionProvider()
	if err != nil { return }
	insights, err := a.callReflect(p, dialogue)
	if err != nil { return }
	now := time.Now().UnixMilli()
	libID, _ := a.memoryStore.DefaultLibrary()
	stored := 0
	for _, in := range insights {
		if in.Confidence < 0.5 || strings.TrimSpace(in.Content) == "" { continue }
		switch in.Kind {
		case "insight", "lesson", "strategy", "error_pattern":
		default:
			in.Kind = "insight"
		}
		id := uuid.NewString()
		if err := a.memoryStore.AddExperience(id, libID, in.Kind, in.Content, in.Context, in.Confidence, now); err != nil {
			continue
		}
		stored++
	}
	if stored > 0 {
		log.Printf("[sched] reflect: %d insights", stored)
	}
}
