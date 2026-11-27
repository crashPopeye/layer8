package github_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/crashPopeye/layer8/internal/diff"
	"github.com/crashPopeye/layer8/internal/github"
	"github.com/crashPopeye/layer8/internal/testkit"
	"github.com/crashPopeye/layer8/internal/tree"
)

// fakeGitHub serves the REST and GraphQL calls the package makes, for repository o/api.
type fakeGitHub struct {
	mu       sync.Mutex
	open     map[string]int
	created  []github.NewPR
	state    string
	graphql  int
	logFetch int
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer t0k" {
		http.Error(w, `{"message":"Bad credentials"}`, 401)
		return
	}
	switch {
	case r.Method == "GET" && r.URL.Path == "/repos/o/api/pulls":
		list := []map[string]int{}
		if n, ok := f.open[strings.TrimPrefix(r.URL.Query().Get("head"), "o:")]; ok {
			list = append(list, map[string]int{"number": n})
		}
		json.NewEncoder(w).Encode(list)
	case r.Method == "POST" && r.URL.Path == "/repos/o/api/pulls":
		var pr github.NewPR
		json.NewDecoder(r.Body).Decode(&pr)
		f.created = append(f.created, pr)
		f.open[pr.Head] = 7
		w.WriteHeader(201)
		w.Write([]byte(`{"number":7}`))
	case r.URL.Path == "/graphql":
		f.graphql++
		var q struct{ Variables struct{ Number int } }
		json.NewDecoder(r.Body).Decode(&q)
		w.Write([]byte(strings.NewReplacer("NUMBER", itoa(q.Variables.Number), "STATE", f.state).Replace(statusJSON)))
	case r.URL.Path == "/repos/o/api/actions/jobs/55/logs":
		http.Redirect(w, r, "/blob/55", http.StatusFound)
	case r.URL.Path == "/blob/55":
		f.logFetch++
		w.Write([]byte("2026-11-27T10:00:00.1234567Z setup\r\n2026-11-27T10:00:01.0000000Z go test ./...\n2026-11-27T10:00:02.0000000Z error: boom\n" +
			"2026-11-27T10:00:03.0000000Z ##[error]Process completed with exit code 1.\n2026-11-27T10:00:04.0000000Z Cleaning up orphan processes\n"))
	default:
		http.NotFound(w, r)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

const statusJSON = `{"data":{"repository":{"pullRequest":{
  "number":NUMBER,"url":"https://github.com/o/api/pull/NUMBER","state":"STATE","isDraft":true,"baseRefName":"rate-limit",
  "mergeable":"MERGEABLE","reviewDecision":"CHANGES_REQUESTED",
  "reviewThreads":{"nodes":[
    {"isResolved":false,"isOutdated":false,"path":"limit.go","line":12,"comments":{"totalCount":2,"nodes":[{"author":{"login":"rev"},"body":"use a const","url":"https://x/c1"}]}},
    {"isResolved":true,"isOutdated":false,"path":"a.go","line":1,"comments":{"totalCount":1,"nodes":[{"author":{"login":"rev"},"body":"done","url":"https://x/c2"}]}}]},
  "commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[
    {"__typename":"CheckRun","name":"test","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://x/run","databaseId":55,"checkSuite":{"app":{"slug":"github-actions"}}},
    {"__typename":"CheckRun","name":"lint","status":"IN_PROGRESS","conclusion":null,"detailsUrl":"https://x/lint","databaseId":56,"checkSuite":{"app":{"slug":"github-actions"}}},
    {"__typename":"StatusContext","context":"deploy","state":"SUCCESS","targetUrl":"https://x/deploy"}]}}}}]}}}}}`

type env struct {
	*testkit.Env
	gh      *fakeGitHub
	svc     *github.Service
	changes [][]tree.Addr
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{gh: &fakeGitHub{open: map[string]int{}, state: "OPEN"}}
	srv := httptest.NewServer(e.gh)
	t.Cleanup(srv.Close)
	client := &github.Client{API: srv.URL, HTTP: srv.Client(), Token: github.GHToken("t0k")}
	e.Env = testkit.New(t, map[string]string{"a.txt": "a\n"}, func(k *testkit.Env) {
		diff.Register(k.Tree)
		e.svc = github.Register(k.Tree, k.DB, client)
	})
	e.Tree.OnChange = func(addrs []tree.Addr) { e.changes = append(e.changes, addrs) }
	testkit.Git(t, e.Repo, "config", "remote.origin.url", "https://github.com/o/api.git")
	testkit.Git(t, e.Repo, "config", "url."+e.Origin+".insteadOf", "https://github.com/o/api.git")
	testkit.Git(t, e.Repo, "config", "user.name", "t")
	testkit.Git(t, e.Repo, "config", "user.email", "t@t")
	return e
}

func (e *env) commit(task tree.Addr) {
	e.T.Helper()
	testkit.Write(e.T, e.Get(task).Fields["worktree"].(string), "change.txt", string(task))
	e.OK(task, "commit", map[string]any{"message": "work"})
}

func TestOpenPRStackedWithArtifactBody(t *testing.T) {
	e := setup(t)
	e.OK("project:api", "create-task", map[string]any{"title": "Rate limit"})
	e.OK("task:rate-limit", "create-subtask", map[string]any{"name": "pr2"})
	e.OK("artifact:rate-limit/pr-description.md", "write", map[string]any{"content": "## Why\nUploads are abused.\n"})
	if testkit.Has(e.Get("task:rate-limit/pr2"), "open-pr") {
		t.Fatal("open-pr offered with nothing to push")
	}
	e.commit("task:rate-limit/pr2")

	res := e.OK("task:rate-limit/pr2", "open-pr", map[string]any{"draft": true})
	want := github.NewPR{Title: "pr2", Body: "## Why\nUploads are abused.\n", Head: "rate-limit-pr2", Base: "rate-limit", Draft: true}
	if len(e.gh.created) != 1 || e.gh.created[0] != want {
		t.Fatalf("created %+v", e.gh.created)
	}
	if !strings.HasPrefix(res.Message, "opened PR #7") {
		t.Errorf("message = %s", res.Message)
	}
	if testkit.Git(t, e.Origin, "rev-parse", "rate-limit-pr2") == "" {
		t.Error("branch not pushed")
	}

	n := e.Get("task:rate-limit/pr2")
	var pr github.PR
	json.Unmarshal(n.Fields["pr"].(json.RawMessage), &pr)
	if pr.Number != 7 || pr.State != "open" || !pr.Draft || pr.Mergeable != "mergeable" || pr.ReviewDecision != "changes_requested" {
		t.Errorf("pr = %+v", pr)
	}
	if pr.ChecksState != "failure" || len(pr.Checks) != 3 || pr.Checks[0].LogTail != "setup\ngo test ./...\nerror: boom\n##[error]Process completed with exit code 1." ||
		pr.Checks[1].State != "pending" || pr.Checks[2].State != "success" {
		t.Errorf("checks = %+v", pr.Checks)
	}
	if pr.UnresolvedThreads != 1 || pr.Threads[0] != (github.Thread{Path: "limit.go", Line: 12, Author: "rev", Body: "use a const", URL: "https://x/c1", Replies: 1}) {
		t.Errorf("threads = %+v", pr.Threads)
	}
	if testkit.Has(n, "open-pr") || !testkit.Has(n, "refresh-pr") {
		t.Errorf("actions = %v", n.Actions)
	}

	e.OK("task:rate-limit/pr2", "refresh-pr", nil)
	if e.gh.logFetch != 1 {
		t.Errorf("log fetched %d times, want once", e.gh.logFetch)
	}
}

func TestOpenPRLinksExisting(t *testing.T) {
	e := setup(t)
	e.OK("project:api", "create-task", map[string]any{"title": "Rate limit"})
	e.commit("task:rate-limit")
	e.gh.open["rate-limit"] = 3
	res := e.OK("task:rate-limit", "open-pr", nil)
	if len(e.gh.created) != 0 || !strings.HasPrefix(res.Message, "linked PR #3") {
		t.Fatalf("created %+v, message %s", e.gh.created, res.Message)
	}
}

func TestRefreshOpenPublishesChanges(t *testing.T) {
	e := setup(t)
	e.OK("project:api", "create-task", map[string]any{"title": "Rate limit"})
	e.commit("task:rate-limit")
	e.OK("task:rate-limit", "open-pr", nil)

	e.changes = nil
	e.svc.RefreshOpen(t.Context())
	if len(e.changes) != 0 {
		t.Fatalf("unchanged PR published %v", e.changes)
	}
	e.gh.state = "MERGED"
	e.svc.RefreshOpen(t.Context())
	if len(e.changes) != 1 || e.changes[0][0] != "task:rate-limit" {
		t.Fatalf("changes = %v", e.changes)
	}
	calls := e.gh.graphql
	e.svc.RefreshOpen(t.Context())
	if e.gh.graphql != calls {
		t.Error("merged PR polled again")
	}
}

func TestRepo(t *testing.T) {
	for url, want := range map[string]string{
		"https://github.com/o/api.git": "o/api",
		"git@github.com:o/api.git":     "o/api",
		"ssh://git@github.com/o/api":   "o/api",
		"https://github.com/o/a.b/":    "o/a.b",
	} {
		dir := t.TempDir()
		testkit.Git(t, dir, "init", "-q")
		testkit.Git(t, dir, "remote", "add", "origin", url)
		if got, err := github.Repo(t.Context(), dir); got != want || err != nil {
			t.Errorf("%s → %q, %v", url, got, err)
		}
	}
}

func TestBadToken(t *testing.T) {
	srv := httptest.NewServer(&fakeGitHub{})
	defer srv.Close()
	c := &github.Client{API: srv.URL, HTTP: srv.Client(), Token: github.GHToken("wrong")}
	if _, err := c.Status(t.Context(), "o/api", 1); err == nil || !strings.Contains(err.Error(), "401 Bad credentials") {
		t.Fatalf("err = %v", err)
	}
}
