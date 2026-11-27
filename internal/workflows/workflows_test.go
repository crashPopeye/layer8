package workflows_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/sessions"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
	"github.com/crashPopeye/layer8/internal/workflows"
)

func TestParse(t *testing.T) {
	cases := map[string]string{
		"steps:\n  - name: a\n    skill: x\n    color: red\n": "field color not found",
		"steps: []\n": "no steps",
		"steps:\n  - name: a\n    skill: x\n  - name: a\n    skill: y\n": `name "a" is used twice`,
		"steps:\n  - name: a\n":                                                      "skill is required",
		"steps:\n  - name: a\n    skill: x\n    harness: gpt\n":                      `harness "gpt"`,
		"steps:\n  - name: a\n    skill: x\n    fanout: all\n":                       `fanout "all"`,
		"steps:\n  - name: Do It\n    skill: x\n":                                    `name "Do It"`,
		"description: ok\nsteps:\n  - name: a\n    skill: x\n    fanout: subtasks\n": "",
	}
	for src, want := range cases {
		_, err := workflows.Parse("w", []byte(src))
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("Parse(%q) = %v, want %q", src, err, want)
		}
	}
}

type env struct {
	t        *testing.T
	tree     *tree.Tree
	svc      *workflows.Service
	dir      string
	started  []tree.Addr
	sessions map[string]tree.Addr
	args     []map[string]string
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if _, err := gitx.Run(t.Context(), dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...); err != nil {
		t.Fatal(err)
	}
}

// newEnv builds a tree with project api, task feat, and a fake start-session that records where it ran.
func newEnv(t *testing.T) *env {
	t.Helper()
	return build(t, false)
}

// build is newEnv, with the real session manager over an idle harness instead of the fake start-session when real is set.
func build(t *testing.T, real bool) *env {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	repo := filepath.Join(root, "api")
	git(t, root, "init", "-q", "-b", "main", repo)
	git(t, repo, "commit", "-q", "--allow-empty", "-m", "init")
	db, err := store.Open(filepath.Join(root, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, tree: tree.New(db), dir: filepath.Join(root, "workflows"), sessions: map[string]tree.Addr{}}
	os.Mkdir(e.dir, 0o755)
	projects.Register(e.tree, db, "test")
	ts := tasks.Register(e.tree, db, tasks.Options{Home: filepath.Join(root, "home"), Apps: config.Apps{}})
	if real {
		sessions.Register(e.tree, db, ts, sessions.Options{Harnesses: map[string]harness.Harness{"claude": idleHarness{}}})
	} else {
		e.fakeSessions()
	}
	e.svc = workflows.Register(e.tree, db, workflows.Options{Dir: e.dir})
	e.ok("user", "app", "add-project", `{"path":"`+repo+`"}`)
	e.ok("user", "project:api", "create-task", `{"title":"feat"}`)
	return e
}

// fakeSessions adds a start-session that only records where it ran, and a here that knows its sessions.
func (e *env) fakeSessions() {
	e.tree.Extend("task", tree.Action{Name: "start-session", Help: "fake",
		Args: json.RawMessage(`{"type":"object","properties":{"prompt":{"type":"string"},"skill":{"type":"string"},
			"harness":{"enum":["claude","codex"]},"model":{"type":"string"},"effort":{"type":"string"},"title":{"type":"string"}}}`),
		Run: func(_ context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
			id8 := fmt.Sprintf("%08d", len(e.started)+1)
			var a map[string]string
			json.Unmarshal(args, &a)
			e.started, e.args = append(e.started, n.Addr), append(e.args, a)
			e.sessions[id8] = n.Addr
			return tree.Result{Addr: tree.Addr("session:" + id8)}, nil
		}})
	e.tree.Here = func(_ context.Context, by tree.Actor) (tree.Addr, error) { return e.sessions[by.ID], nil }
}

func (e *env) do(by string, target tree.Addr, action, args string) tree.Result {
	e.t.Helper()
	return e.tree.Do(e.t.Context(), tree.ParseActor(by), []tree.Op{{Target: target, Action: action, Args: json.RawMessage(args)}})[0]
}

func (e *env) ok(by string, target tree.Addr, action, args string) tree.Result {
	e.t.Helper()
	res := e.do(by, target, action, args)
	if !res.OK {
		e.t.Fatalf("%s %s: %s", target, action, res.Message)
	}
	return res
}

func (e *env) workflow(addr tree.Addr) (map[string]any, []string) {
	e.t.Helper()
	n, err := e.tree.Get(e.t.Context(), tree.Actor{Kind: "user"}, addr, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	var actions []string
	for _, a := range n.Actions {
		actions = append(actions, a.Name)
	}
	w, _ := n.Fields["workflow"].(map[string]any)
	return w, actions
}

func (e *env) write(name, content string) {
	if err := os.WriteFile(filepath.Join(e.dir, name), []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func TestWorkflowNodes(t *testing.T) {
	e := newEnv(t)
	e.write("quick-fix.yaml", "steps:\n  - name: fix\n    skill: my:fix\n")
	e.write("broken.yaml", "steps: nope\n")
	app, err := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, "app", 1)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range app.Children {
		if c.Kind == "workflow" {
			got = append(got, string(c.Addr)+"["+c.Status+"]")
		}
	}
	want := []string{"workflow:broken[invalid]", "workflow:feature[]", "workflow:pr-care[]", "workflow:product-feature[]", "workflow:quick-fix[]"}
	if !slices.Equal(got, want) {
		t.Fatalf("workflows = %v, want %v", got, want)
	}
	qf, _ := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, "workflow:quick-fix", 0)
	if steps := qf.Fields["steps"].([]workflows.Step); len(steps) != 1 || steps[0].Skill != "my:fix" {
		t.Fatalf("user file does not replace the built-in: %+v", qf.Fields)
	}
	broken, _ := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, "workflow:broken", 0)
	if broken.Fields["error"] == nil {
		t.Fatalf("broken workflow has no error: %+v", broken.Fields)
	}
	if res := e.do("user", "task:feat", "run-workflow", `{"workflow":"broken"}`); res.OK {
		t.Fatal("ran an invalid workflow")
	}
}

func TestGate(t *testing.T) {
	e := newEnv(t)
	e.ok("user", "task:feat", "run-workflow", `{"workflow":"quick-fix"}`)
	if !slices.Equal(e.started, []tree.Addr{"task:feat"}) || e.args[0]["skill"] != "layer8:implement" || e.args[0]["title"] != "implement" {
		t.Fatalf("started %v with %v", e.started, e.args)
	}
	w, actions := e.workflow("task:feat")
	if w["status"] != "running" || w["step"] != "implement" || slices.Contains(actions, "run-workflow") || !slices.Contains(actions, "advance") {
		t.Fatalf("workflow = %v, actions = %v", w, actions)
	}

	e.sessions["77777777"] = "task:feat"
	res := e.ok("session:77777777", "here", "advance", `{}`)
	if !strings.Contains(res.Message, "nothing to advance") {
		t.Fatalf("a stranger session advanced the step: %s", res.Message)
	}
	e.ok("session:00000001", "here", "advance", `{}`)
	w, actions = e.workflow("task:feat")
	if w["status"] != "waiting" || w["next"] != "describe-pr" || !slices.Contains(actions, "next-step") || len(e.started) != 1 {
		t.Fatalf("after advance: workflow = %v, actions = %v, started = %v", w, actions, e.started)
	}

	e.ok("user", "task:feat", "next-step", `{}`)
	if len(e.started) != 2 || e.args[1]["skill"] != "layer8:describe-pr" {
		t.Fatalf("next-step started %v", e.args)
	}
	e.ok("session:00000002", "task:feat", "advance", `{}`)
	w, actions = e.workflow("task:feat")
	if w["status"] != "done" || !slices.Contains(actions, "run-workflow") {
		t.Fatalf("workflow = %v, actions = %v", w, actions)
	}
}

func TestAutopilot(t *testing.T) {
	e := newEnv(t)
	e.ok("user", "task:feat", "run-workflow", `{"workflow":"quick-fix","autopilot":true}`)
	if res := e.ok("session:00000001", "here", "advance", `{"step":"describe-pr"}`); !strings.Contains(res.Message, "nothing to advance") {
		t.Fatalf("advanced the wrong step: %s", res.Message)
	}
	e.ok("session:00000001", "here", "advance", `{"step":"implement"}`)
	w, _ := e.workflow("task:feat")
	if len(e.started) != 2 || w["status"] != "running" || w["step"] != "describe-pr" {
		t.Fatalf("autopilot did not start the next step: %v %v", e.started, w)
	}
	e.ok("user", "task:feat", "stop-workflow", `{}`)
	if w, actions := e.workflow("task:feat"); w["status"] != "stopped" || slices.Contains(actions, "advance") {
		t.Fatalf("after stop: %v %v", w, actions)
	}
}

func TestFanoutFollowsTheStack(t *testing.T) {
	e := newEnv(t)
	e.ok("user", "task:feat", "create-subtask", `{"name":"pr1"}`)
	e.ok("user", "task:feat", "create-subtask", `{"name":"pr2","base":"pr1"}`)
	e.ok("user", "task:feat", "create-subtask", `{"name":"docs"}`)
	e.write("ship.yaml", "steps:\n  - name: implement\n    skill: layer8:implement\n    fanout: subtasks\n  - name: describe-pr\n    skill: layer8:describe-pr\n")
	e.ok("user", "task:feat", "run-workflow", `{"workflow":"ship"}`)
	if want := []tree.Addr{"task:feat/pr1", "task:feat/docs"}; !slices.Equal(e.started, want) {
		t.Fatalf("started %v, want %v", e.started, want)
	}
	if w, actions := e.workflow("task:feat/pr1"); w["task"] != tree.Addr("task:feat") || !slices.Contains(actions, "advance") {
		t.Fatalf("sub-task does not see its parent's workflow: %v %v", w, actions)
	}

	e.ok("session:00000001", "here", "advance", `{}`)
	if want := []tree.Addr{"task:feat/pr1", "task:feat/docs", "task:feat/pr2"}; !slices.Equal(e.started, want) {
		t.Fatalf("after pr1: started %v, want %v", e.started, want)
	}
	e.ok("session:00000003", "here", "advance", `{}`)
	if w, _ := e.workflow("task:feat"); w["status"] != "running" {
		t.Fatalf("step finished with docs still running: %v", w)
	}
	e.ok("user", "task:feat/docs", "advance", `{}`)
	w, _ := e.workflow("task:feat")
	if w["status"] != "waiting" || w["next"] != "describe-pr" {
		t.Fatalf("after the last sub-task: %v", w)
	}
}

type idleHarness struct{}

func (idleHarness) Start(context.Context, harness.StartOptions, harness.Sink) (harness.Proc, error) {
	return &idleProc{done: make(chan struct{})}, nil
}

func (idleHarness) Probe(context.Context, string) (harness.Capabilities, error) {
	return harness.Capabilities{}, nil
}

type idleProc struct {
	done chan struct{}
	once sync.Once
}

func (p *idleProc) Send(harness.UserMessage) error        { return nil }
func (p *idleProc) SendNow(context.Context, string) error { return nil }
func (p *idleProc) Interrupt(context.Context) error       { return nil }
func (p *idleProc) Close(time.Duration)                   { p.once.Do(func() { close(p.done) }) }
func (p *idleProc) Done() <-chan struct{}                 { return p.done }

func TestWithTheSessionManager(t *testing.T) {
	e := build(t, true)
	e.ok("user", "task:feat", "run-workflow", `{"workflow":"quick-fix"}`)
	task, err := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, "task:feat", 1)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(task.Children, func(c tree.Summary) bool { return c.Kind == "session" })
	if i < 0 || task.Children[i].Title != "implement" {
		t.Fatalf("no implement session among %+v", task.Children)
	}
	session, err := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, task.Children[i].Addr, 0)
	if err != nil || session.Fields["skill"] != "layer8:implement" {
		t.Fatalf("session = %+v, %v", session.Fields, err)
	}

	e.ok(string(session.Addr), "here", "advance", `{}`)
	if w, _ := e.workflow("task:feat"); w["status"] != "waiting" || w["next"] != "describe-pr" {
		t.Fatalf("after advance from %s: %v", session.Addr, w)
	}
}
