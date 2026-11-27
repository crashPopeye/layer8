// Package sessions runs agent sessions inside tasks: the session kind, its event log
// and approvals, and a manager that keeps a bounded number of agent processes alive.
package sessions

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Options configures Register.
type Options struct {
	// Harnesses are the agent CLIs sessions can run, by name.
	Harnesses map[string]harness.Harness
	// Max is the number of live agent processes; more sessions queue. Default 6.
	Max int
	// IdleAfter closes a process idle that long. Default 10 minutes.
	IdleAfter time.Duration
	// Grace is how long a closing process may take before it is killed. Default 5 seconds.
	Grace time.Duration
	// MCPURL and Token reach the daemon's MCP endpoint from a session.
	MCPURL, Token string
	// AutoApproveDo lets sessions call the layer8 do tool without an approval.
	AutoApproveDo bool
	// Terminal is the macOS application open-terminal uses.
	Terminal string
	// Publish streams a live event; nil drops them.
	Publish func(name string, data any)
	// Open runs macOS open with args; nil runs /usr/bin/open.
	Open func(ctx context.Context, args ...string) error
}

// Manager owns the session kind and the agent processes behind it.
type Manager struct {
	db    *sql.DB
	tree  *tree.Tree
	tasks *tasks.Service
	o     Options

	mu     sync.Mutex
	runs   map[string]*run
	queue  []*run
	probes map[string]*probe
}

// Event is one transcript entry as stored and streamed. Partial events have no id.
type Event struct {
	ID      int64             `json:"id,omitempty"`
	Type    harness.EventType `json:"type"`
	At      string            `json:"at,omitempty"`
	Partial bool              `json:"partial,omitempty"`
	Data    any               `json:"data"`
}

func addr(id8 string) tree.Addr { return tree.Addr("session:" + id8) }

// Register adds the session kind, the start-session task action, sessions as task children
// and the "here" resolver, after marking sessions left live by a previous daemon idle.
func Register(t *tree.Tree, db *sql.DB, ts *tasks.Service, o Options) *Manager {
	o.Max = cmp.Or(o.Max, 6)
	o.IdleAfter = cmp.Or(o.IdleAfter, 10*time.Minute)
	o.Grace = cmp.Or(o.Grace, 5*time.Second)
	if o.Publish == nil {
		o.Publish = func(string, any) {}
	}
	if o.Open == nil {
		o.Open = func(ctx context.Context, args ...string) error {
			if out, err := exec.CommandContext(ctx, "open", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("open: %v %s", err, out)
			}
			return nil
		}
	}
	m := &Manager{db: db, tree: t, tasks: ts, o: o, runs: map[string]*run{}, probes: map[string]*probe{}}
	db.Exec(`UPDATE sessions SET status = 'idle' WHERE status IN ('queued', 'starting', 'running', 'waiting')`)
	db.Exec(`UPDATE approvals SET status = 'expired' WHERE status = 'pending'`)

	harnesses, _ := json.Marshal(slices.Sorted(maps.Keys(o.Harnesses)))
	t.Register(tree.Kind{
		Name: "session",
		Get:  m.get,
		Actions: []tree.Action{
			{Name: "say", Help: "Send a message, resuming the agent if needed", Run: m.sayAction,
				Args: json.RawMessage(`{"type":"object","required":["text"],"properties":{
					"text":{"type":"string"},
					"files":{"type":"array","items":{"type":"string"},"description":"Paths the agent should read"},
					"now":{"type":"boolean","description":"Deliver at once instead of after the running turn"},
					"images":{"type":"array","items":{"type":"object","required":["mediaType","data"],"properties":{"mediaType":{"type":"string"},"data":{"type":"string","description":"base64"}}}}}}`)},
			{Name: "send-now", Help: "Deliver a queued message at once instead of after the running turn", When: statusIn("queued", "starting", "running", "waiting"), Run: m.sendNowAction,
				Args: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string","description":"The queued user message's id; defaults to the oldest"}}}`)},
			{Name: "interrupt", Help: "Stop the current turn", When: statusIn("running", "waiting"), Run: m.interruptAction},
			{Name: "allow", Help: "Allow a pending tool call (user only)", When: statusIn("waiting"), Run: m.allow,
				Args: json.RawMessage(`{"type":"object","properties":{"approval":{"type":"integer","description":"Defaults to the oldest pending"},"always":{"type":"boolean"}}}`)},
			{Name: "deny", Help: "Deny a pending tool call (user only)", When: statusIn("waiting"), Run: m.deny,
				Args: json.RawMessage(`{"type":"object","properties":{"approval":{"type":"integer","description":"Defaults to the oldest pending"},"message":{"type":"string"}}}`)},
			{Name: "close", Help: "End the session and its agent process", When: func(n tree.Node) bool { return n.Fields["status"] != "done" }, Run: m.closeAction},
			{Name: "set", Help: "Change model, effort or permission mode from the next turn", Run: m.set,
				Args: json.RawMessage(`{"type":"object","properties":{"model":{"type":"string"},"effort":{"type":"string"},"permissionMode":{"type":"string"}}}`)},
			{Name: "fork", Help: "Copy this conversation into a new session", When: resumable, Run: m.fork},
			{Name: "open-terminal", Help: "Continue this conversation in the terminal", Run: m.openTerminal,
				When: func(n tree.Node) bool {
					return resumable(n) && resumeCommand[n.Fields["harness"].(string)] != "" && statusIn("idle", "done", "failed")(n)
				}},
		},
	})
	t.Extend("task", tree.Action{
		Name: "start-session", Help: "Start an agent session in this task's worktree", Run: m.startSession,
		When: func(n tree.Node) bool { return n.Fields["status"] == "active" },
		Args: json.RawMessage(`{"type":"object","properties":{
			"prompt":{"type":"string"},
			"skill":{"type":"string","description":"Skill or slash command to run, without the slash"},
			"harness":{"enum":` + string(harnesses) + `},
			"model":{"type":"string"},"effort":{"type":"string"},"permissionMode":{"type":"string"},
			"title":{"type":"string"}}}`),
	})
	t.AddChildren("task", m.taskSessions)
	t.Here = m.here
	return m
}

func statusIn(statuses ...string) func(tree.Node) bool {
	return func(n tree.Node) bool { return slices.Contains(statuses, n.Fields["status"].(string)) }
}

func resumable(n tree.Node) bool { return n.Fields["harnessSessionId"] != "" }

// row is a session as stored.
type row struct {
	ID, ID8, TaskSlug, Harness, Model, Effort, PermissionMode, HarnessSessionID string
	Status, Title, Skill, WorkflowStep, Error, CreatedBy, CreatedAt             string
	Fork                                                                        bool
	CostUSD                                                                     float64
	ContextTokens                                                               int
	Capabilities                                                                sql.NullString
}

func (m *Manager) load(ctx context.Context, id8 string) (row, error) {
	var s row
	err := m.db.QueryRowContext(ctx, `SELECT s.id, s.id8, t.slug, s.harness, s.model, s.effort, s.permission_mode, s.harness_session_id,
		s.status, s.title, s.skill, s.workflow_step, s.error, s.created_by, s.created_at, s.fork, s.cost_usd, s.context_tokens, s.capabilities
		FROM sessions s JOIN tasks t ON t.id = s.task_id WHERE s.id8 = ?`, id8).Scan(
		&s.ID, &s.ID8, &s.TaskSlug, &s.Harness, &s.Model, &s.Effort, &s.PermissionMode, &s.HarnessSessionID,
		&s.Status, &s.Title, &s.Skill, &s.WorkflowStep, &s.Error, &s.CreatedBy, &s.CreatedAt, &s.Fork, &s.CostUSD, &s.ContextTokens, &s.Capabilities)
	if errors.Is(err, sql.ErrNoRows) {
		return row{}, fmt.Errorf("%w: %s", tree.ErrNotFound, addr(id8))
	}
	return s, err
}

func (m *Manager) get(ctx context.Context, a tree.Addr) (tree.Node, error) {
	s, err := m.load(ctx, a.Path())
	if err != nil {
		return tree.Node{}, err
	}
	approvals, err := m.pendingApprovals(ctx, s.ID)
	if err != nil {
		return tree.Node{}, err
	}
	var caps any
	if s.Capabilities.Valid {
		caps = json.RawMessage(s.Capabilities.String)
	}
	return tree.Node{Title: s.Title, Fields: map[string]any{
		"id": s.ID, "task": tasks.AddrOf(s.TaskSlug), "harness": s.Harness, "model": s.Model, "effort": s.Effort,
		"permissionMode": s.PermissionMode, "harnessSessionId": s.HarnessSessionID, "status": s.Status, "skill": s.Skill,
		"workflowStep": s.WorkflowStep, "costUsd": s.CostUSD, "contextTokens": s.ContextTokens, "error": s.Error,
		"createdBy": s.CreatedBy, "createdAt": s.CreatedAt, "capabilities": caps, "approvals": approvals,
	}}, nil
}

// approval is a tool call waiting for the user.
type approval struct {
	ID int64 `json:"id"`
	harness.PermissionRequest
	At string `json:"at"`
}

func (m *Manager) pendingApprovals(ctx context.Context, sessionID string) ([]approval, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id, request, created_at FROM approvals WHERE session_id = ? AND status = 'pending' ORDER BY id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []approval{}
	for rows.Next() {
		var a approval
		var req string
		if err := rows.Scan(&a.ID, &req, &a.At); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(req), &a.PermissionRequest)
		list = append(list, a)
	}
	return list, rows.Err()
}

func (m *Manager) taskSessions(ctx context.Context, a tree.Addr) ([]tree.Summary, error) {
	rows, err := m.db.QueryContext(ctx, `SELECT id8, title, status FROM sessions
		WHERE task_id = (SELECT id FROM tasks WHERE slug = ?) ORDER BY created_at, rowid`, strings.ReplaceAll(a.Path(), "/", "--"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []tree.Summary
	for rows.Next() {
		var id8, title, status string
		if err := rows.Scan(&id8, &title, &status); err != nil {
			return nil, err
		}
		list = append(list, tree.Summary{Addr: addr(id8), Kind: "session", Title: title, Status: status})
	}
	return list, rows.Err()
}

func (m *Manager) here(ctx context.Context, by tree.Actor) (tree.Addr, error) {
	if by.Kind != "session" {
		return "", errors.New("here is only defined inside a layer8 session")
	}
	s, err := m.load(ctx, by.ID)
	if err != nil {
		return "", err
	}
	return tasks.AddrOf(s.TaskSlug), nil
}

// insert stores a new session row built by query from a fresh id and id8, retrying on an id8 clash.
func (m *Manager) insert(ctx context.Context, query string, args ...any) (string, error) {
	for {
		id := uuid.NewString()
		_, err := m.db.ExecContext(ctx, query, append([]any{id, id[:8]}, args...)...)
		if err == nil {
			return id[:8], nil
		}
		if !strings.Contains(err.Error(), "UNIQUE") {
			return "", err
		}
	}
}

func (m *Manager) startSession(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Prompt, Skill, Harness, Model, Effort, PermissionMode, Title string }
	json.Unmarshal(args, &a)
	a.Harness = cmp.Or(a.Harness, "claude")
	if _, ok := m.o.Harnesses[a.Harness]; !ok {
		return tree.Result{}, fmt.Errorf("unknown harness %q", a.Harness)
	}
	text := strings.TrimSpace(a.Prompt)
	if a.Skill != "" {
		text = strings.TrimSpace("/" + strings.TrimPrefix(a.Skill, "/") + " " + text)
	}
	firstLine, _, _ := strings.Cut(a.Prompt, "\n")
	title := cmp.Or(a.Title, a.Skill, truncate(strings.TrimSpace(firstLine), 60), "Session")
	slug := strings.ReplaceAll(n.Addr.Path(), "/", "--")
	id8, err := m.insert(ctx, `INSERT INTO sessions (id, id8, task_id, harness, model, effort, permission_mode, title, skill, created_by)
		VALUES (?, ?, (SELECT id FROM tasks WHERE slug = ?), ?, ?, ?, ?, ?, ?, ?)`,
		slug, a.Harness, a.Model, a.Effort, a.PermissionMode, title, a.Skill, by.String())
	if err != nil {
		return tree.Result{}, err
	}
	if text != "" {
		if _, err := m.say(id8, harness.UserMessage{Text: text}, by); err != nil {
			return tree.Result{}, err
		}
	}
	return tree.Result{Addr: addr(id8), Message: fmt.Sprintf("started %s in %s", addr(id8), n.Addr), Data: map[string]string{"session": id8}}, nil
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func (m *Manager) sayAction(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct {
		Text   string
		Files  []string
		Images []struct{ MediaType, Data string }
		Now    bool
	}
	json.Unmarshal(args, &a)
	msg := harness.UserMessage{Text: a.Text, Files: a.Files}
	for i, img := range a.Images {
		data, err := base64.StdEncoding.DecodeString(img.Data)
		if err != nil {
			return tree.Result{}, fmt.Errorf("images[%d].data: %w", i, err)
		}
		msg.Images = append(msg.Images, harness.Image{MediaType: img.MediaType, Data: data})
	}
	id, err := m.say(n.Addr.Path(), msg, by)
	if err != nil {
		return tree.Result{}, err
	}
	if a.Now {
		if err := m.sendNow(ctx, n.Addr.Path(), id); err != nil {
			return tree.Result{}, err
		}
	}
	return tree.Result{Message: "sent to " + string(n.Addr), Data: map[string]string{"message": id}}, nil
}

func (m *Manager) sendNowAction(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ Message string }
	json.Unmarshal(args, &a)
	if err := m.sendNow(ctx, n.Addr.Path(), a.Message); err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: "sent now to " + string(n.Addr)}, nil
}

func (m *Manager) interruptAction(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	if err := m.interrupt(ctx, n.Addr.Path()); err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: "interrupted " + string(n.Addr)}, nil
}

func (m *Manager) allow(_ context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct {
		Approval int64
		Always   bool
	}
	json.Unmarshal(args, &a)
	id, err := m.answer(n.Addr.Path(), a.Approval, harness.Decision{Allow: true, Always: a.Always}, by)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("allowed approval %d", id)}, nil
}

func (m *Manager) deny(_ context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct {
		Approval int64
		Message  string
	}
	json.Unmarshal(args, &a)
	id, err := m.answer(n.Addr.Path(), a.Approval, harness.Decision{Message: a.Message}, by)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("denied approval %d", id)}, nil
}

func (m *Manager) closeAction(_ context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	m.closeSession(n.Addr.Path())
	return tree.Result{Message: "closing " + string(n.Addr)}, nil
}

func (m *Manager) set(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ Model, Effort, PermissionMode *string }
	json.Unmarshal(args, &a)
	_, err := m.db.ExecContext(ctx, `UPDATE sessions SET model = coalesce(?, model), effort = coalesce(?, effort),
		permission_mode = coalesce(?, permission_mode) WHERE id8 = ?`, a.Model, a.Effort, a.PermissionMode, n.Addr.Path())
	if err != nil {
		return tree.Result{}, err
	}
	m.restartAfterTurn(n.Addr.Path())
	return tree.Result{Message: "settings apply from the next turn"}, nil
}

func (m *Manager) fork(ctx context.Context, n tree.Node, _ json.RawMessage, by tree.Actor) (tree.Result, error) {
	id8, err := m.insert(ctx, `INSERT INTO sessions (id, id8, task_id, harness, model, effort, permission_mode, harness_session_id, fork, title, skill, created_by)
		SELECT ?, ?, task_id, harness, model, effort, permission_mode, harness_session_id, 1, title || ' (fork)', skill, ? FROM sessions WHERE id8 = ?`,
		by.String(), n.Addr.Path())
	if err != nil {
		return tree.Result{}, err
	}
	_, err = m.db.ExecContext(ctx, `INSERT INTO session_events (session_id, type, at, data)
		SELECT (SELECT id FROM sessions WHERE id8 = ?), type, at, data FROM session_events
		WHERE session_id = (SELECT id FROM sessions WHERE id8 = ?) ORDER BY id`, id8, n.Addr.Path())
	if err != nil {
		return tree.Result{}, err
	}
	m.tree.Changed(n.Fields["task"].(tree.Addr))
	return tree.Result{Addr: addr(id8), Message: fmt.Sprintf("forked %s into %s", n.Addr, addr(id8))}, nil
}

func (m *Manager) openTerminal(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	t, err := m.tasks.Lookup(ctx, n.Fields["task"].(tree.Addr))
	if err != nil {
		return tree.Result{}, err
	}
	m.mu.Lock()
	if r := m.runs[n.Addr.Path()]; r != nil && r.idleLive() {
		m.closeRun(r, "idle")
	}
	m.mu.Unlock()
	script := filepath.Join(os.TempDir(), "layer8-"+n.Addr.Path()+".command")
	body := fmt.Sprintf("#!/bin/zsh -l\ncd %s && exec %s %s\n", quote(t.Worktree), resumeCommand[n.Fields["harness"].(string)], quote(n.Fields["harnessSessionId"].(string)))
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		return tree.Result{}, err
	}
	if err := m.o.Open(ctx, "-a", cmp.Or(m.o.Terminal, "Terminal"), script); err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: "opened " + string(n.Addr) + " in the terminal"}, nil
}

// resumeCommand is the shell command that continues a harness's conversation by id.
var resumeCommand = map[string]string{"claude": "claude --resume", "codex": "codex resume"}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
