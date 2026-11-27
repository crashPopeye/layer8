package tasks_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

type env struct {
	t      *testing.T
	tree   *tree.Tree
	svc    *tasks.Service
	home   string
	repo   string
	opened [][]string
}

// gitRepo creates a repository on branch trunk with one commit and returns its resolved path.
func gitRepo(t *testing.T, name string) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	dir = filepath.Join(dir, name)
	git(t, "", "init", "-q", "-b", "trunk", dir)
	git(t, dir, "commit", "-q", "--allow-empty", "-m", "init")
	return dir
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	out, err := gitx.Run(t.Context(), ".", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, tree: tree.New(db), home: t.TempDir(), repo: gitRepo(t, "api-gateway")}
	projects.Register(e.tree, db, "test")
	e.svc = tasks.Register(e.tree, db, tasks.Options{
		Home: e.home,
		Apps: config.Apps{Terminal: "iTerm", Editor: "Cursor"},
		Open: func(_ context.Context, args ...string) error { e.opened = append(e.opened, args); return nil },
	})
	e.ok("app", "add-project", `{"path":"`+e.repo+`"}`)
	return e
}

func (e *env) do(target tree.Addr, action, args string) tree.Result {
	e.t.Helper()
	return e.tree.Do(e.t.Context(), tree.Actor{Kind: "user"}, []tree.Op{{Target: target, Action: action, Args: json.RawMessage(args)}})[0]
}

func (e *env) ok(target tree.Addr, action, args string) tree.Result {
	e.t.Helper()
	r := e.do(target, action, args)
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

func names(n tree.Node) string {
	var out []string
	for _, a := range n.Actions {
		out = append(out, a.Name)
	}
	return strings.Join(out, " ")
}

func TestTaskLifecycle(t *testing.T) {
	e := newEnv(t)

	r := e.ok("project:api-gateway", "create-task", `{"title":"Rate-limit uploads","slug":"ENG-482"}`)
	if r.Addr != "task:eng-482" {
		t.Fatalf("addr = %q", r.Addr)
	}
	root := e.get("task:eng-482")
	wt := filepath.Join(e.home, "worktrees", "api-gateway", "eng-482")
	if root.Fields["worktree"] != wt || root.Fields["branch"] != "eng-482" || root.Fields["base"] != "trunk" || root.Fields["status"] != "active" {
		t.Fatalf("fields = %v", root.Fields)
	}
	if got := git(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); got != "eng-482" {
		t.Fatalf("worktree branch = %q", got)
	}
	if _, err := os.Stat(filepath.Join(e.home, "tasks", "eng-482")); err != nil {
		t.Fatal("artifacts folder missing")
	}
	if got := names(root); got != "create-subtask done archive open" {
		t.Fatalf("actions = %q", got)
	}
	if p := e.get("project:api-gateway"); len(p.Children) != 1 || p.Children[0].Addr != "task:eng-482" || !slices.ContainsFunc(p.Actions, func(a tree.Action) bool { return a.Name == "create-task" }) {
		t.Fatalf("project = %+v", p)
	}

	if r := e.do("project:api-gateway", "create-task", `{"title":"again","slug":"eng-482"}`); r.OK || !strings.Contains(r.Message, "already exists") {
		t.Fatalf("duplicate slug: %+v", r)
	}
	e.ok("project:api-gateway", "create-task", `{"title":"Fix: the bug!"}`)
	if r := e.ok("project:api-gateway", "create-task", `{"title":"fix the bug"}`); r.Addr != "task:fix-the-bug-2" {
		t.Fatalf("derived slug = %q", r.Addr)
	}

	git(t, wt, "commit", "-q", "--allow-empty", "-m", "root work")
	e.ok("task:eng-482", "create-subtask", `{"name":"pr1-store"}`)
	e.ok("task:eng-482", "create-subtask", `{"name":"PR2 middleware","base":"pr1-store"}`)
	t1, err := e.svc.Lookup(t.Context(), "task:eng-482/pr1-store")
	if err != nil {
		t.Fatal(err)
	}
	if t1.Slug != "eng-482--pr1-store" || t1.Branch != "eng-482-pr1-store" || t1.Base != "eng-482" || t1.Root() != "eng-482" || t1.Artifacts != filepath.Join(e.home, "tasks", "eng-482") {
		t.Fatalf("pr1 = %+v", t1)
	}
	if git(t, t1.Worktree, "rev-parse", "HEAD") != git(t, wt, "rev-parse", "HEAD") {
		t.Fatal("sub-task is not stacked on its parent's head")
	}
	pr2 := e.get("task:eng-482/pr2-middleware")
	if pr2.Fields["base"] != "eng-482-pr1-store" || pr2.Fields["parent"] != tree.Addr("task:eng-482") {
		t.Fatalf("pr2 = %v", pr2.Fields)
	}
	if kids := e.get("task:eng-482").Children; len(kids) != 2 || kids[1].Addr != "task:eng-482/pr2-middleware" {
		t.Fatalf("children = %+v", kids)
	}

	e.ok("task:eng-482/pr1-store", "open", `{"target":"editor"}`)
	e.ok("task:eng-482/pr1-store", "open", `{"target":"finder"}`)
	if want := [][]string{{"-a", "Cursor", t1.Worktree}, {t1.Worktree}}; !slices.EqualFunc(e.opened, want, slices.Equal) {
		t.Fatalf("opened = %v", e.opened)
	}

	e.ok("task:eng-482/pr1-store", "done", "")
	if got := names(e.get("task:eng-482/pr1-store")); got != "reopen archive open" {
		t.Fatalf("done actions = %q", got)
	}
	r = e.ok("task:eng-482", "archive", "")
	if !strings.Contains(r.Message, "archived 3 task(s)") {
		t.Fatalf("archive = %q", r.Message)
	}
	for _, dir := range []string{wt, t1.Worktree} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("worktree %s still on disk", dir)
		}
	}
	if git(t, e.repo, "branch", "--list", "eng-482-pr1-store") == "" {
		t.Fatal("archive must keep branches")
	}
	if got := names(e.get("task:eng-482/pr2-middleware")); got != "reopen" {
		t.Fatalf("archived actions = %q", got)
	}

	e.ok("task:eng-482", "reopen", "")
	if git(t, wt, "rev-parse", "--abbrev-ref", "HEAD") != "eng-482" || e.get("task:eng-482").Fields["status"] != "active" {
		t.Fatal("reopen did not restore the worktree")
	}
	if r := e.do("project:api-gateway", "remove", ""); r.OK || !strings.Contains(r.Message, "still has 5 tasks") {
		t.Fatalf("remove project with tasks: %+v", r)
	}
}

func TestArtifacts(t *testing.T) {
	e := newEnv(t)
	e.ok("project:api-gateway", "create-task", `{"title":"t","slug":"eng-1"}`)
	e.ok("task:eng-1", "create-subtask", `{"name":"pr1"}`)

	ghost := e.get("artifact:eng-1/notes/05-plan.md")
	if ghost.Fields["exists"] != false || names(ghost) != "write" {
		t.Fatalf("missing artifact = %+v", ghost)
	}
	e.ok("artifact:eng-1/notes/05-plan.md", "write", `{"content":"# Plan"}`)
	e.ok("artifact:eng-1/01-ticket.md", "write", `{"content":"ticket"}`)
	os.WriteFile(filepath.Join(e.home, "tasks", "eng-1", ".hidden"), []byte("x"), 0o644)

	a := e.get("artifact:eng-1/notes/05-plan.md")
	if a.Fields["size"] != int64(6) || a.Fields["type"] != "text/markdown" || names(a) != "write delete" {
		t.Fatalf("artifact = %+v", a)
	}
	data, _ := os.ReadFile(a.Fields["path"].(string))
	if string(data) != "# Plan" {
		t.Fatalf("content = %q", data)
	}
	kids := e.get("task:eng-1").Children
	if len(kids) != 3 || kids[0].Kind != "task" || kids[1].Addr != "artifact:eng-1/01-ticket.md" || kids[2].Addr != "artifact:eng-1/notes/05-plan.md" {
		t.Fatalf("root children = %+v", kids)
	}
	sub := e.get("task:eng-1/pr1")
	if files := sub.Fields["artifactFiles"].([]tree.Summary); len(files) != 2 || len(sub.Children) != 0 {
		t.Fatalf("sub-task artifacts = %+v children = %+v", files, sub.Children)
	}

	e.ok("artifact:eng-1/01-ticket.md", "delete", "")
	if len(e.get("task:eng-1").Fields["artifactFiles"].([]tree.Summary)) != 1 {
		t.Fatal("delete did not remove the artifact")
	}
	for _, addr := range []tree.Addr{"artifact:eng-1/../x", "artifact:eng-1//etc/passwd", "artifact:eng-1--pr1/x.md", "artifact:nope/x.md"} {
		if r := e.do(addr, "write", `{"content":"x"}`); r.OK {
			t.Fatalf("%s accepted", addr)
		}
	}
}

func TestWatchReportsDiskChanges(t *testing.T) {
	e := newEnv(t)
	e.ok("project:api-gateway", "create-task", `{"title":"t","slug":"eng-2"}`)
	changes := make(chan []tree.Addr, 16)
	e.tree.OnChange = func(addrs []tree.Addr) { changes <- addrs }
	go e.svc.Watch(t.Context(), 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	os.WriteFile(filepath.Join(e.home, "tasks", "eng-2", "research.md"), []byte("r"), 0o644)
	timeout := time.After(5 * time.Second)
	for {
		select {
		case addrs := <-changes:
			if slices.Contains(addrs, "artifact:eng-2/research.md") && slices.Contains(addrs, "task:eng-2") {
				return
			}
		case <-timeout:
			t.Fatal("no change reported")
		}
	}
}
