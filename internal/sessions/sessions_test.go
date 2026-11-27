package sessions_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/sessions"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

type fakeHarness struct {
	starts chan *fakeProc
	probes atomic.Int32
}

func (h *fakeHarness) Start(_ context.Context, o harness.StartOptions, sink harness.Sink) (harness.Proc, error) {
	p := &fakeProc{opts: o, sink: sink, sent: make(chan harness.UserMessage, 10), now: make(chan string, 10), done: make(chan struct{})}
	h.starts <- p
	return p, nil
}

func (h *fakeHarness) Probe(context.Context, string) (harness.Capabilities, error) {
	h.probes.Add(1)
	return harness.Capabilities{Commands: []harness.Command{{Name: "review"}}}, nil
}

type fakeProc struct {
	opts harness.StartOptions
	sink harness.Sink
	sent chan harness.UserMessage
	now  chan string
	once sync.Once
	done chan struct{}
}

func (p *fakeProc) Send(msg harness.UserMessage) error { p.sent <- msg; return nil }

func (p *fakeProc) SendNow(_ context.Context, id string) error { p.now <- id; return nil }

func (p *fakeProc) Interrupt(context.Context) error {
	p.sink.OnEvent(harness.Event{Type: harness.EventResult, Data: map[string]any{"subtype": "error_during_execution", "isError": true}})
	return nil
}

func (p *fakeProc) Close(time.Duration) { p.exit(nil) }

func (p *fakeProc) Done() <-chan struct{} { return p.done }

func (p *fakeProc) exit(err error) {
	p.once.Do(func() {
		close(p.done)
		p.sink.OnExit(err)
	})
}

// take receives the next message and reports it delivered.
func (p *fakeProc) take(t *testing.T) harness.UserMessage {
	t.Helper()
	msg := recv(t, p.sent)
	p.sink.OnEvent(harness.Event{Type: harness.EventDelivered, Data: map[string]any{"id": msg.ID}})
	return msg
}

// turn answers one message and ends the turn as harness session h-1.
func (p *fakeProc) turn(t *testing.T, cost float64) harness.UserMessage {
	t.Helper()
	msg := p.take(t)
	p.sink.OnEvent(harness.Event{Type: harness.EventText, Partial: true, Data: map[string]any{"text": "o"}})
	p.sink.OnEvent(harness.Event{Type: harness.EventText, Data: map[string]any{"text": "ok"}})
	p.sink.OnEvent(harness.Event{Type: harness.EventResult, Data: map[string]any{"sessionId": "h-1", "costUsd": cost, "contextTokens": 1200}})
	return msg
}

func recv[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out")
	}
	var zero T
	return zero
}

type env struct {
	t      *testing.T
	db     *sql.DB
	tree   *tree.Tree
	h      *fakeHarness
	m      *sessions.Manager
	task   tree.Addr
	opened [][]string
	events chan any
}

func newEnv(t *testing.T, o sessions.Options) *env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, events: make(chan any, 1000)}
	e.register(o)
	repo := gitRepo(t)
	e.ok("app", "add-project", `{"path":"`+repo+`"}`)
	e.task = e.ok(tree.Addr("project:"+filepath.Base(repo)), "create-task", `{"title":"Eng 482"}`).Addr
	return e
}

// register builds the tree and the manager on e.db, as the daemon does at start.
func (e *env) register(o sessions.Options) {
	e.tree = tree.New(e.db)
	e.h = &fakeHarness{starts: make(chan *fakeProc, 10)}
	projects.Register(e.tree, e.db, "test")
	ts := tasks.Register(e.tree, e.db, tasks.Options{Home: e.t.TempDir()})
	o.Harnesses = map[string]harness.Harness{"claude": e.h}
	o.Publish = func(name string, data any) {
		if name == "session" {
			e.events <- data
		}
	}
	o.Open = func(_ context.Context, args ...string) error { e.opened = append(e.opened, args); return nil }
	e.m = sessions.Register(e.tree, e.db, ts, o)
}

func gitRepo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	dir = filepath.Join(dir, "api-gateway")
	for _, args := range [][]string{{"init", "-q", "-b", "trunk", dir}, {"-C", dir, "commit", "-q", "--allow-empty", "-m", "init"}} {
		if _, err := gitx.Run(t.Context(), ".", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func (e *env) as(by tree.Actor, target tree.Addr, action, args string) tree.Result {
	e.t.Helper()
	return e.tree.Do(e.t.Context(), by, []tree.Op{{Target: target, Action: action, Args: json.RawMessage(args)}})[0]
}

func (e *env) ok(target tree.Addr, action, args string) tree.Result {
	e.t.Helper()
	r := e.as(tree.Actor{Kind: "user"}, target, action, args)
	if !r.OK {
		e.t.Fatalf("%s %s: %s", target, action, r.Message)
	}
	return r
}

func (e *env) get(addr tree.Addr) tree.Node {
	e.t.Helper()
	n, err := e.tree.Get(e.t.Context(), tree.Actor{Kind: "user"}, addr, 1)
	if err != nil {
		e.t.Fatal(err)
	}
	return n
}

func (e *env) waitStatus(addr tree.Addr, want string) tree.Node {
	e.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		n := e.get(addr)
		if n.Fields["status"] == want {
			return n
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("%s status = %v, want %s", addr, n.Fields["status"], want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (e *env) start(args string) (tree.Addr, *fakeProc) {
	e.t.Helper()
	s := e.ok(e.task, "start-session", args).Addr
	p := recv(e.t, e.h.starts)
	e.waitStatus(s, "running")
	return s, p
}

func (e *env) eventTypes(s tree.Addr) []string {
	e.t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+s.Path()+"/events", nil)
	req.SetPathValue("id8", s.Path())
	e.m.ServeEvents(rec, req)
	var body struct{ Events []sessions.Event }
	json.Unmarshal(rec.Body.Bytes(), &body)
	var types []string
	for _, ev := range body.Events {
		types = append(types, string(ev.Type))
	}
	return types
}

func TestStartSessionWiresTheHarness(t *testing.T) {
	e := newEnv(t, sessions.Options{MCPURL: "http://127.0.0.1:7979/mcp", Token: "tok"})
	s, p := e.start(`{"skill":"research","prompt":"the auth flow","model":"opus","permissionMode":"acceptEdits"}`)
	msg := p.turn(t, 0.25)
	if msg.Text != "/research the auth flow" {
		t.Fatalf("message = %q", msg.Text)
	}
	o := p.opts
	task := e.get(e.task)
	if o.Cwd != task.Fields["worktree"] || !slices.Equal(o.AddDirs, []string{task.Fields["artifacts"].(string)}) ||
		!slices.Contains(o.Env, "LAYER8_ARTIFACTS="+task.Fields["artifacts"].(string)) || o.Model != "opus" || o.PermissionMode != "acceptEdits" ||
		o.MCPURL != "http://127.0.0.1:7979/mcp" || o.MCPHeaders["X-Layer8-Session"] != s.Path() || o.MCPHeaders["Authorization"] != "Bearer tok" ||
		!slices.Equal(o.AllowedTools, []string{"ListMcpResourcesTool", "ReadMcpResourceTool"}) || !strings.Contains(o.AppendSystemPrompt, string(s)) {
		t.Fatalf("start options = %+v", o)
	}
	n := e.waitStatus(s, "idle")
	if n.Title != "research" || n.Fields["harnessSessionId"] != "h-1" || n.Fields["costUsd"] != 0.25 || n.Fields["contextTokens"] != 1200 || n.Fields["createdBy"] != "user" {
		t.Fatalf("session = %+v", n)
	}
	if got := e.eventTypes(s); !slices.Equal(got, []string{"user", "delivered", "text", "result"}) {
		t.Fatalf("stored events = %v", got)
	}
	children := e.get(e.task).Children
	if !slices.ContainsFunc(children, func(c tree.Summary) bool { return c.Addr == s && c.Status == "idle" }) {
		t.Fatalf("task children = %+v", children)
	}
	var partial bool
	for len(e.events) > 0 {
		b, _ := json.Marshal(<-e.events)
		partial = partial || strings.Contains(string(b), `"partial":true`)
	}
	if !partial {
		t.Fatal("partial deltas were not streamed")
	}
}

func TestQueueIsFIFOAndCapped(t *testing.T) {
	e := newEnv(t, sessions.Options{Max: 2})
	a, pa := e.start(`{"prompt":"a"}`)
	b, pb := e.start(`{"prompt":"b"}`)
	c := e.ok(e.task, "start-session", `{"prompt":"c"}`).Addr
	d := e.ok(e.task, "start-session", `{"prompt":"d"}`).Addr
	e.waitStatus(c, "queued")
	e.waitStatus(d, "queued")
	if len(e.h.starts) != 0 {
		t.Fatal("a third process started over the cap")
	}

	pa.turn(t, 0)
	e.waitStatus(a, "idle")
	pc := recv(t, e.h.starts)
	if msg := recv(t, pc.sent); msg.Text != "c" {
		t.Fatalf("first dequeued = %q, want c", msg.Text)
	}
	e.waitStatus(c, "running")
	e.waitStatus(d, "queued")

	pb.turn(t, 0)
	pd := recv(t, e.h.starts)
	if msg := recv(t, pd.sent); msg.Text != "d" {
		t.Fatalf("second dequeued = %q, want d", msg.Text)
	}
	e.waitStatus(b, "idle")
	e.waitStatus(d, "running")
}

func TestFullPoolEvictsTheOldestIdleProcess(t *testing.T) {
	e := newEnv(t, sessions.Options{Max: 1})
	a, pa := e.start(`{"prompt":"a"}`)
	pa.turn(t, 0)
	e.waitStatus(a, "idle")
	b := e.ok(e.task, "start-session", `{"prompt":"b"}`).Addr
	recv(t, pa.done)
	recv(t, e.h.starts)
	e.waitStatus(b, "running")
	e.waitStatus(a, "idle")
}

func TestIdleProcessClosesAndResumes(t *testing.T) {
	e := newEnv(t, sessions.Options{IdleAfter: 30 * time.Millisecond})
	s, p := e.start(`{"prompt":"hello"}`)
	p.turn(t, 0.5)
	recv(t, p.done)
	e.waitStatus(s, "idle")

	e.ok(s, "say", `{"text":"again","files":["/w/a.go"],"images":[{"mediaType":"image/png","data":"cG5n"}]}`)
	p2 := recv(t, e.h.starts)
	if p2.opts.ResumeID != "h-1" || p2.opts.Fork {
		t.Fatalf("resume options = %+v", p2.opts)
	}
	msg := p2.turn(t, 0.25)
	if msg.Text != "again" || msg.Files[0] != "/w/a.go" || string(msg.Images[0].Data) != "png" {
		t.Fatalf("resumed message = %+v", msg)
	}
	if n := e.waitStatus(s, "idle"); n.Fields["costUsd"] != 0.75 {
		t.Fatalf("cost = %v, want the sum of both processes", n.Fields["costUsd"])
	}
}

func TestApprovalFlowAndActorGuard(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	s, p := e.start(`{"prompt":"write"}`)
	p.take(t)
	decisions := make(chan harness.Decision, 2)
	ask := func(id string) {
		p.sink.OnPermission(harness.PermissionRequest{ID: id, Tool: "Write", Input: json.RawMessage(`{"file_path":"a.txt"}`)},
			func(d harness.Decision) { decisions <- d })
	}
	ask("perm-1")
	n := e.waitStatus(s, "waiting")
	pending, _ := json.Marshal(n.Fields["approvals"])
	if !strings.Contains(string(pending), `"tool":"Write"`) || !slices.ContainsFunc(n.Actions, func(a tree.Action) bool { return a.Name == "allow" }) {
		t.Fatalf("waiting session = %s %+v", pending, n.Actions)
	}
	for _, by := range []tree.Actor{{Kind: "session", ID: s.Path()}, {Kind: "mcp"}, {Kind: "plugin", ID: "x"}, {Kind: "cli"}} {
		if r := e.as(by, s, "allow", `{}`); r.OK || !strings.Contains(r.Message, "only the user") {
			t.Fatalf("allow by %s = %+v", by, r)
		}
	}
	e.ok(s, "allow", `{"always":true}`)
	if d := recv(t, decisions); !d.Allow || !d.Always {
		t.Fatalf("decision = %+v", d)
	}
	e.waitStatus(s, "running")

	ask("perm-2")
	e.waitStatus(s, "waiting")
	e.ok(s, "deny", `{"message":"not that file"}`)
	if d := recv(t, decisions); d.Allow || d.Message != "not that file" {
		t.Fatalf("decision = %+v", d)
	}
	if r := e.as(tree.Actor{Kind: "user"}, s, "deny", `{"approval":99}`); r.OK {
		t.Fatal("deny on a running session succeeded")
	}
	var statuses []string
	rows, _ := e.db.Query(`SELECT status || ':' || decided_by FROM approvals ORDER BY id`)
	for rows.Next() {
		var st string
		rows.Scan(&st)
		statuses = append(statuses, st)
	}
	if !slices.Equal(statuses, []string{"allowed:user", "denied:user"}) {
		t.Fatalf("approvals = %v", statuses)
	}
	if got := e.eventTypes(s); !slices.Equal(got, []string{"user", "delivered", "approval", "approval", "approval", "approval"}) {
		t.Fatalf("events = %v", got)
	}
}

func TestInterruptDeniesPendingApprovals(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	s, p := e.start(`{"prompt":"write"}`)
	p.take(t)
	decisions := make(chan harness.Decision, 1)
	p.sink.OnPermission(harness.PermissionRequest{ID: "perm-1", Tool: "Bash"}, func(d harness.Decision) { decisions <- d })
	e.waitStatus(s, "waiting")
	e.ok(s, "interrupt", `{}`)
	if d := recv(t, decisions); d.Allow {
		t.Fatalf("decision = %+v", d)
	}
	e.waitStatus(s, "idle")
}

func TestOneResultCanAnswerSeveralMessages(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	s, p := e.start(`{"prompt":"a"}`)
	p.take(t)
	e.ok(s, "say", `{"text":"b"}`)
	p.take(t)
	p.sink.OnEvent(harness.Event{Type: harness.EventResult, Data: map[string]any{}})
	e.waitStatus(s, "idle")
}

func TestSendNow(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	s, p := e.start(`{"prompt":"a"}`)
	p.take(t)
	say := func(args string) string {
		r := e.ok(s, "say", args)
		recv(t, p.sent)
		return r.Data.(map[string]string)["message"]
	}
	b, c := say(`{"text":"b"}`), say(`{"text":"c"}`)

	e.ok(s, "send-now", `{}`)
	if got := recv(t, p.now); got != b {
		t.Fatalf("send-now without a message sent %q, want the oldest %q", got, b)
	}
	e.ok(s, "send-now", `{"message":"`+c+`"}`)
	if got := recv(t, p.now); got != c {
		t.Fatalf("send-now sent %q, want %q", got, c)
	}
	d := say(`{"text":"d","now":true}`)
	if got := recv(t, p.now); got != d {
		t.Fatalf("say now sent %q now, want %q", got, d)
	}
	if r := e.as(tree.Actor{Kind: "mcp"}, s, "send-now", `{"message":"nope"}`); r.OK {
		t.Fatal("send-now of an unknown message succeeded")
	}

	p.sink.OnEvent(harness.Event{Type: harness.EventResult, Data: map[string]any{}})
	e.waitStatus(s, "running")
	for _, id := range []string{b, c, d} {
		p.sink.OnEvent(harness.Event{Type: harness.EventDelivered, Data: map[string]any{"id": id}})
	}
	p.sink.OnEvent(harness.Event{Type: harness.EventResult, Data: map[string]any{}})
	e.waitStatus(s, "idle")
}

func TestSendNowReordersMessagesWaitingForAProcess(t *testing.T) {
	e := newEnv(t, sessions.Options{Max: 1})
	a, pa := e.start(`{"prompt":"a"}`)
	c := e.ok(e.task, "start-session", `{"prompt":"c1"}`).Addr
	e.waitStatus(c, "queued")
	e.ok(c, "say", `{"text":"c2"}`)
	id := e.ok(c, "say", `{"text":"c3"}`).Data.(map[string]string)["message"]
	e.ok(c, "send-now", `{"message":"`+id+`"}`)

	pa.turn(t, 0)
	e.waitStatus(a, "idle")
	pc := recv(t, e.h.starts)
	var got []string
	for range 3 {
		got = append(got, recv(t, pc.sent).Text)
	}
	if !slices.Equal(got, []string{"c3", "c1", "c2"}) {
		t.Fatalf("delivery order = %v", got)
	}
}

func TestCrashCloseAndRestartRecovery(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	crashed, p := e.start(`{"prompt":"crash"}`)
	p.exit(errors.New("claude exited: exit status 2: out of cheese"))
	if n := e.waitStatus(crashed, "failed"); !strings.Contains(n.Fields["error"].(string), "out of cheese") {
		t.Fatalf("failed session = %+v", n.Fields)
	}

	closed, p := e.start(`{"prompt":"bye"}`)
	e.ok(closed, "close", `{}`)
	recv(t, p.done)
	e.waitStatus(closed, "done")

	running, p := e.start(`{"prompt":"write"}`)
	p.turn(t, 0)
	e.waitStatus(running, "idle")
	e.ok(running, "say", `{"text":"write"}`)
	recv(t, p.sent)
	p.sink.OnPermission(harness.PermissionRequest{ID: "perm-1", Tool: "Write"}, func(harness.Decision) {})
	e.waitStatus(running, "waiting")

	e.register(sessions.Options{})
	n := e.get(running)
	if approvals, _ := json.Marshal(n.Fields["approvals"]); n.Fields["status"] != "idle" || string(approvals) != "[]" {
		t.Fatalf("after restart = %+v", n.Fields)
	}
	e.ok(running, "say", `{"text":"go on"}`)
	if p := recv(t, e.h.starts); p.opts.ResumeID != "h-1" {
		t.Fatalf("resume id = %q", p.opts.ResumeID)
	}
	e.waitStatus(closed, "done")
	e.waitStatus(crashed, "failed")
}

func TestHereAndSessionCreatedSessions(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	s := e.ok(e.task, "start-session", `{"title":"Plan"}`).Addr
	by := tree.Actor{Kind: "session", ID: s.Path()}
	n, err := e.tree.Get(t.Context(), by, "here", 0)
	if err != nil || n.Addr != e.task {
		t.Fatalf("here = %v, %v", n.Addr, err)
	}
	if _, err := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, "here", 0); err == nil {
		t.Fatal("here resolved for the user")
	}
	child := e.as(by, "here", "start-session", `{"title":"Implement"}`)
	if !child.OK || e.get(child.Addr).Fields["createdBy"] != by.String() {
		t.Fatalf("child = %+v", child)
	}
	var logged int
	e.db.QueryRow(`SELECT count(*) FROM actions_log WHERE actor_kind = 'session' AND actor_id = ? AND action = 'start-session' AND ok`, s.Path()).Scan(&logged)
	if logged != 1 {
		t.Fatalf("actions_log rows = %d", logged)
	}
	if len(e.h.starts) != 0 {
		t.Fatal("a session without a prompt started a process")
	}
}

func TestForkSetAndOpenTerminal(t *testing.T) {
	e := newEnv(t, sessions.Options{Terminal: "iTerm"})
	s, p := e.start(`{"prompt":"hello","model":"sonnet"}`)
	p.turn(t, 0)
	e.waitStatus(s, "idle")

	fork := e.ok(s, "fork", `{}`).Addr
	if n := e.get(fork); n.Title != "hello (fork)" || n.Fields["model"] != "sonnet" || n.Fields["status"] != "idle" {
		t.Fatalf("fork = %+v", n)
	}
	if got := e.eventTypes(fork); !slices.Equal(got, []string{"user", "delivered", "text", "result"}) {
		t.Fatalf("fork events = %v", got)
	}
	e.ok(fork, "say", `{"text":"branch off"}`)
	pf := recv(t, e.h.starts)
	if pf.opts.ResumeID != "h-1" || !pf.opts.Fork {
		t.Fatalf("fork start = %+v", pf.opts)
	}

	e.ok(s, "set", `{"model":"opus","effort":"high"}`)
	recv(t, p.done)
	e.ok(s, "say", `{"text":"go"}`)
	if p2 := recv(t, e.h.starts); p2.opts.Model != "opus" || p2.opts.Effort != "high" {
		t.Fatalf("restart after set = %+v", p2.opts)
	} else {
		p2.turn(t, 0)
		e.waitStatus(s, "idle")
		e.ok(s, "open-terminal", `{}`)
		recv(t, p2.done)
	}
	if len(e.opened) != 1 || e.opened[0][1] != "iTerm" {
		t.Fatalf("opened = %v", e.opened)
	}
	script, _ := os.ReadFile(e.opened[0][2])
	if want := "cd '" + e.get(e.task).Fields["worktree"].(string) + "' && exec claude --resume 'h-1'"; !strings.Contains(string(script), want) {
		t.Fatalf("script = %s", script)
	}
}

func TestCapabilitiesAreCached(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	for range 2 {
		rec := httptest.NewRecorder()
		e.m.ServeCapabilities(rec, httptest.NewRequest(http.MethodGet, "/api/capabilities?task="+string(e.task), nil))
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"review"`) {
			t.Fatalf("capabilities = %d %s", rec.Code, rec.Body)
		}
	}
	if n := e.h.probes.Load(); n != 1 {
		t.Fatalf("probes = %d, want 1", n)
	}
}

func TestUsageNode(t *testing.T) {
	e := newEnv(t, sessions.Options{})
	s, p := e.start(`{"prompt":"hi","title":"research"}`)
	p.take(t)
	p.sink.OnEvent(harness.Event{Type: harness.EventLimits, Data: map[string]any{"limits": []harness.Limit{
		{Window: "5h", UsedPercent: 42, ResetsAt: 1764252000}, {Window: "weekly", UsedPercent: 18, ResetsAt: 1764720000}}}})
	p.sink.OnEvent(harness.Event{Type: harness.EventLimits, Data: map[string]any{"limits": []harness.Limit{{Window: "weekly", UsedPercent: 19, ResetsAt: 1764720000}}}})
	for range 2 {
		p.sink.OnEvent(harness.Event{Type: harness.EventResult, Data: map[string]any{"sessionId": "h-1", "contextTokens": 370, "contextWindow": 200000,
			"tokens": harness.Tokens{Input: 10, Output: 20, CacheRead: 300, CacheWrite: 40}}})
	}
	if n := e.waitStatus(s, "idle"); n.Fields["contextWindow"] != 200000 {
		t.Fatalf("contextWindow = %v", n.Fields["contextWindow"])
	}
	if got := e.eventTypes(s); !slices.Equal(got, []string{"user", "delivered", "result", "result"}) {
		t.Fatalf("stored events = %v, want limits kept out of the transcript", got)
	}
	if !slices.ContainsFunc(e.get("app").Children, func(c tree.Summary) bool { return c.Addr == "usage" }) {
		t.Fatal("usage is not a child of app")
	}
	raw, _ := json.Marshal(e.get("usage").Fields)
	var u struct {
		Limits []struct {
			Harness, Window string
			UsedPercent     float64
			ResetsAt        int64
		}
		Today []struct {
			Session, Task, Title, Harness        string
			Input, Output, CacheRead, CacheWrite int
		}
	}
	json.Unmarshal(raw, &u)
	if len(u.Limits) != 2 || u.Limits[0].Harness != "claude" || u.Limits[0].Window != "5h" || u.Limits[0].UsedPercent != 42 ||
		u.Limits[1].Window != "weekly" || u.Limits[1].UsedPercent != 19 || u.Limits[1].ResetsAt != 1764720000 {
		t.Fatalf("limits = %s", raw)
	}
	if len(u.Today) != 1 || u.Today[0].Session != string(s) || u.Today[0].Task != string(e.task) || u.Today[0].Title != "research" ||
		u.Today[0].Input != 20 || u.Today[0].Output != 40 || u.Today[0].CacheRead != 600 || u.Today[0].CacheWrite != 80 {
		t.Fatalf("today = %s", raw)
	}
}
