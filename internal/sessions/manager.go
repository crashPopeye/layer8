package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

// run is the live side of a session: its agent process, or its place in the queue.
type run struct {
	id8      string
	proc     harness.Proc
	queued   bool
	pending  []harness.UserMessage
	turns    int
	respond  map[int64]func(harness.Decision)
	after    string
	stale    bool
	idle     *time.Timer
	active   time.Time
	baseCost float64
	status   string
}

func newRun(id8 string, pending []harness.UserMessage) *run {
	return &run{id8: id8, pending: pending, respond: map[int64]func(harness.Decision){}, active: time.Now()}
}

func (r *run) state() string {
	switch {
	case r.queued:
		return "queued"
	case r.proc == nil:
		return "starting"
	case len(r.respond) > 0:
		return "waiting"
	case r.turns > 0:
		return "running"
	}
	return "idle"
}

func (r *run) idleLive() bool {
	return r.proc != nil && r.after == "" && r.turns == 0 && len(r.respond) == 0
}

// say records a user turn and delivers it, starting or queueing a process when the session has none.
func (m *Manager) say(id8 string, msg harness.UserMessage, by tree.Actor) error {
	data := map[string]any{"text": msg.Text, "by": by.String()}
	if len(msg.Files) > 0 {
		data["files"] = msg.Files
	}
	if len(msg.Images) > 0 {
		var images []map[string]any
		for _, img := range msg.Images {
			images = append(images, map[string]any{"mediaType": img.MediaType, "data": img.Data})
		}
		data["images"] = images
	}
	m.record(id8, harness.EventUser, data)
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id8]
	switch {
	case r == nil:
		m.admit(newRun(id8, []harness.UserMessage{msg}))
		return nil
	case r.proc != nil && r.after == "":
		if err := r.proc.Send(msg); err != nil {
			return err
		}
		r.turns++
		r.active = time.Now()
		if r.idle != nil {
			r.idle.Stop()
		}
	default:
		r.pending = append(r.pending, msg)
	}
	m.sync(r)
	return nil
}

// admit starts r when a slot is free, and otherwise queues it and closes the oldest idle process.
func (m *Manager) admit(r *run) {
	full := m.live() >= m.o.Max
	m.runs[r.id8] = r
	if !full {
		m.sync(r)
		go m.start(r)
		return
	}
	r.queued = true
	m.queue = append(m.queue, r)
	m.sync(r)
	var oldest *run
	for _, o := range m.runs {
		if o.idleLive() && (oldest == nil || o.active.Before(oldest.active)) {
			oldest = o
		}
	}
	if oldest != nil {
		m.closeRun(oldest, "idle")
	}
}

func (m *Manager) live() int { return len(m.runs) - len(m.queue) }

// Busy reports whether any session is queued, starting, running or waiting for an approval.
func (m *Manager) Busy() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.runs {
		if r.state() != "idle" {
			return true
		}
	}
	return false
}

// next starts queued runs while slots are free.
func (m *Manager) next() {
	for len(m.queue) > 0 && m.live() < m.o.Max {
		r := m.queue[0]
		m.queue = m.queue[1:]
		r.queued = false
		m.sync(r)
		go m.start(r)
	}
}

func (m *Manager) start(r *run) {
	p, cost, err := m.spawn(r)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs[r.id8] != r {
		if p != nil {
			go p.Close(m.o.Grace)
		}
		return
	}
	if err != nil {
		delete(m.runs, r.id8)
		status := "failed"
		if r.after == "done" {
			status = "done"
		}
		m.record(r.id8, harness.EventError, map[string]any{"message": err.Error()})
		m.setStatus(r.id8, status, err.Error())
		m.next()
		return
	}
	r.proc, r.baseCost = p, cost
	if r.after != "" {
		go p.Close(m.o.Grace)
		return
	}
	for _, msg := range r.pending {
		if p.Send(msg) == nil {
			r.turns++
		}
	}
	r.pending = nil
	r.active = time.Now()
	m.sync(r)
}

// spawn starts the session's agent process in its task's worktree.
func (m *Manager) spawn(r *run) (harness.Proc, float64, error) {
	ctx := context.Background()
	s, err := m.load(ctx, r.id8)
	if err != nil {
		return nil, 0, err
	}
	t, err := m.tasks.Lookup(ctx, tasks.AddrOf(s.TaskSlug))
	if err != nil {
		return nil, 0, err
	}
	h, ok := m.o.Harnesses[s.Harness]
	if !ok {
		return nil, 0, fmt.Errorf("unknown harness %q", s.Harness)
	}
	allowed := []string{"ListMcpResourcesTool", "ReadMcpResourceTool"}
	if m.o.AutoApproveDo {
		allowed = append(allowed, "mcp__layer8__do")
	}
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	p, err := h.Start(ctx, harness.StartOptions{
		Cwd: t.Worktree, AddDirs: []string{t.Artifacts}, Env: []string{"LAYER8_ARTIFACTS=" + t.Artifacts},
		Model: s.Model, Effort: s.Effort, PermissionMode: s.PermissionMode, ResumeID: s.HarnessSessionID, Fork: s.Fork,
		MCPURL: m.o.MCPURL, MCPHeaders: map[string]string{"Authorization": "Bearer " + m.o.Token, "X-Layer8-Session": r.id8},
		AllowedTools: allowed,
		AppendSystemPrompt: fmt.Sprintf("You are running inside layer8 as session:%s in %s. The task tree's shared artifacts folder is %s ($LAYER8_ARTIFACTS). "+
			"Read the layer8://here MCP resource for your task and the actions you can apply with the layer8 do tool.", r.id8, t.Addr(), t.Artifacts),
	}, &sink{m: m, r: r, harness: s.Harness})
	return p, s.CostUSD, err
}

const startTimeout = 2 * time.Minute

// closeRun closes r's process; its status becomes after once the process exits.
func (m *Manager) closeRun(r *run, after string) {
	r.after = after
	if r.idle != nil {
		r.idle.Stop()
	}
	if r.proc != nil {
		go r.proc.Close(m.o.Grace)
	}
}

// release closes r's process when it is still live and idle.
func (m *Manager) release(r *run) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs[r.id8] == r && r.idleLive() {
		m.closeRun(r, "idle")
	}
}

// sync stores r's status when it changed.
func (m *Manager) sync(r *run) {
	if st := r.state(); st != r.status {
		r.status = st
		m.setStatus(r.id8, st, "")
	}
}

func (m *Manager) setStatus(id8, status, errMsg string) {
	if _, err := m.db.Exec(`UPDATE sessions SET status = ?, error = ? WHERE id8 = ?`, status, errMsg, id8); err != nil {
		slog.Warn("session status update failed", "session", id8, "err", err)
	}
	m.changed(id8)
}

func (m *Manager) changed(id8 string) {
	var slug string
	m.db.QueryRow(`SELECT t.slug FROM sessions s JOIN tasks t ON t.id = s.task_id WHERE s.id8 = ?`, id8).Scan(&slug)
	m.tree.Changed(addr(id8), tasks.AddrOf(slug))
}

// record stores a transcript event and streams it.
func (m *Manager) record(id8 string, typ harness.EventType, data map[string]any) {
	raw, err := json.Marshal(data)
	if err != nil {
		raw, _ = json.Marshal(map[string]any{"marshalError": err.Error()})
	}
	at := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := m.db.Exec(`INSERT INTO session_events (session_id, type, at, data) VALUES ((SELECT id FROM sessions WHERE id8 = ?), ?, ?, ?)`, id8, typ, at, raw)
	if err != nil {
		slog.Warn("session event insert failed", "session", id8, "err", err)
		return
	}
	id, _ := res.LastInsertId()
	m.publish(id8, Event{ID: id, Type: typ, At: at, Data: json.RawMessage(raw)})
}

func (m *Manager) publish(id8 string, events ...Event) {
	m.o.Publish("session", map[string]any{"session": id8, "events": events})
}

// closeSession ends the session: queued runs leave the queue, live processes are closed.
func (m *Manager) closeSession(id8 string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id8]
	switch {
	case r == nil:
		m.setStatus(id8, "done", "")
	case r.queued:
		m.queue = slices.DeleteFunc(m.queue, func(q *run) bool { return q == r })
		delete(m.runs, id8)
		m.setStatus(id8, "done", "")
	default:
		r.pending = nil
		m.closeRun(r, "done")
	}
}

// interrupt denies pending approvals and stops the current turn.
func (m *Manager) interrupt(ctx context.Context, id8 string) error {
	m.mu.Lock()
	r := m.runs[id8]
	if r == nil || r.proc == nil {
		m.mu.Unlock()
		return errors.New("session has no running process")
	}
	for id, respond := range r.respond {
		respond(harness.Decision{Message: "The user interrupted."})
		m.db.Exec(`UPDATE approvals SET status = 'denied', decided_by = 'user' WHERE id = ?`, id)
		m.record(id8, harness.EventApproval, map[string]any{"approval": id, "decision": "deny", "message": "interrupted", "by": "user"})
	}
	clear(r.respond)
	r.turns = min(r.turns, 1)
	p := r.proc
	m.sync(r)
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return p.Interrupt(ctx)
}

// answer settles a pending approval. Only the user may answer.
func (m *Manager) answer(id8 string, approval int64, d harness.Decision, by tree.Actor) (int64, error) {
	if by.Kind != "user" {
		return 0, fmt.Errorf("only the user can answer approvals, not %s", by)
	}
	if approval == 0 {
		m.db.QueryRow(`SELECT coalesce(min(a.id), 0) FROM approvals a JOIN sessions s ON s.id = a.session_id WHERE s.id8 = ? AND a.status = 'pending'`, id8).Scan(&approval)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.runs[id8]
	var respond func(harness.Decision)
	if r != nil {
		respond = r.respond[approval]
		delete(r.respond, approval)
	}
	if respond == nil {
		return 0, fmt.Errorf("approval %d is not pending on session:%s", approval, id8)
	}
	decision, status := "deny", "denied"
	if d.Allow {
		decision, status = "allow", "allowed"
	}
	m.db.Exec(`UPDATE approvals SET status = ?, decided_by = ? WHERE id = ?`, status, by.String(), approval)
	m.record(id8, harness.EventApproval, map[string]any{"approval": approval, "decision": decision, "always": d.Always, "message": d.Message, "by": by.String()})
	respond(d)
	m.sync(r)
	return approval, nil
}

// restartAfterTurn makes a live session pick up new settings: now when idle, otherwise after its turn.
func (m *Manager) restartAfterTurn(id8 string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.runs[id8]; r != nil {
		r.stale = true
		if r.idleLive() {
			m.closeRun(r, "idle")
		}
	}
}

// sink receives one process's output.
type sink struct {
	m       *Manager
	r       *run
	harness string
}

func (s *sink) OnEvent(e harness.Event) {
	m, id8 := s.m, s.r.id8
	if e.Type == harness.EventLimits {
		limits, _ := e.Data["limits"].([]harness.Limit)
		m.setLimits(s.harness, limits)
		return
	}
	if e.Partial {
		m.publish(id8, Event{Type: e.Type, Partial: true, Data: e.Data})
		return
	}
	m.record(id8, e.Type, e.Data)
	switch {
	case e.Type == harness.EventSystem && e.Data["subtype"] == "init":
		caps, _ := json.Marshal(map[string]any{"mcpServers": e.Data["mcp_servers"], "skills": e.Data["skills"],
			"slashCommands": e.Data["slash_commands"], "plugins": e.Data["plugins"]})
		m.db.Exec(`UPDATE sessions SET capabilities = ? WHERE id8 = ?`, caps, id8)
		m.saveHarnessID(id8, e.Data["session_id"])
		m.changed(id8)
	case e.Type == harness.EventResult:
		m.endTurn(s.r, e.Data)
	}
}

func (m *Manager) saveHarnessID(id8 string, v any) {
	if hid, _ := v.(string); hid != "" {
		m.db.Exec(`UPDATE sessions SET harness_session_id = ?, fork = 0 WHERE id8 = ?`, hid, id8)
	}
}

func (m *Manager) endTurn(r *run, data map[string]any) {
	m.saveHarnessID(r.id8, data["sessionId"])
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db.Exec(`UPDATE sessions SET cost_usd = ?, context_tokens = ?, context_window = COALESCE(NULLIF(?, 0), context_window) WHERE id8 = ?`,
		r.baseCost+number(data["costUsd"]), int(number(data["contextTokens"])), int(number(data["contextWindow"])), r.id8)
	r.turns = max(r.turns-1, 0)
	r.active = time.Now()
	if r.idleLive() {
		if r.stale || len(m.queue) > 0 {
			m.closeRun(r, "idle")
		} else {
			r.idle = time.AfterFunc(m.o.IdleAfter, func() { m.release(r) })
		}
	}
	m.sync(r)
	m.changed(r.id8)
	m.tree.Changed(usageAddr)
}

func number(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}

func (s *sink) OnPermission(req harness.PermissionRequest, respond func(harness.Decision)) {
	m, id8 := s.m, s.r.id8
	raw, _ := json.Marshal(req)
	res, err := m.db.Exec(`INSERT INTO approvals (session_id, request) VALUES ((SELECT id FROM sessions WHERE id8 = ?), ?)`, id8, raw)
	if err != nil {
		respond(harness.Decision{Message: "layer8 could not record this approval: " + err.Error()})
		return
	}
	id, _ := res.LastInsertId()
	m.record(id8, harness.EventApproval, map[string]any{"approval": id, "tool": req.Tool, "input": req.Input,
		"description": req.Description, "toolUseId": req.ToolUseID, "decision": "pending"})
	m.mu.Lock()
	defer m.mu.Unlock()
	s.r.respond[id] = respond
	m.sync(s.r)
}

func (s *sink) OnExit(err error) {
	m, r := s.m, s.r
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs[r.id8] != r {
		return
	}
	delete(m.runs, r.id8)
	if r.idle != nil {
		r.idle.Stop()
	}
	m.db.Exec(`UPDATE approvals SET status = 'expired' WHERE session_id = (SELECT id FROM sessions WHERE id8 = ?) AND status = 'pending'`, r.id8)
	status, msg := "idle", ""
	switch {
	case r.after == "done":
		status = "done"
	case err != nil:
		status, msg = "failed", err.Error()
		m.record(r.id8, harness.EventError, map[string]any{"message": msg})
	}
	m.setStatus(r.id8, status, msg)
	m.next()
	if len(r.pending) > 0 {
		m.admit(newRun(r.id8, r.pending))
	}
}
