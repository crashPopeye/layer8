package server

import (
	"sync"

	"github.com/crashPopeye/layer8/internal/tree"
)

// Event is one message on the live stream: node, session, ui or toast.
type Event struct {
	Name string
	Data any
}

// NodeChange is the data of a node event.
type NodeChange struct {
	Addrs []tree.Addr `json:"addrs"`
}

// Hub fans events out to every subscriber.
type Hub struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewHub returns a hub with no subscribers.
func NewHub() *Hub {
	return &Hub{subs: map[chan Event]struct{}{}}
}

// Publish sends an event to every subscriber, dropping those that fall behind.
func (h *Hub) Publish(name string, data any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- Event{Name: name, Data: data}:
		default:
			delete(h.subs, ch)
			close(ch)
		}
	}
}

// Subscribe returns a channel of events, closed when cancel is called or the subscriber falls behind.
func (h *Hub) Subscribe() (events <-chan Event, cancel func()) {
	ch := make(chan Event, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}
}
