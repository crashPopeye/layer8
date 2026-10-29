// Package sessions runs agent sessions: one long-lived claude process per active
// session, a global concurrency limit on running turns, and approvals.
package sessions

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/store"
)

// Proc is the part of claude.Process the manager uses; tests substitute a fake.
type Proc interface {
	Send(text string) error
	Interrupt(ctx context.Context) error
	Shutdown(grace time.Duration)
	Done() <-chan struct{}
}

type StartFunc func(harness string, o claude.Options, h claude.Handlers) (Proc, error)

// Bus is how the manager tells the UI about changes.
type Bus interface {
	Changed()
	Event(store.Event)
}

// Launch describes what a session needs to (re)start its process.
type Launch struct {
	Harness            string
	Cwd                string
	AddDirs            []string
	AppendSystemPrompt string
	Model              string
	Effort             string
	PermissionMode     string
}

type live struct {
	proc         Proc
	busy         bool // a turn is in flight
	interrupting bool
	stale        bool // settings changed: restart the process after this turn
	context      int  // tokens in the context window
	idle         *time.Timer
	stderr       []string
}

type pendingApproval struct {
	sessionID string
	requestID string
	always    []string // rules "Always allow" saves
	respond   func(claude.Decision)
}

type queued struct {
	sessionID string
	prompt    string
}

type Manager struct {
	store  *store.Store
	bus    Bus
	start  StartFunc
	max    func() int
	launch func(store.Session) (Launch, error)

	// IdleTimeout closes a process after this long without a turn; the next message resumes it.
	IdleTimeout time.Duration
	// OnTurnEnd runs after each completed turn (ok=false when it failed or was interrupted).
	OnTurnEnd func(s store.Session, ok bool)

	mu        sync.Mutex
	live      map[string]*live
	queue     []queued
	approvals map[string]*pendingApproval
	closed    bool
}

func New(st *store.Store, bus Bus, start StartFunc, maxConcurrent func() int, launch func(store.Session) (Launch, error)) *Manager {
	return &Manager{
		store: st, bus: bus, start: start, max: maxConcurrent, launch: launch,
		IdleTimeout: 10 * time.Minute,
		live:        map[string]*live{}, approvals: map[string]*pendingApproval{},
	}
}

var ErrBusy = errors.New("this session is still working; stop it or wait for it to finish")

// Create inserts a session and queues its first prompt.
func (m *Manager) Create(s store.Session, prompt string) (store.Session, error) {
	s.Status = "queued"
	s, err := m.store.InsertSession(s)
	if err != nil {
		return s, err
	}
	m.mu.Lock()
	m.queue = append(m.queue, queued{s.ID, prompt})
	m.mu.Unlock()
	m.bus.Changed()
	m.pump()
	return s, nil
}

// Send queues a follow-up message on an existing session.
func (m *Manager) Send(sessionID, text string) error {
	if strings.TrimSpace(text) == "" {
		return errors.New("empty message")
	}
	s, err := m.store.Session(sessionID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if l := m.live[sessionID]; l != nil && l.busy {
		m.mu.Unlock()
		return ErrBusy
	}
	for _, q := range m.queue {
		if q.sessionID == sessionID {
			m.mu.Unlock()
			return ErrBusy
		}
	}
	m.queue = append(m.queue, queued{sessionID, text})
	m.mu.Unlock()
	_ = m.store.SetSessionStatus(s.ID, "queued", "")
	m.bus.Changed()
	m.pump()
	return nil
}

// Busy reports whether a session has a turn in flight or queued.
func (m *Manager) Busy(sessionID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.live[sessionID]; l != nil && l.busy {
		return true
	}
	for _, q := range m.queue {
		if q.sessionID == sessionID {
			return true
		}
	}
	return false
}

func (m *Manager) runningCount() int {
	n := 0
	for _, l := range m.live {
		if l.busy {
			n++
		}
	}
	return n
}

func (m *Manager) pump() {
	for {
		m.mu.Lock()
		if m.closed || len(m.queue) == 0 || m.runningCount() >= m.max() {
			m.mu.Unlock()
			return
		}
		q := m.queue[0]
		m.queue = m.queue[1:]
		l := m.live[q.sessionID]
		if l == nil {
			l = &live{}
			m.live[q.sessionID] = l
		}
		l.busy, l.interrupting = true, false
		if l.idle != nil {
			l.idle.Stop()
			l.idle = nil
		}
		m.mu.Unlock()
		m.startTurn(q.sessionID, q.prompt, l)
	}
}

func (m *Manager) startTurn(sessionID, prompt string, l *live) {
	fail := func(err error) {
		m.mu.Lock()
		l.busy = false
		if l.proc == nil {
			delete(m.live, sessionID)
		}
		m.mu.Unlock()
		m.emit(sessionID, "error", map[string]any{"message": err.Error()})
		_ = m.store.SetSessionStatus(sessionID, "failed", err.Error())
		m.bus.Changed()
		go m.pump()
	}
	s, err := m.store.Session(sessionID)
	if err != nil {
		fail(err)
		return
	}
	_ = m.store.SetSessionStatus(sessionID, "running", "")
	m.emit(sessionID, "user", map[string]any{"text": prompt})
	m.bus.Changed()

	m.mu.Lock()
	proc := l.proc
	m.mu.Unlock()
	if proc == nil {
		ln, err := m.launch(s)
		if err != nil {
			fail(err)
			return
		}
		proc, err = m.start(ln.Harness, claude.Options{
			Cwd: ln.Cwd, Resume: s.ClaudeSessionID, AddDirs: ln.AddDirs,
			AppendSystemPrompt: ln.AppendSystemPrompt, Model: ln.Model, Effort: ln.Effort, PermissionMode: ln.PermissionMode,
		}, m.handlers(sessionID, l))
		if err != nil {
			fail(err)
			return
		}
		m.mu.Lock()
		l.proc = proc
		m.mu.Unlock()
		go m.watchExit(sessionID, l, proc)
	}
	if err := proc.Send(prompt); err != nil {
		fail(err)
	}
}

func (m *Manager) handlers(sessionID string, l *live) claude.Handlers {
	return claude.Handlers{
		OnMessage: func(msg claude.Message) { m.onMessage(sessionID, l, msg) },
		OnPermission: func(req claude.PermissionRequest, respond func(claude.Decision)) {
			a, err := m.store.InsertApproval(sessionID, req.ToolName, req.Input)
			if err != nil {
				respond(claude.Decision{Allow: false, Message: "layer8 could not record the approval request"})
				return
			}
			m.mu.Lock()
			var always []string
			for _, r := range req.AlwaysRules() {
				always = append(always, r.String())
			}
			m.approvals[a.ID] = &pendingApproval{sessionID: sessionID, requestID: req.RequestID, always: always, respond: respond}
			m.mu.Unlock()
			_ = m.store.SetSessionStatus(sessionID, "waiting_approval", "")
			m.bus.Changed()
		},
		OnPermissionCancelled: func(requestID string) {
			m.mu.Lock()
			var id string
			for aid, p := range m.approvals {
				if p.sessionID == sessionID && p.requestID == requestID {
					id = aid
				}
			}
			delete(m.approvals, id)
			m.mu.Unlock()
			if id != "" {
				_ = m.store.SetApprovalStatus(id, "cancelled")
				_ = m.store.SetSessionStatus(sessionID, "running", "")
				m.bus.Changed()
			}
		},
		OnStderr: func(line string) {
			m.mu.Lock()
			l.stderr = append(l.stderr, line)
			if len(l.stderr) > 30 {
				l.stderr = l.stderr[len(l.stderr)-30:]
			}
			m.mu.Unlock()
		},
	}
}

const toolResultLimit = 20000

func (m *Manager) onMessage(sessionID string, l *live, msg claude.Message) {
	switch msg.Type {
	case "system":
		if msg.Subtype != "init" {
			return
		}
		if msg.SessionID != "" {
			_ = m.store.SetClaudeSessionID(sessionID, msg.SessionID)
		}
		// init also starts turns the agent begins on its own (a background task finished).
		m.mu.Lock()
		autonomous := !l.busy
		if autonomous {
			l.busy = true
			if l.idle != nil {
				l.idle.Stop()
				l.idle = nil
			}
		}
		m.mu.Unlock()
		if autonomous {
			_ = m.store.SetSessionStatus(sessionID, "running", "")
		}
		m.bus.Changed()
	case "assistant":
		parent := ""
		if msg.ParentToolUseID != nil {
			parent = *msg.ParentToolUseID
		}
		if u := msg.Message.Usage; u != nil && parent == "" && u.Context() > 0 {
			m.mu.Lock()
			l.context = u.Context()
			m.mu.Unlock()
			_ = m.store.SetSessionContext(sessionID, u.Context(), 0)
			m.bus.Changed()
		}
		for _, b := range msg.Message.Blocks() {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					m.emit(sessionID, "text", map[string]any{"text": b.Text, "parent": parent})
				}
			case "tool_use":
				m.emit(sessionID, "tool_use", map[string]any{"id": b.ID, "name": b.Name, "input": b.Input, "parent": parent})
			}
		}
	case "user":
		for _, b := range msg.Message.Blocks() {
			if b.Type != "tool_result" {
				continue
			}
			text := b.ResultText()
			if len(text) > toolResultLimit {
				text = text[:toolResultLimit] + "\n… (truncated)"
			}
			m.emit(sessionID, "tool_result", map[string]any{"toolUseId": b.ToolUseID, "isError": b.IsError, "text": text})
		}
	case "result":
		m.mu.Lock()
		interrupted := l.interrupting
		l.busy, l.interrupting = false, false
		if l.stale {
			m.retire(sessionID, l, 30*time.Second)
		} else {
			m.armIdle(sessionID, l)
		}
		tokens := l.context
		m.mu.Unlock()
		window := 0
		for _, u := range msg.ModelUsage {
			window = max(window, u.ContextWindow)
		}
		_ = m.store.SetSessionContext(sessionID, tokens, window)
		_ = m.store.SetSessionCost(sessionID, msg.TotalCostUSD)
		ok := msg.Subtype == "success" && !msg.IsError
		m.emit(sessionID, "result", map[string]any{
			"ok": ok, "subtype": msg.Subtype, "durationMs": msg.DurationMS, "turns": msg.NumTurns,
			"costUsd": msg.TotalCostUSD, "interrupted": interrupted, "errors": msg.Errors,
		})
		status, lastErr := "idle", ""
		switch {
		case interrupted:
			status = "interrupted"
		case !ok:
			status, lastErr = "failed", strings.Join(append([]string{msg.Subtype}, msg.Errors...), ": ")
		}
		_ = m.store.SetSessionStatus(sessionID, status, lastErr)
		m.bus.Changed()
		if m.OnTurnEnd != nil {
			if s, err := m.store.Session(sessionID); err == nil {
				go m.OnTurnEnd(s, ok && !interrupted)
			}
		}
		go m.pump()
	}
}

// retire forgets a process and shuts it down; the next message starts a new one. Caller holds m.mu.
func (m *Manager) retire(sessionID string, l *live, grace time.Duration) {
	if m.live[sessionID] == l {
		delete(m.live, sessionID)
	}
	if l.idle != nil {
		l.idle.Stop()
	}
	if l.proc != nil {
		go l.proc.Shutdown(grace)
	}
}

// Reconfigure makes the session's next turn start a process with its new settings.
func (m *Manager) Reconfigure(sessionID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l := m.live[sessionID]
	if l == nil {
		return
	}
	if l.busy {
		l.stale = true
		return
	}
	m.retire(sessionID, l, 30*time.Second)
}

// Remove stops everything a session is doing so it can be deleted.
func (m *Manager) Remove(sessionID string) {
	m.mu.Lock()
	kept := m.queue[:0]
	for _, q := range m.queue {
		if q.sessionID != sessionID {
			kept = append(kept, q)
		}
	}
	m.queue = kept
	var pending []*pendingApproval
	for id, p := range m.approvals {
		if p.sessionID == sessionID {
			pending = append(pending, p)
			delete(m.approvals, id)
		}
	}
	if l := m.live[sessionID]; l != nil {
		l.busy = false
		m.retire(sessionID, l, 2*time.Second)
	}
	m.mu.Unlock()
	for _, p := range pending {
		p.respond(claude.Decision{Allow: false, Message: "The session was deleted."})
	}
	go m.pump()
}

// armIdle closes an idle process later; caller holds m.mu.
func (m *Manager) armIdle(sessionID string, l *live) {
	if m.IdleTimeout <= 0 || l.proc == nil {
		return
	}
	proc := l.proc
	l.idle = time.AfterFunc(m.IdleTimeout, func() {
		m.mu.Lock()
		stillIdle := m.live[sessionID] == l && !l.busy
		if stillIdle {
			// Forget it before shutting down, so a message sent meanwhile starts a fresh
			// process (with --resume) instead of writing to one that is closing.
			delete(m.live, sessionID)
		}
		m.mu.Unlock()
		if stillIdle {
			proc.Shutdown(30 * time.Second)
		}
	})
}

func (m *Manager) watchExit(sessionID string, l *live, proc Proc) {
	<-proc.Done()
	m.mu.Lock()
	wasBusy := l.busy
	tail := strings.Join(l.stderr, "\n")
	if m.live[sessionID] == l {
		delete(m.live, sessionID)
	}
	var cancelled []string
	for id, p := range m.approvals {
		if p.sessionID == sessionID {
			cancelled = append(cancelled, id)
			delete(m.approvals, id)
		}
	}
	closed := m.closed
	m.mu.Unlock()
	for _, id := range cancelled {
		_ = m.store.SetApprovalStatus(id, "cancelled")
	}
	if wasBusy && !closed {
		msg := "claude exited unexpectedly"
		if tail != "" {
			msg += ":\n" + tail
		}
		m.emit(sessionID, "error", map[string]any{"message": msg})
		_ = m.store.SetSessionStatus(sessionID, "failed", msg)
	}
	m.bus.Changed()
	go m.pump()
}

// Release closes an idle session's process (e.g. before the user resumes it in a
// terminal). The next message resumes it in a new process. It fails while busy.
func (m *Manager) Release(sessionID string) error {
	m.mu.Lock()
	for _, q := range m.queue {
		if q.sessionID == sessionID {
			m.mu.Unlock()
			return ErrBusy
		}
	}
	l := m.live[sessionID]
	if l == nil {
		m.mu.Unlock()
		return nil
	}
	if l.busy {
		m.mu.Unlock()
		return ErrBusy
	}
	delete(m.live, sessionID)
	if l.idle != nil {
		l.idle.Stop()
	}
	proc := l.proc
	m.mu.Unlock()
	if proc != nil {
		proc.Shutdown(5 * time.Second)
	}
	return nil
}

// AlwaysRules returns the rules "Always allow" would save for a pending approval.
func (m *Manager) AlwaysRules(approvalID string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.approvals[approvalID]; p != nil {
		return p.always
	}
	return nil
}

// Decide answers a pending approval. always also saves its allow rules.
func (m *Manager) Decide(approvalID string, allow, always bool, message string) error {
	m.mu.Lock()
	p := m.approvals[approvalID]
	delete(m.approvals, approvalID)
	stillWaiting := false
	if p != nil {
		for _, other := range m.approvals {
			if other.sessionID == p.sessionID {
				stillWaiting = true
			}
		}
	}
	m.mu.Unlock()
	if p == nil {
		return errors.New("this approval is no longer pending")
	}
	status := "denied"
	if allow {
		status = "allowed"
	}
	_ = m.store.SetApprovalStatus(approvalID, status)
	if !stillWaiting {
		_ = m.store.SetSessionStatus(p.sessionID, "running", "")
	}
	p.respond(claude.Decision{Allow: allow, Always: allow && always, Message: message})
	m.bus.Changed()
	return nil
}

// Interrupt stops the current turn (or removes a queued one).
func (m *Manager) Interrupt(sessionID string) error {
	m.mu.Lock()
	for i, q := range m.queue {
		if q.sessionID == sessionID {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			m.mu.Unlock()
			_ = m.store.SetSessionStatus(sessionID, "interrupted", "")
			m.bus.Changed()
			return nil
		}
	}
	l := m.live[sessionID]
	if l == nil || !l.busy || l.proc == nil {
		m.mu.Unlock()
		return nil
	}
	l.interrupting = true
	var pending []string
	for id, p := range m.approvals {
		if p.sessionID == sessionID {
			pending = append(pending, id)
		}
	}
	proc := l.proc
	m.mu.Unlock()
	for _, id := range pending {
		_ = m.Decide(id, false, false, "Interrupted by the user.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := proc.Interrupt(ctx); err != nil {
		// The CLI didn't acknowledge: stop the process; the session can be resumed later.
		proc.Shutdown(2 * time.Second)
		return fmt.Errorf("interrupt: %w", err)
	}
	return nil
}

// Running is how many turns are in flight, and Queued how many wait for a slot.
func (m *Manager) Counts() (running, queued int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runningCount(), len(m.queue)
}

// Shutdown stops all processes; their sessions can be resumed after restart.
func (m *Manager) Shutdown() {
	m.mu.Lock()
	m.closed = true
	procs := []Proc{}
	for _, l := range m.live {
		if l.proc != nil {
			procs = append(procs, l.proc)
		}
		if l.idle != nil {
			l.idle.Stop()
		}
	}
	for id, p := range m.approvals {
		p.respond(claude.Decision{Allow: false, Message: "layer8 is quitting."})
		delete(m.approvals, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range procs {
		wg.Add(1)
		go func(p Proc) { defer wg.Done(); p.Shutdown(3 * time.Second) }(p)
	}
	wg.Wait()
	_ = m.store.RecoverSessions()
}

func (m *Manager) emit(sessionID, typ string, data map[string]any) {
	e, err := m.store.AddEvent(sessionID, typ, data)
	if err == nil {
		m.bus.Event(e)
	}
}
