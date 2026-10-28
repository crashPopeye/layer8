package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/sessions"
	"github.com/crashPopeye/layer8/internal/store"
)

// ---- fixtures

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitFile(t *testing.T, dir, name, content, msg string) string {
	write(t, filepath.Join(dir, name), content)
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", msg)
	return git(t, dir, "rev-parse", "HEAD")
}

// repo makes an upstream with one commit and a clone (the workspace). It returns
// the clone and a second clone used to push "teammate" commits to main.
func repo(t *testing.T) (clone, other string) {
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream.git")
	git(t, root, "init", "-q", "--bare", "-b", "main", upstream)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", upstream, seed)
	commitFile(t, seed, "README.md", "hello\n", "init")
	git(t, seed, "push", "-q", "origin", "HEAD:main")
	clone = filepath.Join(root, "work")
	git(t, root, "clone", "-q", upstream, clone)
	return clone, seed
}

type fakeProc struct {
	done chan struct{}
	once sync.Once
	h    claude.Handlers
	run  func(p *fakeProc, text string)
}

func (p *fakeProc) Send(text string) error              { go p.run(p, text); return nil }
func (p *fakeProc) Interrupt(ctx context.Context) error { return nil }
func (p *fakeProc) Shutdown(time.Duration)              { p.once.Do(func() { close(p.done) }) }
func (p *fakeProc) Done() <-chan struct{}               { return p.done }

type harness struct {
	t       *testing.T
	app     *App
	mu      sync.Mutex
	starts  []claude.Options
	prompts []string
	onSend  func(o claude.Options, text string)
}

func newApp(t *testing.T) *harness {
	t.Helper()
	t.Setenv("LAYER8_HOME", t.TempDir())
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t}
	start := func(o claude.Options, hd claude.Handlers) (sessions.Proc, error) {
		h.mu.Lock()
		h.starts = append(h.starts, o)
		h.mu.Unlock()
		return &fakeProc{done: make(chan struct{}), h: hd, run: func(p *fakeProc, text string) {
			h.mu.Lock()
			h.prompts = append(h.prompts, text)
			fn := h.onSend
			h.mu.Unlock()
			if fn != nil {
				fn(o, text)
			}
			p.h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "c-" + filepath.Base(o.Cwd)})
			p.h.OnMessage(claude.Message{Type: "result", Subtype: "success", TotalCostUSD: 0.01})
		}}, nil
	}
	app, err := New(Options{Paths: paths, Version: "test", Start: start})
	if err != nil {
		t.Fatal(err)
	}
	cfg := app.Config.Get()
	cfg.UseTmux = false
	cfg.AutoStartSlices = false
	if err := app.Config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Shutdown)
	h.app = app
	return h
}

func (h *harness) waitIdle(id string) store.Session {
	h.t.Helper()
	for i := 0; i < 400; i++ {
		s, _ := h.app.Store.Session(id)
		if s.Status == "idle" || s.Status == "failed" {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("session never finished")
	return store.Session{}
}

const planMD = "# Plan\n\n## pr1: Schema\n\n## pr2: API\n\n```yaml\nprs:\n  - id: pr1\n    title: Schema\n    depends_on: []\n  - id: pr2\n    title: API\n    depends_on: [pr1]\n```\n"

// ---- tests

func TestTaskSlicesSyncAndConflicts(t *testing.T) {
	h := newApp(t)
	a := h.app
	ctx := context.Background()
	work, teammate := repo(t)

	ws, err := a.AddWorkspace(ctx, work)
	if err != nil || !ws.IsGit || ws.DefaultBranch != "main" {
		t.Fatalf("workspace %+v %v", ws, err)
	}
	task, err := a.CreateTask(ctx, NewTask{WorkspaceID: ws.ID, Title: "Resumable uploads"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Branch != "layer8/resumable-uploads" || task.BaseRef != "origin/main" || task.BaseSHA == "" {
		t.Fatalf("task %+v", task)
	}
	if git(t, task.WorktreePath, "branch", "--show-current") != task.Branch {
		t.Fatal("worktree not on task branch")
	}

	// Research session: runs in the task worktree with the artifacts dir added.
	s, err := a.StartSession(ctx, StartSession{TaskID: task.ID, Kind: "research"})
	if err != nil {
		t.Fatal(err)
	}
	h.waitIdle(s.ID)
	if h.starts[0].Cwd != task.WorktreePath || h.starts[0].AddDirs[0] != task.ArtifactsPath ||
		!strings.Contains(h.starts[0].AppendSystemPrompt, task.ArtifactsPath) || !strings.Contains(h.prompts[0], "research.md") {
		t.Fatalf("launch %+v / %q", h.starts[0], h.prompts[0])
	}

	if _, err := a.CreateSlices(ctx, task.ID); err == nil {
		t.Fatal("slices without a plan should fail")
	}
	write(t, filepath.Join(task.ArtifactsPath, "plan.md"), planMD)
	slices, err := a.CreateSlices(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(slices) != 2 || slices[1].ParentKey != "pr1" || slices[0].Branch != "layer8/resumable-uploads-pr1" {
		t.Fatalf("slices %+v", slices)
	}
	again, _ := a.CreateSlices(ctx, task.ID)
	if len(again) != 2 {
		t.Fatal("CreateSlices is not idempotent")
	}
	pr1, pr2 := slices[0], slices[1]

	// Slice session: prompt names the slice and the parent's handoff.
	s2, err := a.StartSession(ctx, StartSession{TaskID: task.ID, SliceID: pr2.ID, Kind: "implement"})
	if err != nil {
		t.Fatal(err)
	}
	h.waitIdle(s2.ID)
	last := h.prompts[len(h.prompts)-1]
	if s2.Kind != "slice" || !strings.Contains(last, "`pr2`") || !strings.Contains(last, "handoff-pr1.md") {
		t.Fatalf("slice prompt (%s): %s", s2.Kind, last)
	}

	// Work on the stack, while a teammate lands an unrelated change on main.
	pr1Tip := commitFile(t, pr1.WorktreePath, "schema.sql", "create table x;\n", "schema")
	git(t, pr2.WorktreePath, "rebase", "-q", pr1.Branch)
	commitFile(t, pr2.WorktreePath, "api.go", "package api\n", "api")
	commitFile(t, teammate, "other.txt", "unrelated\n", "teammate change")
	git(t, teammate, "push", "-q", "origin", "HEAD:main")
	// pr2 was rebased by hand above; tell layer8 where its base is now.
	x2, _ := a.Store.Slice(pr2.ID)
	x2.BaseSHA = pr1Tip
	a.Store.UpdateSlice(x2)

	a.SyncAll(ctx)
	main := git(t, work, "rev-parse", "origin/main")
	x1, _ := a.Store.Slice(pr1.ID)
	x2, _ = a.Store.Slice(pr2.ID)
	if x1.SyncState != "ok" || x2.SyncState != "ok" {
		t.Fatalf("sync states %q (%s) / %q (%s)", x1.SyncState, x1.SyncMessage, x2.SyncState, x2.SyncMessage)
	}
	if git(t, pr1.WorktreePath, "merge-base", "HEAD", "origin/main") != main {
		t.Fatal("pr1 not rebased onto main")
	}
	if got := git(t, pr2.WorktreePath, "merge-base", "HEAD", pr1.Branch); got != git(t, pr1.WorktreePath, "rev-parse", "HEAD") {
		t.Fatal("pr2 not stacked on the new pr1")
	}
	if n := git(t, pr2.WorktreePath, "rev-list", "--count", pr1.Branch+"..HEAD"); n != "1" {
		t.Fatalf("pr2 should carry exactly its own commit, has %s", n)
	}

	// Diff of pr2 against its parent shows only api.go.
	d, err := a.Diff(ctx, task.ID, pr2.ID, "")
	if err != nil || len(d.Files) != 1 || d.Files[0].NewPath != "api.go" || len(d.Commits) != 1 {
		t.Fatalf("diff %+v %v", d, err)
	}

	// A dirty worktree is never rebased.
	commitFile(t, teammate, "more.txt", "x\n", "more")
	git(t, teammate, "push", "-q", "origin", "HEAD:main")
	write(t, filepath.Join(pr1.WorktreePath, "wip.txt"), "uncommitted")
	a.SyncAll(ctx)
	x1, _ = a.Store.Slice(pr1.ID)
	if x1.SyncState != "dirty" {
		t.Fatalf("dirty worktree state = %q", x1.SyncState)
	}
	os.Remove(filepath.Join(pr1.WorktreePath, "wip.txt"))

	// A conflicting change on main: the rebase is aborted and reported.
	commitFile(t, teammate, "schema.sql", "something else;\n", "conflicting")
	git(t, teammate, "push", "-q", "origin", "HEAD:main")
	before := git(t, pr1.WorktreePath, "rev-parse", "HEAD")
	a.SyncAll(ctx)
	x1, _ = a.Store.Slice(pr1.ID)
	if x1.SyncState != "conflict" || git(t, pr1.WorktreePath, "rev-parse", "HEAD") != before {
		t.Fatalf("conflict handling: state %q", x1.SyncState)
	}
	if st := git(t, pr1.WorktreePath, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree left dirty after aborted rebase: %s", st)
	}
	cs, err := a.ResolveConflicts(ctx, pr1.ID)
	if err != nil {
		t.Fatal(err)
	}
	h.waitIdle(cs.ID)
	if !strings.Contains(h.prompts[len(h.prompts)-1], "git rebase --onto origin/main") {
		t.Fatal(h.prompts[len(h.prompts)-1])
	}
}

func TestMergedParentReparentsChild(t *testing.T) {
	h := newApp(t)
	a := h.app
	ctx := context.Background()
	work, teammate := repo(t)
	ws, _ := a.AddWorkspace(ctx, work)
	task, _ := a.CreateTask(ctx, NewTask{WorkspaceID: ws.ID, Title: "Stack"})
	write(t, filepath.Join(task.ArtifactsPath, "plan.md"), planMD)
	slices, err := a.CreateSlices(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	pr1, pr2 := slices[0], slices[1]
	pr1Tip := commitFile(t, pr1.WorktreePath, "a.txt", "a\n", "pr1 work")
	a.SyncAll(ctx) // pr2 picks up pr1's commit
	commitFile(t, pr2.WorktreePath, "b.txt", "b\n", "pr2 work")
	if git(t, pr2.WorktreePath, "merge-base", "HEAD", pr1.Branch) != pr1Tip {
		t.Fatal("setup: pr2 not on pr1")
	}

	// pr1 is squash-merged into main by a teammate.
	git(t, teammate, "pull", "-q", "origin", "main")
	commitFile(t, teammate, "a.txt", "a\n", "pr1 (squashed)")
	git(t, teammate, "push", "-q", "origin", "HEAD:main")
	x1, _ := a.Store.Slice(pr1.ID)
	x1.PRState = "MERGED"
	a.Store.UpdateSlice(x1)

	a.SyncAll(ctx)
	x2, _ := a.Store.Slice(pr2.ID)
	if x2.SyncState != "ok" {
		t.Fatalf("pr2 %q: %s", x2.SyncState, x2.SyncMessage)
	}
	if git(t, pr2.WorktreePath, "merge-base", "HEAD", "origin/main") != git(t, work, "rev-parse", "origin/main") {
		t.Fatal("pr2 not moved onto main after its parent merged")
	}
	if n := git(t, pr2.WorktreePath, "rev-list", "--count", "origin/main..HEAD"); n != "1" {
		t.Fatalf("pr2 should only carry its own commit, has %s", n)
	}
	task2, _ := a.Store.Task(task.ID)
	if got := a.parentBranch(task2, x2); got != "main" {
		t.Fatalf("PR base after parent merged = %q", got)
	}
}

func TestPlainFolderAndAPI(t *testing.T) {
	h := newApp(t)
	a := h.app
	dir := t.TempDir()
	srv := httptest.NewServer(a.Handler(Hooks{}))
	defer srv.Close()

	call := func(method, path, body string, out any) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if out != nil {
			json.NewDecoder(res.Body).Decode(out)
		}
		return res.StatusCode
	}
	var ws store.Workspace
	if code := call("POST", "/api/workspaces", `{"path":"`+dir+`"}`, &ws); code != 200 || ws.IsGit {
		t.Fatalf("add workspace %d %+v", code, ws)
	}
	var task store.Task
	if code := call("POST", "/api/tasks", `{"workspaceId":"`+ws.ID+`","title":"Notes"}`, &task); code != 200 || task.WorktreePath != ws.Path {
		t.Fatalf("task %d %+v", code, task)
	}
	var e map[string]string
	if code := call("POST", "/api/tasks", `{"workspaceId":"`+ws.ID+`"}`, &e); code != 422 || e["error"] == "" {
		t.Fatalf("missing title: %d %v", code, e)
	}
	if code := call("POST", "/api/tasks/"+task.ID+"/slices", ``, &e); code != 422 {
		t.Fatalf("slices on plain folder: %d", code)
	}
	var sess store.Session
	if code := call("POST", "/api/sessions", `{"taskId":"`+task.ID+`","kind":"chat","prompt":"hello there"}`, &sess); code != 200 {
		t.Fatalf("session %d", code)
	}
	h.waitIdle(sess.ID)
	var evs []store.Event
	call("GET", "/api/sessions/"+sess.ID+"/events", "", &evs)
	if len(evs) < 2 || evs[0].Type != "user" {
		t.Fatalf("events %+v", evs)
	}
	var cfg config.Config
	call("PUT", "/api/config", `{"linearApiKey":"lin_secret","maxConcurrentSessions":3}`, nil)
	call("GET", "/api/config", "", &cfg)
	if cfg.LinearAPIKey != secretSet || cfg.MaxConcurrentSessions != 3 {
		t.Fatalf("config %+v", cfg)
	}
	call("PUT", "/api/config", `{"linearApiKey":"`+secretSet+`","maxConcurrentSessions":4}`, nil)
	if a.Config.Get().LinearAPIKey != "lin_secret" {
		t.Fatal("masked secret overwrote the real one")
	}
	if code := call("GET", "/api/tasks/"+task.ID+"/artifacts/..%2Fconfig.json", "", &e); code == 200 {
		t.Fatal("path traversal allowed")
	}
	if code := call("POST", "/api/open-url", `{"url":"file:///etc/passwd"}`, &e); code != 400 {
		t.Fatalf("open-url accepted file: (%d)", code)
	}

	// Comments are delivered to the latest session and marked sent.
	call("POST", "/api/comments", `{"taskId":"`+task.ID+`","target":"artifact","path":"plan.md","line":3,"quote":"x","body":"why not y?"}`, nil)
	var got store.Session
	if code := call("POST", "/api/comments/send", `{"taskId":"`+task.ID+`"}`, &got); code != 200 || got.ID != sess.ID {
		t.Fatalf("send comments %d %+v", code, got)
	}
	h.waitIdle(sess.ID)
	if !strings.Contains(h.prompts[len(h.prompts)-1], "plan.md:3") || !strings.Contains(h.prompts[len(h.prompts)-1], "why not y?") {
		t.Fatal(h.prompts[len(h.prompts)-1])
	}
	var st State
	call("GET", "/api/state", "", &st)
	if len(st.Tasks) != 1 || st.Version != "test" {
		t.Fatalf("state %+v", st)
	}
}
