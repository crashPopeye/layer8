// Package session runs a task's Claude sessions: one claude process per live
// session, a message queue, approvals, and the conversation stored as events.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/google/uuid"
)

// ErrInvalid marks errors caused by bad input or a session in the wrong state.
var ErrInvalid = errors.New("invalid input")

// PermissionModes are the approval modes a session can run in; "" keeps the user's default.
var PermissionModes = []string{"default", "acceptEdits", "auto", "bypassPermissions"}

// Proc is the part of a claude process the Manager drives.
type Proc interface {
	Send(text string) error
	Interrupt() error
	SetModel(model string) error
	SetPermissionMode(mode string) error
	Close() error
}

// Spawner starts a claude process; tests substitute a scripted one.
type Spawner func(claude.Options, claude.Handlers) (Proc, error)

// Claude spawns the real claude CLI.
func Claude(o claude.Options, h claude.Handlers) (Proc, error) {
	p, err := claude.Spawn(o, h)
	if err != nil {
		return nil, err
	}
	return p, nil
}

// Config is what every spawned session shares.
type Config struct {
	PluginDir     string
	ArtifactsRoot string
	LogDir        string
	Env           []string
}

// Manager owns every live session process and the policy around it.
type Manager struct {
	// OnIdle, when set, runs in its own goroutine each time a session finishes
	// a turn with nothing queued; set it before the first session starts.
	OnIdle func(store.Session)

	store   *store.Store
	publish func(any)
	spawn   Spawner
	cfg     Config
	refresh *coalescer

	mu   sync.Mutex
	live map[string]*live
}

type live struct {
	proc     Proc
	queue    []string
	draft    string
	baseCost float64
	waiting  map[string]chan claude.PermissionDecision
	stderr   *os.File
}

func NewManager(st *store.Store, publish func(any), spawn Spawner, cfg Config) *Manager {
	m := &Manager{store: st, publish: publish, spawn: spawn, cfg: cfg, live: map[string]*live{}}
	m.refresh = newCoalescer(RefreshWindow, func(taskID string) {
		publish(map[string]any{"kind": "artifacts", "id": taskID})
		publish(map[string]any{"kind": "changes", "id": taskID})
	})
	return m
}

// View is a session with what only the live process knows.
type View struct {
	store.Session
	Queued int `json:"queued"`
}

// Sessions lists a task's sessions, oldest first.
func (m *Manager) Sessions(taskRef string) ([]View, error) {
	t, err := m.store.Task(taskRef)
	if err != nil {
		return nil, err
	}
	sessions, err := m.store.Sessions(t.ID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	views := make([]View, len(sessions))
	for i, s := range sessions {
		views[i] = View{Session: s}
		if l := m.live[s.ID]; l != nil {
			views[i].Queued = len(l.queue)
		}
	}
	return views, nil
}

// StartInput describes a new session; a non-empty Prompt is sent as its first message.
type StartInput struct {
	Label          string `json:"label"`
	Prompt         string `json:"prompt"`
	Model          string `json:"model"`
	Effort         string `json:"effort"`
	PermissionMode string `json:"permissionMode"`
}

// Start creates a session in the task and sends its first message.
func (m *Manager) Start(ctx context.Context, taskRef string, in StartInput) (store.Session, error) {
	if in.PermissionMode != "" && !slices.Contains(PermissionModes, in.PermissionMode) {
		return store.Session{}, fmt.Errorf("%w: unknown permission mode %q", ErrInvalid, in.PermissionMode)
	}
	t, err := m.store.Task(taskRef)
	if err != nil {
		return store.Session{}, err
	}
	s := store.Session{
		ID:             uuid.Must(uuid.NewV7()).String(),
		TaskID:         t.ID,
		Label:          strings.TrimSpace(in.Label),
		Phase:          t.Phase,
		Model:          in.Model,
		Effort:         in.Effort,
		PermissionMode: in.PermissionMode,
		Status:         "ready_for_input",
		CreatedAt:      time.Now().UnixMilli(),
	}
	if s.Label == "" {
		s.Label = "session"
	}
	if err := m.store.InsertSession(s); err != nil {
		return store.Session{}, err
	}
	m.notify(s, "session")
	if strings.TrimSpace(in.Prompt) != "" {
		if _, err := m.Send(ctx, s.ID, in.Prompt); err != nil {
			return s, err
		}
	}
	return m.store.Session(s.ID)
}

// Send writes a user message to the session, spawning its process with --resume
// if it has none. During a turn the message is queued and sent after the turn's result.
func (m *Manager) Send(ctx context.Context, sessionID, text string) (queued bool, err error) {
	if strings.TrimSpace(text) == "" {
		return false, fmt.Errorf("%w: empty message", ErrInvalid)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.store.Session(sessionID)
	if err != nil {
		return false, err
	}
	l := m.live[sessionID]
	if l != nil && s.InTurn {
		l.queue = append(l.queue, text)
		m.notify(s, "session")
		return true, nil
	}
	s.Status = "running"
	if l == nil {
		if l, err = m.spawnLocked(s); err != nil {
			return false, err
		}
		s.Status = "starting"
	}
	return false, m.beginTurnLocked(&s, l, text)
}

func (m *Manager) beginTurnLocked(s *store.Session, l *live, text string) error {
	m.emit(s.ID, "user", "", map[string]string{"text": text})
	s.InTurn = true
	if err := m.save(*s); err != nil {
		return err
	}
	return l.proc.Send(text)
}

func (m *Manager) spawnLocked(s store.Session) (*live, error) {
	t, err := m.store.Task(s.TaskID)
	if err != nil {
		return nil, err
	}
	p, err := m.store.Project(t.ProjectID)
	if err != nil {
		return nil, err
	}
	var parent *store.Task
	if t.ParentID != "" {
		if pt, err := m.store.Task(t.ParentID); err == nil {
			parent = &pt
		}
	}
	artifacts := filepath.Join(m.cfg.ArtifactsRoot, t.ID)
	l := &live{baseCost: s.CostUSD, waiting: map[string]chan claude.PermissionDecision{}}
	if m.cfg.LogDir != "" {
		if err := os.MkdirAll(m.cfg.LogDir, 0o755); err == nil {
			l.stderr, _ = os.OpenFile(filepath.Join(m.cfg.LogDir, "claude-"+s.ID+".stderr"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		}
	}
	opts := claude.Options{
		Cwd:                t.Worktree,
		Resume:             s.ClaudeSessionID,
		Model:              s.Model,
		Effort:             s.Effort,
		PermissionMode:     s.PermissionMode,
		PluginDir:          m.cfg.PluginDir,
		AddDirs:            []string{artifacts},
		Env:                append(slices.Clone(m.cfg.Env), "L8_SESSION_ID="+s.ID, "L8_TASK_ID="+t.ID),
		SystemPromptAppend: ContextBlock(ContextData{Session: s, Task: t, Parent: parent, Project: p, ArtifactsDir: artifacts}),
		Hooks:              hooks,
	}
	if l.stderr != nil {
		opts.Stderr = l.stderr
	}
	proc, err := m.spawn(opts, claude.Handlers{
		OnMessage:    func(msg claude.Message) { m.onMessage(s.ID, l, msg) },
		OnPermission: func(req claude.PermissionRequest) claude.PermissionDecision { return m.onPermission(s.ID, l, req) },
		OnHook:       func(call claude.HookCall) claude.HookResult { return m.onHook(t.ID, artifacts, call) },
		OnExit:       func(err error) { m.onExit(s.ID, l, err) },
	})
	if err != nil {
		if l.stderr != nil {
			l.stderr.Close()
		}
		return nil, err
	}
	l.proc = proc
	m.live[s.ID] = l
	return l, nil
}

// Decision is the user's answer to an approval: allow, always (allow and save
// the suggested rules to user settings) or deny.
type Decision struct {
	Decision string `json:"decision"`
	Message  string `json:"message"`
}

// Decide answers a pending approval.
func (m *Manager) Decide(ctx context.Context, approvalID string, d Decision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, err := m.store.Approval(approvalID)
	if err != nil {
		return err
	}
	if a.Status != "pending" {
		return fmt.Errorf("%w: approval is already %s", ErrInvalid, a.Status)
	}
	var answer claude.PermissionDecision
	status := "allowed"
	switch d.Decision {
	case "allow":
		answer.Allow = true
	case "always":
		answer.Allow = true
		var suggestions []json.RawMessage
		json.Unmarshal(a.Suggestions, &suggestions)
		answer.UpdatedPermissions = toUserSettings(suggestions)
	case "deny":
		answer.Message, status = d.Message, "denied"
	default:
		return fmt.Errorf("%w: decision must be allow, always or deny", ErrInvalid)
	}
	l := m.live[a.SessionID]
	if l == nil || l.waiting[approvalID] == nil {
		m.store.SetApprovalStatus(approvalID, "stale")
		m.publish(map[string]any{"kind": "approval", "id": approvalID, "sessionId": a.SessionID})
		return fmt.Errorf("%w: the session that asked is no longer running", ErrInvalid)
	}
	l.waiting[approvalID] <- answer
	delete(l.waiting, approvalID)
	if err := m.store.SetApprovalStatus(approvalID, status); err != nil {
		return err
	}
	m.publish(map[string]any{"kind": "approval", "id": approvalID, "sessionId": a.SessionID})
	if len(l.waiting) == 0 {
		s, err := m.store.Session(a.SessionID)
		if err != nil {
			return err
		}
		s.Status = "running"
		return m.save(s)
	}
	return nil
}

// toUserSettings retargets permission suggestions at the user's settings, so
// "always" outlives the task's worktree.
func toUserSettings(suggestions []json.RawMessage) []json.RawMessage {
	out := make([]json.RawMessage, 0, len(suggestions))
	for _, raw := range suggestions {
		var s map[string]any
		if json.Unmarshal(raw, &s) != nil {
			continue
		}
		s["destination"] = "userSettings"
		if b, err := json.Marshal(s); err == nil {
			out = append(out, b)
		}
	}
	return out
}

// Interrupt stops the session's current turn, drops its queue and denies its pending approvals.
func (m *Manager) Interrupt(sessionID string) error {
	m.mu.Lock()
	l := m.live[sessionID]
	if l == nil {
		m.mu.Unlock()
		return fmt.Errorf("%w: session is not running", ErrInvalid)
	}
	l.queue = nil
	m.denyWaitingLocked(sessionID, l, "denied", "The user interrupted the turn.")
	m.mu.Unlock()
	return l.proc.Interrupt()
}

// Stop closes the session's process; a later Send resumes it.
func (m *Manager) Stop(sessionID string) error {
	m.mu.Lock()
	s, err := m.store.Session(sessionID)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	l := m.live[sessionID]
	delete(m.live, sessionID)
	if l != nil {
		m.denyWaitingLocked(sessionID, l, "stale", "The session was stopped.")
	}
	s.Status, s.InTurn = "stopped", false
	err = m.save(s)
	m.mu.Unlock()
	if l != nil {
		l.proc.Close()
	}
	return err
}

func (m *Manager) denyWaitingLocked(sessionID string, l *live, status, message string) {
	for id, ch := range l.waiting {
		ch <- claude.PermissionDecision{Message: message}
		delete(l.waiting, id)
		m.store.SetApprovalStatus(id, status)
		m.publish(map[string]any{"kind": "approval", "id": id, "sessionId": sessionID})
	}
}

func (m *Manager) onPermission(sessionID string, l *live, req claude.PermissionRequest) claude.PermissionDecision {
	m.mu.Lock()
	if m.live[sessionID] != l {
		m.mu.Unlock()
		return claude.PermissionDecision{Message: "The session was stopped."}
	}
	suggestions, _ := json.Marshal(req.Suggestions)
	a := store.Approval{
		ID:          uuid.Must(uuid.NewV7()).String(),
		SessionID:   sessionID,
		RequestID:   req.RequestID,
		ToolUseID:   req.ToolUseID,
		ToolName:    req.ToolName,
		Input:       req.Input,
		Suggestions: suggestions,
		Status:      "pending",
		CreatedAt:   time.Now().UnixMilli(),
	}
	ch := make(chan claude.PermissionDecision, 1)
	if err := m.store.InsertApproval(a); err != nil {
		m.mu.Unlock()
		return claude.PermissionDecision{Message: "layer8 could not record the approval: " + err.Error()}
	}
	l.waiting[a.ID] = ch
	m.emit(sessionID, "approval", "", map[string]any{"approvalId": a.ID, "tool": a.ToolName, "input": a.Input})
	if s, err := m.store.Session(sessionID); err == nil {
		s.Status = "waiting_approval"
		m.save(s)
		m.notify(s, "attention")
	}
	m.publish(map[string]any{"kind": "approval", "id": a.ID, "sessionId": sessionID})
	m.mu.Unlock()
	return <-ch
}

func (m *Manager) onExit(sessionID string, l *live, exitErr error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.stderr != nil {
		l.stderr.Close()
	}
	if m.live[sessionID] != l {
		return
	}
	delete(m.live, sessionID)
	m.denyWaitingLocked(sessionID, l, "stale", "The session exited.")
	s, err := m.store.Session(sessionID)
	if err != nil {
		return
	}
	if s.InTurn {
		s.Status, s.InTurn = "failed", false
		text := "claude exited during the turn"
		if exitErr != nil {
			text += ": " + exitErr.Error()
		}
		if m.cfg.LogDir != "" {
			text += " (stderr in " + filepath.Join(m.cfg.LogDir, "claude-"+sessionID+".stderr") + ")"
		}
		m.emit(sessionID, "system", "", map[string]string{"subtype": "exit", "text": text})
	} else if s.Status != "stopped" {
		s.Status = "ready_for_input"
	}
	m.save(s)
}

// emit stores one conversation event and announces it.
func (m *Manager) emit(sessionID, kind, parentToolUseID string, body any) {
	e, err := m.store.AppendEvent(sessionID, kind, parentToolUseID, body)
	if err != nil {
		return
	}
	m.publish(map[string]any{"kind": "event", "id": sessionID, "seq": e.Seq})
}

func (m *Manager) save(s store.Session) error {
	if err := m.store.SaveSession(s); err != nil {
		return err
	}
	m.notify(s, "session")
	return nil
}

func (m *Manager) notify(s store.Session, kind string) {
	m.publish(map[string]any{"kind": kind, "id": s.ID, "taskId": s.TaskID, "status": s.Status, "label": s.Label})
}
