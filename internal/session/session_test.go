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
	mu       sync.Mutex
	opts     claude.Options
	h        claude.Handlers
	sent     []string
	controls []string
	closed   bool
}

func (p *fakeProc) Send(text string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, text)
	return nil
}
func (p *fakeProc) Interrupt() error                    { return nil }
func (p *fakeProc) SetModel(model string) error         { return p.control("model=" + model) }
func (p *fakeProc) SetPermissionMode(mode string) error { return p.control("mode=" + mode) }

func (p *fakeProc) control(c string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.controls = append(p.controls, c)
	return nil
}

func (p *fakeProc) Controls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.controls)
}

func (p *fakeProc) Closed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}
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

	mu        sync.Mutex
	published []map[string]any
}

func (f *fixture) record(v any) {
	if m, ok := v.(map[string]any); ok {
		f.mu.Lock()
		f.published = append(f.published, m)
		f.mu.Unlock()
	}
}

// count is how many published events had kind.
func (f *fixture) count(kind string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.published {
		if e["kind"] == kind {
			n++
		}
	}
	return n
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
	f.m = NewManager(st, f.record, func(o claude.Options, h claude.Handlers) (Proc, error) {
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
	if r := guardParent("/h/artifacts/c", call(".layer8/task/parent/03-prd.md")); !strings.Contains(r.Deny, "use l8 artifact write --task <parent>") {
		t.Fatalf("write under parent/ answered %+v", r)
	}
	if r := guardParent("/h/artifacts/c", call("/h/artifacts/c/parent/03-prd.md")); r.Deny == "" {
		t.Fatal("write under the artifacts parent link allowed")
	}
	if r := guardParent("/h/artifacts/c", call(".layer8/task/notes.md")); r.Deny != "" {
		t.Fatalf("own artifact denied: %s", r.Deny)
	}
}

func TestParentGuardThroughTheHook(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Start(context.Background(), "t1", StartInput{Prompt: "edit the prd"}); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.procs[0].opts.Hooks, func(h claude.HookMatcher) bool {
		return h.Event == "PreToolUse" && h.ID == parentGuard && strings.Contains(h.Matcher, "Write")
	}) {
		t.Fatalf("hooks = %+v", f.procs[0].opts.Hooks)
	}
	write := func(path string) claude.HookResult {
		in, _ := json.Marshal(map[string]string{"file_path": path})
		return f.procs[0].h.OnHook(claude.HookCall{CallbackID: parentGuard,
			Input: claude.HookInput{HookEventName: "PreToolUse", ToolName: "Write", ToolInput: in, Cwd: f.task.Worktree}})
	}
	if r := write(".layer8/task/parent/03-prd.md"); r.Deny == "" {
		t.Fatal("Write to the parent's PRD allowed")
	}
	if r := write(".layer8/task/notes.md"); r.Deny != "" {
		t.Fatalf("Write to notes.md denied: %s", r.Deny)
	}
}

func TestRefreshCoalesces(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Start(context.Background(), "t1", StartInput{Prompt: "write the prd"}); err != nil {
		t.Fatal(err)
	}
	write := claude.HookCall{CallbackID: refresh, Input: claude.HookInput{HookEventName: "PostToolUse", ToolName: "Write"}}
	for range 5 {
		if r := f.procs[0].h.OnHook(write); r.Deny != "" || r.AdditionalContext != "" {
			t.Fatalf("refresh hook answered %+v", r)
		}
	}
	if n := f.count("artifacts"); n != 0 {
		t.Fatalf("%d artifacts events before the window ends", n)
	}
	waitFor(t, func() bool { return f.count("artifacts") == 1 })
	time.Sleep(RefreshWindow + 100*time.Millisecond)
	if a, c := f.count("artifacts"), f.count("changes"); a != 1 || c != 1 {
		t.Fatalf("artifacts = %d, changes = %d after one window", a, c)
	}
	f.procs[0].h.OnHook(write)
	waitFor(t, func() bool { return f.count("artifacts") == 2 })
}

func TestSkillRoutesToTheNextPhase(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Start(context.Background(), "t1", StartInput{Label: "research", Prompt: "/l8:create-research .layer8/task/"}); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.procs[0].opts.Hooks, func(h claude.HookMatcher) bool {
		return h.Event == "PostToolUse" && h.Matcher == "Skill" && h.ID == route
	}) {
		t.Fatalf("hooks = %+v", f.procs[0].opts.Hooks)
	}
	skill := func(name string) claude.HookResult {
		in, _ := json.Marshal(map[string]string{"skill": name})
		return f.procs[0].h.OnHook(claude.HookCall{CallbackID: route,
			Input: claude.HookInput{HookEventName: "PostToolUse", ToolName: "Skill", ToolInput: in}})
	}
	if r := skill("l8:create-research"); r.AdditionalContext != "Next in prd_tdd: /l8:create-prd" {
		t.Fatalf("after create-research: %+v", r)
	}
	if r := skill("l8:show-me"); r.AdditionalContext != "" {
		t.Fatalf("after show-me: %+v", r)
	}
}

func TestOnIdleAfterTheQueueDrains(t *testing.T) {
	f := newFixture(t)
	idle := make(chan store.Session, 2)
	f.m.OnIdle = func(s store.Session) { idle <- s }
	s, err := f.m.Start(context.Background(), "t1", StartInput{Label: "prd", Prompt: "first"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Send(context.Background(), s.ID, "second"); err != nil {
		t.Fatal(err)
	}
	f.procs[0].h.OnMessage(result(0.1))
	select {
	case <-idle:
		t.Fatal("OnIdle ran with a message still queued")
	case <-time.After(50 * time.Millisecond):
	}
	f.procs[0].h.OnMessage(result(0.2))
	select {
	case got := <-idle:
		if got.ID != s.ID || got.Status != "ready_for_input" || got.InTurn {
			t.Fatalf("OnIdle got %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("OnIdle never ran")
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

func TestForkResumesTheSourceTranscriptOnce(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	src, err := f.m.Start(ctx, "t1", StartInput{Label: "research", Prompt: "plan it", Model: "opus"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Start(ctx, "t1", StartInput{ForkFrom: src.ID}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("fork before init = %v", err)
	}
	f.procs[0].h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "claude-1"})
	f.procs[0].h.OnMessage(result(0.1))

	fork, err := f.m.Start(ctx, "t1", StartInput{ForkFrom: src.ID, Prompt: "try the other way"})
	if err != nil {
		t.Fatal(err)
	}
	if fork.ForkedFrom != src.ID || fork.Label != "research fork" || fork.Model != "opus" || fork.ClaudeSessionID != "" {
		t.Fatalf("fork = %+v", fork)
	}
	if o := f.procs[1].opts; o.Resume != "claude-1" || !o.Fork {
		t.Fatalf("fork spawn: resume %q fork %v", o.Resume, o.Fork)
	}
	f.procs[1].h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "claude-2"})
	if got := f.session(t, fork.ID); got.ClaudeSessionID != "claude-2" || got.ForkedFrom != src.ID {
		t.Fatalf("fork after init = %+v", got)
	}
	if got := f.session(t, src.ID); got.ClaudeSessionID != "claude-1" {
		t.Fatalf("source after fork = %+v", got)
	}
	var kinds []string
	for _, e := range f.events(t, fork.ID) {
		kinds = append(kinds, e.Kind)
	}
	if want := []string{"user", "result", "system", "user"}; !slices.Equal(kinds, want) {
		t.Fatalf("fork events = %v, want %v", kinds, want)
	}

	f.procs[1].h.OnMessage(result(0.2))
	if err := f.m.Stop(fork.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.m.Send(ctx, fork.ID, "again"); err != nil {
		t.Fatal(err)
	}
	if o := f.procs[2].opts; o.Resume != "claude-2" || o.Fork {
		t.Fatalf("fork respawn: resume %q fork %v", o.Resume, o.Fork)
	}
}

func TestUpdateLive(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s, err := f.m.Start(ctx, "t1", StartInput{Prompt: "go"})
	if err != nil {
		t.Fatal(err)
	}
	proc := f.procs[0]
	proc.h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "claude-1"})
	str := func(v string) *string { return &v }

	if _, err := f.m.Update(s.ID, Update{PermissionMode: str("yolo")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad mode = %v", err)
	}
	if _, err := f.m.Update(s.ID, Update{Effort: str("high")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("effort during a turn = %v", err)
	}
	got, err := f.m.Update(s.ID, Update{PermissionMode: str("bypassPermissions"), Model: str("sonnet")})
	if err != nil || got.PermissionMode != "bypassPermissions" || got.Model != "sonnet" {
		t.Fatalf("Update = %+v, %v", got, err)
	}
	if want := []string{"model=sonnet", "mode=bypassPermissions"}; !slices.Equal(proc.Controls(), want) {
		t.Fatalf("controls = %v, want %v", proc.Controls(), want)
	}

	proc.h.OnMessage(result(0.1))
	if _, err := f.m.Update(s.ID, Update{Effort: str("high")}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, proc.Closed)
	if got := f.session(t, s.ID); got.Status != "ready_for_input" || got.Effort != "high" {
		t.Fatalf("after effort change = %+v", got)
	}
	if _, err := f.m.Send(ctx, s.ID, "next"); err != nil {
		t.Fatal(err)
	}
	if o := f.procs[1].opts; o.Effort != "high" || o.Resume != "claude-1" || o.PermissionMode != "bypassPermissions" {
		t.Fatalf("respawn options %+v", o)
	}
}

func TestCommandsFromInit(t *testing.T) {
	f := newFixture(t)
	if _, err := f.m.Start(context.Background(), "t1", StartInput{Prompt: "go"}); err != nil {
		t.Fatal(err)
	}
	f.procs[0].h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "c", SlashCommands: []string{"compact", "l8:create-prd"}})
	if got, _ := f.m.Commands("smoke"); !slices.Equal(got, []string{"compact", "l8:create-prd"}) {
		t.Fatalf("Commands = %v", got)
	}
}
