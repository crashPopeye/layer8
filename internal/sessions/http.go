package sessions

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/tree"
)

// ServeEvents pages a session's stored events: GET /api/sessions/{id8}/events?after=<id>&limit=<n>.
func (m *Manager) ServeEvents(w http.ResponseWriter, r *http.Request) {
	id8 := r.PathValue("id8")
	s, err := m.load(r.Context(), id8)
	if errors.Is(err, tree.ErrNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := m.db.QueryContext(r.Context(), `SELECT id, type, at, data FROM session_events WHERE session_id = ? AND id > ? ORDER BY id LIMIT ?`, s.ID, after, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.ID, &e.Type, &e.At, &data); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		e.Data = json.RawMessage(data)
		events = append(events, e)
	}
	writeJSON(w, map[string]any{"events": events})
}

// ServeCapabilities probes what a harness offers in a task's worktree:
// GET /api/capabilities?task=<addr>&harness=<name>.
func (m *Manager) ServeCapabilities(w http.ResponseWriter, r *http.Request) {
	t, err := m.tasks.Lookup(r.Context(), tree.Addr(r.URL.Query().Get("task")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	caps, err := m.Capabilities(r.Context(), cmp.Or(r.URL.Query().Get("harness"), "claude"), t.Worktree)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, caps)
}

// probe is one cached capability probe.
type probe struct {
	mu   sync.Mutex
	at   time.Time
	caps harness.Capabilities
	err  error
}

const probeTTL = time.Minute

// Capabilities returns what the named harness offers in cwd, probing at most once per probeTTL.
func (m *Manager) Capabilities(ctx context.Context, name, cwd string) (harness.Capabilities, error) {
	h, ok := m.o.Harnesses[name]
	if !ok {
		return harness.Capabilities{}, fmt.Errorf("unknown harness %q", name)
	}
	key := name + "\x00" + cwd
	m.mu.Lock()
	p := m.probes[key]
	if p == nil {
		p = &probe{}
		m.probes[key] = p
	}
	m.mu.Unlock()
	p.mu.Lock()
	defer p.mu.Unlock()
	if time.Since(p.at) >= probeTTL {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		p.caps, p.err = h.Probe(ctx, cwd)
		p.at = time.Now()
	}
	return p.caps, p.err
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
