package linear_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/linear"
	"github.com/crashPopeye/layer8/internal/testkit"
	"github.com/crashPopeye/layer8/internal/tree"
)

func setupTasks(t *testing.T) (*testkit.Env, *fakeLinear) {
	t.Helper()
	fake := newFake()
	api := httptest.NewServer(fake)
	t.Cleanup(api.Close)
	c := linear.New(filepath.Join(t.TempDir(), "linear.json"), "")
	c.APIEndpoint = api.URL
	e := testkit.New(t, map[string]string{"a.txt": "a\n"}, func(e *testkit.Env) {
		e.Tree.Extend("task", tree.Action{Name: "open-pr", Help: "stub",
			Run: func(context.Context, tree.Node, json.RawMessage, tree.Actor) (tree.Result, error) {
				return tree.Result{Message: "opened PR #7", Data: map[string]any{"number": 7, "url": "https://github.com/o/api/pull/7"}}, nil
			}})
		linear.Register(e.Tree, c, "")
		linear.RegisterTasks(e.Tree, c, e.Tasks)
	})
	if _, err := c.Connect(t.Context(), apiKey); err != nil {
		t.Fatal(err)
	}
	return e, fake
}

func linkOf(t *testing.T, n tree.Node) linear.Link {
	t.Helper()
	var l linear.Link
	raw, _ := n.Fields["linear"].(json.RawMessage)
	json.Unmarshal(raw, &l)
	return l
}

func ticket(t *testing.T, e *testkit.Env, root string) string {
	t.Helper()
	data, _ := os.ReadFile(filepath.Join(e.Home, "tasks", root, "01-ticket.md"))
	return string(data)
}

func TestCreateTaskFromLinear(t *testing.T) {
	e, fake := setupTasks(t)
	fake.answer("Issue", strings.Replace(responses["Issue"], `"eng-482-rate-limit-uploads"`, `"Alex/ENG-482: rate limit..uploads "`, 1))

	res := e.OK("project:api", "create-task-from-linear", map[string]any{"issue": "ENG-482"})
	if res.Addr != "task:eng-482" || !strings.HasSuffix(res.Message, "linked to ENG-482") {
		t.Fatalf("result = %+v", res)
	}
	n := e.Get("task:eng-482")
	want := linear.Link{ID: "ENG-482", URL: "https://linear.app/acme/issue/ENG-482", Title: "Rate-limit uploads", State: linear.State{Name: "In Progress", Type: "started"}}
	if n.Title != "Rate-limit uploads" || n.Fields["branch"] != "Alex/ENG-482-rate-limit-uploads" || linkOf(t, n) != want {
		t.Fatalf("task = %s %v %+v", n.Title, n.Fields["branch"], linkOf(t, n))
	}
	if !strings.HasPrefix(ticket(t, e, "eng-482"), "# ENG-482 Rate-limit uploads\n") {
		t.Fatalf("01-ticket.md = %q", ticket(t, e, "eng-482"))
	}
	if fake.count("Issue") != 1 {
		t.Fatalf("a fresh link must be served from the cache, Issue calls = %d", fake.count("Issue"))
	}
	if r := e.Do("project:api", "create-task-from-linear", map[string]any{"issue": "ENG-482"}); r.OK || !strings.Contains(r.Message, "already exists") {
		t.Fatalf("second task for the issue = %+v", r)
	}
}

func TestLinkUnlinkAndLazyState(t *testing.T) {
	e, fake := setupTasks(t)
	e.OK("project:api", "create-task", map[string]any{"title": "Other"})
	if n := e.Get("task:other"); !testkit.Has(n, "link-linear") || testkit.Has(n, "unlink-linear") || testkit.Has(n, "refresh-linear") {
		t.Fatalf("unlinked actions = %+v", n.Actions)
	}

	e.OK("task:other", "link-linear", map[string]any{"issue": "ENG-482"})
	if linkOf(t, e.Get("task:other")).ID != "ENG-482" || !strings.HasPrefix(ticket(t, e, "other"), "# ENG-482") {
		t.Fatal("link-linear must set the field and write 01-ticket.md")
	}

	fake.answer("Issue", strings.Replace(responses["Issue"], `"name":"In Progress"`, `"name":"Done"`, 1))
	e.OK("task:other", "refresh-linear", nil)
	if st := linkOf(t, e.Get("task:other")).State.Name; st != "Done" || !strings.Contains(ticket(t, e, "other"), "**State:** Done") {
		t.Fatalf("after refresh state = %s, ticket = %s", st, ticket(t, e, "other"))
	}

	e.OK("task:other", "unlink-linear", nil)
	n := e.Get("task:other")
	if linkOf(t, n).ID != "" || !testkit.Has(n, "link-linear") || testkit.Has(n, "unlink-linear") {
		t.Fatalf("after unlink = %+v", n.Fields["linear"])
	}

	e.OK("project:api", "create-task", map[string]any{"title": "Docs"})
	if err := e.Tasks.SetLinear(t.Context(), "task:docs", json.RawMessage(`{"id":"ENG-9","url":"u","title":"Upload limits doc","state":{"name":"Todo","type":"unstarted"}}`)); err != nil {
		t.Fatal(err)
	}
	fake.answer("Issue", `{"issue":{"id":"uuid-9","identifier":"ENG-9","title":"Upload limits doc","url":"u","state":{"name":"In Review","type":"started"}}}`)
	before := fake.count("Issue")
	if st := linkOf(t, e.Get("task:docs")).State.Name; st != "In Review" {
		t.Fatalf("read state = %s, want the live one", st)
	}
	e.Get("task:docs")
	if fake.count("Issue") != before+1 {
		t.Fatalf("state must be cached between reads: %d Issue calls", fake.count("Issue")-before)
	}
	var stored string
	e.DB.QueryRow(`SELECT linear FROM tasks WHERE slug = 'docs'`).Scan(&stored)
	if !strings.Contains(stored, "In Review") {
		t.Fatalf("changed state must be persisted, got %s", stored)
	}
}

func TestSubIssueOnSubtask(t *testing.T) {
	e, fake := setupTasks(t)
	e.OK("project:api", "create-task", map[string]any{"title": "Plain"})
	if r := e.Do("task:plain", "create-subtask", map[string]any{"name": "pr1", "linear": true}); r.OK || !strings.Contains(r.Message, "not linked") {
		t.Fatalf("sub-issue under an unlinked task = %+v", r)
	}
	if _, err := e.Tasks.Lookup(t.Context(), "task:plain/pr1"); err == nil {
		t.Fatal("no sub-task must be created")
	}

	e.OK("project:api", "create-task-from-linear", map[string]any{"issue": "ENG-482"})
	res := e.OK("task:eng-482", "create-subtask", map[string]any{"name": "pr1-store", "linear": true})
	if res.Addr != "task:eng-482/pr1-store" || !strings.HasSuffix(res.Message, "linked to new sub-issue ENG-500") {
		t.Fatalf("result = %+v", res)
	}
	input := fake.vars["CreateIssue"]["input"].(map[string]any)
	if input["parentId"] != "uuid-482" || input["title"] != "pr1-store" {
		t.Fatalf("issueCreate input = %v", input)
	}
	if linkOf(t, e.Get("task:eng-482/pr1-store")).ID != "ENG-500" {
		t.Fatal("sub-task must be linked to the sub-issue")
	}

	e.OK("task:eng-482", "create-subtask", map[string]any{"name": "pr2"})
	if fake.count("CreateIssue") != 1 || linkOf(t, e.Get("task:eng-482/pr2")).ID != "" {
		t.Fatal("create-subtask without linear must not touch Linear")
	}
}

func TestOpenPRAttachesToIssue(t *testing.T) {
	e, fake := setupTasks(t)
	e.OK("project:api", "create-task", map[string]any{"title": "Plain"})
	if r := e.OK("task:plain", "open-pr", nil); r.Message != "opened PR #7" || fake.count("AttachPR") != 0 {
		t.Fatalf("unlinked open-pr = %+v", r)
	}

	e.OK("project:api", "create-task-from-linear", map[string]any{"issue": "ENG-482"})
	r := e.OK("task:eng-482", "open-pr", nil)
	if r.Message != "opened PR #7, attached to ENG-482" || fake.vars["AttachPR"]["issueId"] != "ENG-482" || fake.vars["AttachPR"]["url"] != "https://github.com/o/api/pull/7" {
		t.Fatalf("open-pr = %+v, vars = %v", r, fake.vars["AttachPR"])
	}

	fake.answer("AttachPR", `{"attachmentLinkGitHubPR":{"success":false}}`)
	if r := e.OK("task:eng-482", "open-pr", nil); !strings.Contains(r.Message, "attaching to ENG-482 failed") {
		t.Fatalf("a Linear failure must not fail open-pr: %+v", r)
	}
}
