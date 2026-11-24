package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Event tells SSE clients that an entity changed; they refetch what they show.
type Event struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Hub fans events out to every connected SSE client.
type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }

// Notify publishes that the entity kind/id changed.
func (h *Hub) Notify(kind, id string) { h.Publish(Event{Kind: kind, ID: id}) }

// Publish sends v to every client. A client too slow to keep up is dropped; it
// reconnects and refetches.
func (h *Hub) Publish(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	msg := fmt.Appendf(nil, "data: %s\n\n", b)
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- msg:
		default:
			h.drop(c)
		}
	}
}

func (h *Hub) drop(c chan []byte) {
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c)
	}
}

// ServeHTTP streams events to one client until it disconnects.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	c := make(chan []byte, 64)
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		h.drop(c)
		h.mu.Unlock()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-c:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		}
		flusher.Flush()
	}
}
