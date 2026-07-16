package core

import "sync"

// EventBus is a lightweight in-process pub/sub event bus.
type EventBus struct {
	mu     sync.RWMutex
	subs   map[string][]chan any
	closed bool
}

// NewEventBus creates an empty event bus.
func NewEventBus() *EventBus {
	return &EventBus{
		subs: make(map[string][]chan any),
	}
}

// Subscribe registers a channel to receive events on the given topic.
func (b *EventBus) Subscribe(topic string, ch chan any) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs[topic] = append(b.subs[topic], ch)
}

// Publish sends an event to all subscribers of the given topic.
func (b *EventBus) Publish(topic string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return
	}
	for _, ch := range b.subs[topic] {
		select {
		case ch <- data:
		default:
			// drop if subscriber is not reading
		}
	}
}
