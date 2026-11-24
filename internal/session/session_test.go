package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/store"
)

// fakeProc records what the Manager writes; the test plays the CLI through its handlers.
type fakeProc struct {
	mu     sync.Mutex
	opts   claude.Options
	h      claude.Handlers
	sent   []string
	closed bool
}

func (p *fakeProc) Send(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, text)
	return nil
}
func (p *fakeProc) Interrupt() error                    { return nil }
func (p *fakeProc) SetModel(string) error               { return nil }
func (p *fakeProc) SetPermissionMode(mode string) error { return nil }
func (p *fakeProc) Close() error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.h.OnExit(nil)
	return nil
}

func (p *fakeProc) Sent() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.sent)
}

type fixture struct {
	m     *Manager
	st    *store.Store
	procs []*fakeProc
	task  store.Task
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	p := store.Project{ID: "p1", Name: "layer8", Path: "/w/layer8", BaseBranch: "main", Instructions: "Use Go.", CreatedAt: 1}
	task := store.Task{ID: "t1", ProjectID: "p1", Slug: "smoke", Title: "Smoke", Workflow: "prd_tdd", Status: "active",
		Branch: "l8/smoke", Worktree: home, Autonomous: true, CreatedAt: 1}
	if err := st.InsertProject(p); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertTask(task); err != nil {
		t.Fatal(err)
	}
	f := &fixture{st: st, task: task}
	f.m = NewManager(st, func(any) {}, func(o claude.Options, h claude.Handlers) (Proc, error) {
		p := &fakeProc{opts: o, h: h}
		f.procs = append(f.procs, p)
		return p, nil
	}, Config{ArtifactsRoot: filepath.Join(home, "artifacts"), PluginDir: "/plug"})
	return f
}

func (f *fixture) session(t *testing.T, id string) store.Session {
	t.Helper()
	s, err := f.st.Session(id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (f *fixture) events(t *testing.T, id string) []store.Event {
	t.Helper()
	events, err := f.st.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func result(cost float64) claude.Message {
	return claude.Message{Type: "result", Subtype: "success", TotalCostUSD: cost}
}

func TestQueueDuringTurn(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s, err := f.m.Start(ctx, "smoke", StartInput{Label: "research", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != "starting" || !s.InTurn {
		t.Fatalf("after start: %+v", s)
	}
	proc := f.procs[0]
	if !slices.Contains(proc.opts.AddDirs, filepath.Join(f.m.cfg.ArtifactsRoot, "t1")) || proc.opts.PluginDir != "/plug" ||
		!slices.Contains(proc.opts.Env, "L8_TASK_ID=t1") || !slices.Contains(proc.opts.Env, "L8_SESSION_ID="+s.ID) {
		t.Fatalf("spawn options %+v", proc.opts)
	}
	proc.h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "claude-1"})
	if got := f.session(t, s.ID); got.Status != "running" || got.ClaudeSessionID != "claude-1" {
		t.Fatalf("after init: %+v", got)
	}

	queued, err := f.m.Send(ctx, s.ID, "second")
	if err != nil || !queued {
		t.Fatalf("Send during turn = %v, %v", queued, err)
	}
	if views, _ := f.m.Sessions("t1"); views[0].Queued != 1 {
		t.Fatalf("queued = %d", views[0].Queued)
	}
	if got := proc.Sent(); !slices.Equal(got, []string{"first"}) {
		t.Fatalf("sent before result = %v", got)
	}

	proc.h.OnMessage(result(0.10))
	if got := proc.Sent(); !slices.Equal(got, []string{"first", "second"}) {
		t.Fatalf("sent after result = %v", got)
	}
	if got := f.session(t, s.ID); got.Status != "running" || !got.InTurn {
		t.Fatalf("after first result: %+v", got)
	}
	proc.h.OnMessage(result(0.25))
	got := f.session(t, s.ID)
	if got.Status != "ready_for_input" || got.InTurn || got.CostUSD != 0.25 {
		t.Fatalf("after second result: %+v", got)
	}

	var kinds []string
	for _, e := range f.events(t, s.ID) {
		kinds = append(kinds, e.Kind)
	}
	if want := []string{"user", "result", "user", "result"}; !slices.Equal(kinds, want) {
		t.Fatalf("events = %v, want %v", kinds, want)
	}
}

func TestAlwaysWritesUserSettings(t *testing.T) {
	f := newFixture(t)
	s, err := f.m.Start(context.Background(), "t1", StartInput{Prompt: "run go version"})
	if err != nil {
		t.Fatal(err)
	}
	proc := f.procs[0]
	decided := make(chan claude.PermissionDecision, 1)
	go func() {
		decided <- proc.h.OnPermission(claude.PermissionRequest{
			RequestID: "perm-1", ToolName: "Bash", ToolUseID: "tu-1", Input: json.RawMessage(`{"command":"go version"}`),
			Suggestions: []json.RawMessage{json.RawMessage(`{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"go version"}],"behavior":"allow","destination":"localSettings"}`)},
		})
	}()

	var pending store.Approval
	waitFor(t, func() bool {
		approvals, _ := f.st.Approvals(s.ID)
		if len(approvals) == 1 {
			pending = approvals[0]
		}
		return pending.ID != ""
	})
	if got := f.session(t, s.ID); got.Status != "waiting_approval" {
		t.Fatalf("status = %q", got.Status)
	}
	if err := f.m.Decide(context.Background(), pending.ID, Decision{Decision: "always"}); err != nil {
		t.Fatal(err)
	}
	d := <-decided
	if !d.Allow || len(d.UpdatedPermissions) != 1 {
		t.Fatalf("decision = %+v", d)
	}
	var update map[string]any
	json.Unmarshal(d.UpdatedPermissions[0], &update)
	if update["destination"] != "userSettings" || update["type"] != "addRules" {
		t.Fatalf("updatedPermissions = %s", d.UpdatedPermissions[0])
	}
	if a, _ := f.st.Approval(pending.ID); a.Status != "allowed" {
		t.Fatalf("approval status = %q", a.Status)
	}
	if got := f.session(t, s.ID); got.Status != "running" {
		t.Fatalf("status after decide = %q", got.Status)
	}
	if err := f.m.Decide(context.Background(), pending.ID, Decision{Decision: "deny"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second decide err = %v", err)
	}
}

func TestExitDuringTurnFails(t *testing.T) {
	f := newFixture(t)
	s, err := f.m.Start(context.Background(), "t1", StartInput{Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	f.procs[0].h.OnExit(errors.New("exit status 1"))
	got := f.session(t, s.ID)
	if got.Status != "failed" || got.InTurn {
		t.Fatalf("after exit: %+v", got)
	}
	events := f.events(t, s.ID)
	if last := events[len(events)-1]; last.Kind != "system" || !strings.Contains(string(last.Body), "exit status 1") {
		t.Fatalf("last event = %s %s", last.Kind, last.Body)
	}

	if _, err := f.m.Send(context.Background(), s.ID, "again"); err != nil {
		t.Fatal(err)
	}
	if len(f.procs) != 2 {
		t.Fatalf("Send after exit spawned %d processes", len(f.procs))
	}
}

func TestIdleExitAndStop(t *testing.T) {
	f := newFixture(t)
	s, err := f.m.Start(context.Background(), "t1", StartInput{Prompt: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	f.procs[0].h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "claude-1"})
	f.procs[0].h.OnMessage(result(0.5))
	if err := f.m.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	if got := f.session(t, s.ID); got.Status != "stopped" || !f.procs[0].closed {
		t.Fatalf("after stop: %+v", got)
	}
	if _, err := f.m.Send(context.Background(), s.ID, "back"); err != nil {
		t.Fatal(err)
	}
	if resume := f.procs[1].opts.Resume; resume != "claude-1" {
		t.Fatalf("respawn resume = %q", resume)
	}
	f.procs[1].h.OnMessage(result(0.25))
	if got := f.session(t, s.ID); got.CostUSD != 0.75 {
		t.Fatalf("cost across processes = %v", got.CostUSD)
	}
}

func TestToolEventsNestByParent(t *testing.T) {
	f := newFixture(t)
	s, err := f.m.Start(context.Background(), "t1", StartInput{Prompt: "explore"})
	if err != nil {
		t.Fatal(err)
	}
	h := f.procs[0].h
	assistant := func(parent, content string) claude.Message {
		return claude.Message{Type: "assistant", ParentToolUseID: parent, Message: &claude.APIMessage{Role: "assistant", Content: json.RawMessage(content)}}
	}
	h.OnMessage(assistant("", `[{"type":"tool_use","id":"toolu_task","name":"Task","input":{"description":"find callers"}}]`))
	h.OnMessage(assistant("toolu_task", `[{"type":"tool_use","id":"toolu_grep","name":"Grep","input":{"pattern":"store.Open"}}]`))
	h.OnMessage(claude.Message{Type: "user", ParentToolUseID: "toolu_task", Message: &claude.APIMessage{Role: "user",
		Content: json.RawMessage(`[{"type":"tool_result","tool_use_id":"toolu_grep","content":"daemon.go:12"}]`)}})
	h.OnMessage(assistant("", `[{"type":"text","text":"Two callers."}]`))

	type row struct{ kind, parent, summary string }
	var rows []row
	for _, e := range f.events(t, s.ID)[1:] {
		var body struct{ Summary string }
		json.Unmarshal(e.Body, &body)
		rows = append(rows, row{e.Kind, e.ParentToolUseID, body.Summary})
	}
	want := []row{
		{"tool", "", "Task: find callers"},
		{"tool", "toolu_task", "Grep: store.Open"},
		{"tool_result", "toolu_task", ""},
		{"text", "", ""},
	}
	if !slices.Equal(rows, want) {
		t.Fatalf("rows = %+v\nwant %+v", rows, want)
	}
}

func TestParentGuard(t *testing.T) {
	call := func(path string) claude.HookCall {
		in, _ := json.Marshal(map[string]string{"file_path": path})
		return claude.HookCall{CallbackID: parentGuard, Input: claude.HookInput{HookEventName: "PreToolUse", ToolName: "Write", ToolInput: in, Cwd: "/ws/child"}}
	}
	if r := onHook("/h/artifacts/c", call(".layer8/task/parent/03-prd.md")); r.Deny == "" {
		t.Fatal("write under parent/ allowed")
	}
	if r := onHook("/h/artifacts/c", call("/h/artifacts/c/parent/03-prd.md")); r.Deny == "" {
		t.Fatal("write under the artifacts parent link allowed")
	}
	if r := onHook("/h/artifacts/c", call(".layer8/task/notes.md")); r.Deny != "" {
		t.Fatalf("own artifact denied: %s", r.Deny)
	}
}

func TestContextBlock(t *testing.T) {
	block := ContextBlock(ContextData{
		Session:      store.Session{ID: "s1", Label: "prd"},
		Task:         store.Task{ID: "t1", Title: "Local agent desktop app", Workflow: "prd_tdd", Phase: "prd", Autonomous: true},
		Parent:       &store.Task{ID: "t0", Title: "Parent"},
		Project:      store.Project{Instructions: "Use Golang as much as possible wherever it is possible and relevant."},
		ArtifactsDir: "/h/artifacts/t1",
	})
	for _, want := range []string{
		"<layer8_session>", "Session ID: s1 (prd)", "Task: Local agent desktop app · t1 · workflow prd_tdd · phase prd",
		"Parent task: Parent · t0", "Autonomous: yes", "symlink to /h/artifacts/t1",
		"<project_instructions>\nUse Golang as much as possible wherever it is possible and relevant.\n</project_instructions>",
	} {
		if !strings.Contains(block, want) {
			t.Errorf("context block lacks %q:\n%s", want, block)
		}
	}
	plain := ContextBlock(ContextData{Task: store.Task{Workflow: "freeform"}})
	if !strings.Contains(plain, "Autonomous: no") || strings.Contains(plain, "Parent task") || strings.Contains(plain, "project_instructions") {
		t.Errorf("plain context block:\n%s", plain)
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
