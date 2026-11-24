package gh

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type call struct {
	dir, stdin string
	args       []string
}

// fake answers gh calls with canned output and records them.
func fake(out string, err error, calls *[]call) CLI {
	return CLI{Run: func(ctx context.Context, dir, stdin string, args ...string) ([]byte, error) {
		*calls = append(*calls, call{dir, stdin, args})
		return []byte(out), err
	}}
}

func TestPRCreate(t *testing.T) {
	var calls []call
	c := fake("Creating pull request for l8/x into main\n\nhttps://github.com/o/r/pull/142\n", nil, &calls)
	pr, err := c.PRCreate(context.Background(), "/wt", NewPR{Head: "l8/x", Base: "main", Title: "Add x", Body: "## Why\nbody", Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 142 || pr.URL != "https://github.com/o/r/pull/142" || pr.State != "OPEN" || !pr.Draft {
		t.Fatalf("pr = %+v", pr)
	}
	want := []string{"pr", "create", "--head", "l8/x", "--base", "main", "--title", "Add x", "--body-file", "-", "--draft"}
	if got := calls[0]; !slices.Equal(got.args, want) || got.stdin != "## Why\nbody" || got.dir != "/wt" {
		t.Fatalf("call = %+v", got)
	}
}

const viewJSON = `{
  "number": 142, "url": "https://github.com/o/r/pull/142", "state": "OPEN", "isDraft": true,
  "reviewDecision": "CHANGES_REQUESTED", "mergeable": "MERGEABLE", "headRefName": "l8/x", "baseRefName": "main",
  "statusCheckRollup": [
    {"__typename": "CheckRun", "name": "test", "status": "COMPLETED", "conclusion": "FAILURE", "detailsUrl": "https://ci/1"},
    {"__typename": "CheckRun", "name": "vet", "status": "COMPLETED", "conclusion": "SUCCESS", "detailsUrl": "https://ci/2"},
    {"__typename": "CheckRun", "name": "build", "status": "IN_PROGRESS", "conclusion": "", "detailsUrl": "https://ci/3"},
    {"__typename": "StatusContext", "context": "deploy/preview", "state": "ERROR", "targetUrl": "https://ci/4"}
  ]
}`

func TestPRView(t *testing.T) {
	var calls []call
	pr, err := fake(viewJSON, nil, &calls).PRView(context.Background(), "/wt", "l8/x")
	if err != nil {
		t.Fatal(err)
	}
	want := PR{
		Number: 142, URL: "https://github.com/o/r/pull/142", State: "OPEN", Draft: true,
		ReviewDecision: "CHANGES_REQUESTED", Mergeable: "MERGEABLE", Head: "l8/x", Base: "main",
		Checks: []Check{
			{"test", "FAILURE", "https://ci/1"},
			{"vet", "SUCCESS", "https://ci/2"},
			{"build", "PENDING", "https://ci/3"},
			{"deploy/preview", "ERROR", "https://ci/4"},
		},
		Threads: []Thread{},
	}
	if !reflect.DeepEqual(pr, want) {
		t.Fatalf("pr = %+v", pr)
	}
	var failed []string
	for _, c := range pr.Checks {
		if c.Failed() {
			failed = append(failed, c.Name)
		}
	}
	if !slices.Equal(failed, []string{"test", "deploy/preview"}) {
		t.Fatalf("failed = %v", failed)
	}
	if args := strings.Join(calls[0].args, " "); !strings.HasPrefix(args, "pr view l8/x --json ") {
		t.Fatalf("args = %s", args)
	}
}

func TestPRViewNone(t *testing.T) {
	var calls []call
	_, err := fake("", errors.New(`gh pr: no pull requests found for branch "l8/x"`), &calls).PRView(context.Background(), "/wt", "l8/x")
	if !errors.Is(err, ErrNoPR) {
		t.Fatalf("err = %v", err)
	}
}

const threadsJSON = `{"data": {"repository": {"pullRequest": {"reviewThreads": {"nodes": [
  {"isResolved": false, "isOutdated": false, "path": "cmd/l8/main.go", "line": 42, "originalLine": 40,
   "comments": {"nodes": [
     {"author": {"login": "alice"}, "body": "use --setting-sources explicitly", "url": "https://github.com/o/r/pull/142#c1"},
     {"author": null, "body": "+1", "url": "https://github.com/o/r/pull/142#c2"}]}},
  {"isResolved": true, "isOutdated": false, "path": "README.md", "line": 1, "originalLine": 1, "comments": {"nodes": []}},
  {"isResolved": false, "isOutdated": true, "path": "go.mod", "line": 0, "originalLine": 7,
   "comments": {"nodes": [{"author": {"login": "bob"}, "body": "why this version?", "url": "u3"}]}}
]}}}}}`

func TestThreads(t *testing.T) {
	var calls []call
	threads, err := fake(threadsJSON, nil, &calls).Threads(context.Background(), "/wt", 142)
	if err != nil {
		t.Fatal(err)
	}
	want := []Thread{
		{Path: "cmd/l8/main.go", Line: 42, Comments: []Comment{
			{"alice", "use --setting-sources explicitly", "https://github.com/o/r/pull/142#c1"},
			{"ghost", "+1", "https://github.com/o/r/pull/142#c2"},
		}},
		{Path: "go.mod", Line: 7, Outdated: true, Comments: []Comment{{"bob", "why this version?", "u3"}}},
	}
	if !reflect.DeepEqual(threads, want) {
		t.Fatalf("threads = %+v", threads)
	}
	args := calls[0].args
	if args[0] != "api" || args[1] != "graphql" || !slices.Contains(args, "number=142") || !slices.Contains(args, "owner={owner}") {
		t.Fatalf("args = %v", args)
	}
}
