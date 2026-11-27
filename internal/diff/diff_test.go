package diff_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/diff"
	"github.com/crashPopeye/layer8/internal/testkit"
	"github.com/crashPopeye/layer8/internal/tree"
)

const task = tree.Addr("task:rate-limit")

func setup(t *testing.T) (*testkit.Env, string) {
	t.Helper()
	e := testkit.New(t, map[string]string{
		"a.txt":      "one\ntwo\nthree\n",
		"c.txt":      "keep me\nas I am\n",
		".gitignore": "*.log\n",
	}, func(e *testkit.Env) { diff.Register(e.Tree) })
	res := e.OK("project:api", "create-task", map[string]any{"title": "Rate limit"})
	return e, res.Data.(map[string]string)["worktree"]
}

func paths(d diff.Diff) []string {
	var out []string
	for _, f := range d.Files {
		out = append(out, f.Status+" "+f.Path)
	}
	return out
}

func TestComputeModes(t *testing.T) {
	e, wt := setup(t)
	testkit.Write(t, wt, "a.txt", "one\nTWO\nthree\n")
	testkit.Git(t, wt, "commit", "-q", "-am", "shout")
	testkit.Git(t, wt, "mv", "c.txt", "d.txt")
	testkit.Write(t, wt, "new.txt", "fresh\n")
	testkit.Write(t, wt, "debug.log", "ignored\n")

	committed, err := diff.Compute(t.Context(), wt, "main", diff.Committed)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths(committed); !slices.Equal(got, []string{"modified a.txt"}) {
		t.Fatalf("committed = %v", got)
	}
	var lines []string
	for _, l := range committed.Files[0].Hunks[0].Lines {
		lines = append(lines, strings.Join([]string{l.Kind, itoa(l.Old), itoa(l.New), l.Text}, " "))
	}
	want := []string{"ctx 1 1 one", "del 2 0 two", "add 0 2 TWO", "ctx 3 3 three"}
	if !slices.Equal(lines, want) {
		t.Fatalf("lines = %q", lines)
	}

	uncommitted, _ := diff.Compute(t.Context(), wt, "main", diff.Uncommitted)
	if got := paths(uncommitted); !slices.Equal(got, []string{"renamed d.txt", "added new.txt"}) {
		t.Fatalf("uncommitted = %v", got)
	}
	if uncommitted.Files[0].OldPath != "c.txt" {
		t.Errorf("rename from = %q", uncommitted.Files[0].OldPath)
	}

	all, _ := diff.Compute(t.Context(), wt, "main", diff.All)
	if got := paths(all); len(got) != 3 || all.Added != 2 || all.Deleted != 1 {
		t.Fatalf("all = %v %+v", got, all.Stats)
	}
	if status := testkit.Git(t, wt, "status", "--porcelain"); !strings.Contains(status, "?? new.txt") {
		t.Errorf("the real index changed:\n%s", status)
	}

	n := e.Get(task)
	if n.Fields["changes"] != (diff.Stats{Files: 3, Added: 2, Deleted: 1}) || n.Fields["ahead"] != 1 || n.Fields["behind"] != 0 {
		t.Errorf("fields = %v %v %v", n.Fields["changes"], n.Fields["ahead"], n.Fields["behind"])
	}
	if !testkit.Has(n, "commit") || !testkit.Has(n, "push") {
		t.Errorf("actions = %v", n.Actions)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

func TestParseEdgeCases(t *testing.T) {
	patch := "diff --git a/img.png b/img.png\nnew file mode 100644\nindex 0000000..1111111\nBinary files /dev/null and b/img.png differ\n" +
		"diff --git a/my file.txt b/my file.txt\nindex 1..2 100644\n--- a/my file.txt\n+++ b/my file.txt\n@@ -1,2 +1,2 @@\n-a\n+b\n \n\\ No newline at end of file\n" +
		"diff --git a/gone.txt b/gone.txt\ndeleted file mode 100644\n--- a/gone.txt\n+++ /dev/null\n@@ -1 +0,0 @@\n--- not a header\n"
	files := diff.Parse(patch)
	if len(files) != 3 {
		t.Fatalf("files = %+v", files)
	}
	if !files[0].Binary || files[0].Status != "added" || files[0].Path != "img.png" {
		t.Errorf("binary = %+v", files[0])
	}
	if f := files[1]; f.Path != "my file.txt" || len(f.Hunks[0].Lines) != 3 || f.Hunks[0].Lines[2].Kind != "ctx" {
		t.Errorf("spaces = %+v", f)
	}
	if f := files[2]; f.Status != "deleted" || f.Deleted != 1 || f.Hunks[0].Lines[0].Text != "-- not a header" {
		t.Errorf("deleted = %+v", f)
	}
}

func TestCommitAndPush(t *testing.T) {
	e, wt := setup(t)
	if n := e.Get(task); testkit.Has(n, "commit") || testkit.Has(n, "push") {
		t.Fatalf("clean task offers %v", n.Actions)
	}
	testkit.Write(t, wt, "one.txt", "1\n")
	testkit.Write(t, wt, "two.txt", "2\n")
	testkit.Git(t, wt, "config", "user.name", "t")
	testkit.Git(t, wt, "config", "user.email", "t@t")

	e.OK(task, "commit", map[string]any{"message": "only one", "paths": []string{"one.txt"}})
	if status := testkit.Git(t, wt, "status", "--porcelain"); status != "?? two.txt" {
		t.Fatalf("after partial commit: %q", status)
	}
	e.OK(task, "commit", map[string]any{"message": "the rest"})
	if r := e.Do(task, "commit", map[string]any{"message": "nothing"}); r.OK {
		t.Fatal("commit offered on a clean worktree")
	}

	e.OK(task, "push", nil)
	head := testkit.Git(t, wt, "rev-parse", "HEAD")
	if remote := testkit.Git(t, e.Origin, "rev-parse", "rate-limit"); remote != head {
		t.Fatalf("origin has %s, want %s", remote, head)
	}
	if up := testkit.Git(t, wt, "rev-parse", "--abbrev-ref", "@{upstream}"); up != "origin/rate-limit" {
		t.Errorf("upstream = %s", up)
	}

	testkit.Git(t, wt, "commit", "-q", "--amend", "-m", "rewritten")
	e.OK(task, "push", nil)

	other := t.TempDir() + "/other"
	testkit.Git(t, ".", "clone", "-q", "-b", "rate-limit", e.Origin, other)
	testkit.Git(t, other, "commit", "-q", "--allow-empty", "-m", "someone else")
	testkit.Git(t, other, "push", "-q", "origin", "rate-limit")
	testkit.Git(t, wt, "commit", "-q", "--allow-empty", "-m", "mine")
	if r := e.Do(task, "push", nil); r.OK || !strings.Contains(r.Message, "stale info") {
		t.Fatalf("push over someone else's commit = %+v", r)
	}
}

func TestHandler(t *testing.T) {
	e, wt := setup(t)
	testkit.Write(t, wt, "new.txt", "fresh\n")
	srv := httptest.NewServer(diff.Handler(e.Tree))
	defer srv.Close()

	res, err := http.Get(srv.URL + "?task=task:rate-limit&mode=uncommitted")
	if err != nil {
		t.Fatal(err)
	}
	var d diff.Diff
	json.NewDecoder(res.Body).Decode(&d)
	if res.StatusCode != 200 || len(d.Files) != 1 || d.Files[0].Hunks[0].Lines[0].Text != "fresh" {
		t.Fatalf("%d %+v", res.StatusCode, d)
	}
	for query, code := range map[string]int{"?task=task:nope": 404, "?task=project:api": 404, "?task=task:rate-limit&mode=weird": 400} {
		if res, _ := http.Get(srv.URL + query); res.StatusCode != code {
			t.Errorf("%s = %d, want %d", query, res.StatusCode, code)
		}
	}
}
