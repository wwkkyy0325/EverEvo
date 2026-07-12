// Package collab provides the multi-agent collaboration kernel: an in-process
// event bus, shared blackboard, local agent dispatcher, and collaboration
// sessions. These let multiple agents and workflows coordinate asynchronously
// instead of the existing synchronous one-shot delegation.
//
// Design notes:
//   - All primitives are in-process (no HTTP for local-local comms).
//   - Concurrency follows existing idioms: sync.RWMutex guards maps; buffered
//     channels signal completion (mirrors workflow.ExecutionState.Done).
//   - Events flow through the backend bus AND optionally forward to the Wails
//     frontend for visualization (the bus does NOT depend on Wails itself).
package collab

import (
	"log"
	"strings"
	"sync"
	"time"
)

// Event is one notification published on the bus.
type Event struct {
	Topic   string    `json:"topic"`   // e.g. "agent.<id>.done", "blackboard.<id>.changed"
	Source  string    `json:"source"`  // agentID or workflow execID
	Type    string    `json:"type"`    // finer-grained event type
	Payload any       `json:"payload,omitempty"`
	At      time.Time `json:"at"`
}

// subscriberBufferSize caps each subscriber's channel. When full, the oldest
// event is dropped (keep-recent policy) to prevent a slow consumer from
// blocking publishers or leaking memory.
const subscriberBufferSize = 16

// subscriberIdleTTL is how long a subscriber may sit idle before being
// auto-reclaimed. Prevents leaked subscriptions when a caller forgets to
// Unsubscribe. Refreshed on each delivered event.
const subscriberIdleTTL = 30 * time.Minute

// EventBus is an in-process publish/subscribe broker. Subscribers receive
// events on buffered channels; topics support prefix matching ("agent.*").
type EventBus struct {
	mu   sync.RWMutex
	subs map[string][]*Subscriber
	// forward is an optional sink (e.g. Wails EventsEmit) for frontend mirroring.
	forward func(string, any)
}

// NewEventBus creates a bus. forward (nil ok) mirrors every event to an
// external sink for UI visualization.
func NewEventBus(forward func(topic string, data any)) *EventBus {
	return &EventBus{
		subs:    map[string][]*Subscriber{},
		forward: forward,
	}
}

// Subscriber is a typed handle so callers hold a receive-only channel while
// the bus retains the send end for identity-matched unsubscribe. An idle
// reaper closes the channel after subscriberIdleTTL of no delivery, so a
// forgotten subscription can't leak forever.
type Subscriber struct {
	topic     string
	bus       *EventBus
	recv      <-chan Event
	send      chan Event
	lastUsed  time.Time
	mu        sync.Mutex
}

// Recv returns the receive-only channel for the caller to read events from.
func (s *Subscriber) Recv() <-chan Event { return s.recv }

// Subscribe registers interest in a topic and returns a Subscriber. Topic may
// be an exact name or end with ".*" to match a prefix. The caller SHOULD call
// Unsubscribe when done; an idle reaper reclaims it after subscriberIdleTTL.
func (b *EventBus) Subscribe(topic string) *Subscriber {
	ch := make(chan Event, subscriberBufferSize)
	sub := &Subscriber{topic: topic, bus: b, recv: ch, send: ch, lastUsed: time.Now()}
	b.mu.Lock()
	b.subs[topic] = append(b.subs[topic], sub)
	b.mu.Unlock()
	go sub.reapWhenIdle()
	return sub
}

// reapWhenIdle closes + unsubscribes this subscriber after it sits idle past
// subscriberIdleTTL. Activity (event delivery) refreshes the timer.
func (s *Subscriber) reapWhenIdle() {
	for {
		s.mu.Lock()
		deadline := s.lastUsed.Add(subscriberIdleTTL)
		s.mu.Unlock()
		wait := time.Until(deadline)
		if wait <= 0 {
			s.bus.Unsubscribe(s.topic, s)
			return
		}
		time.Sleep(wait)
	}
}

// Unsubscribe removes a subscriber. Safe to call with an unknown sub.
func (b *EventBus) Unsubscribe(topic string, sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[topic]
	for i, s := range subs {
		if s == sub {
			b.subs[topic] = append(subs[:i], subs[i+1:]...)
			close(s.send)
			break
		}
	}
	if len(b.subs[topic]) == 0 {
		delete(b.subs, topic)
	}
}

// Publish broadcasts an event to all matching subscribers. Matching is exact
// OR prefix when a subscriber registered with a trailing ".*" (e.g. a
// subscription to "agent.*" receives "agent.ag-123.done").
func (b *EventBus) Publish(topic string, ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	ev.Topic = topic

	b.mu.RLock()
	// Snapshot matching subscribers under the read lock.
	var targets []*Subscriber
	for subTopic, subs := range b.subs {
		if topicMatches(subTopic, topic) {
			targets = append(targets, subs...)
		}
	}
	b.mu.RUnlock()

	for _, s := range targets {
		s.mu.Lock()
		s.lastUsed = ev.At
		s.mu.Unlock()
		select {
		case s.send <- ev:
		default:
			// Buffer full — drop the oldest by draining one, then push.
			select {
			case <-s.send:
			default:
			}
			select {
			case s.send <- ev:
			default:
			}
			log.Printf("[collab] event bus: dropped oldest on full subscriber (topic=%s)", topic)
		}
	}

	if b.forward != nil {
		b.forward(topic, ev)
	}
}

// topicMatches reports whether a published topic delivers to a subscription.
// A subscription ending in ".*" is a prefix match on the stem.
func topicMatches(subTopic, pubTopic string) bool {
	if subTopic == pubTopic {
		return true
	}
	if strings.HasSuffix(subTopic, ".*") {
		stem := strings.TrimSuffix(subTopic, "*")
		return strings.HasPrefix(pubTopic, stem)
	}
	return false
}
