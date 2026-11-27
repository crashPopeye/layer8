package comments_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/comments"
	"github.com/crashPopeye/layer8/internal/testkit"
	"github.com/crashPopeye/layer8/internal/tree"
)

type said struct {
	session tree.Addr
	text    string
}

// newEnv builds task feat with sub-task feat/sub, artifact plan.md, and fake sessions that record what they are told.
func newEnv(t *testing.T, sessions map[string]string) (*testkit.Env, *[]said) {
	t.Helper()
	var log []said
	e := testkit.New(t, map[string]string{"main.go": "package main\n"}, func(e *testkit.Env) {
		comments.Register(e.Tree, e.DB)
		e.Tree.Register(tree.Kind{
			Name: "session",
			Get: func(_ context.Context, a tree.Addr) (tree.Node, error) {
				return tree.Node{Fields: map[string]any{"status": sessions[a.Path()]}}, nil
			},
			Actions: []tree.Action{{Name: "say", Args: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"}}}`), Run: func(_ context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
				var a struct{ Text string }
				json.Unmarshal(args, &a)
				log = append(log, said{n.Addr, a.Text})
				return tree.Result{}, nil
			}}},
		})
		e.Tree.AddChildren("task", func(_ context.Context, a tree.Addr) ([]tree.Summary, error) {
			if a != "task:feat" {
				return nil, nil
			}
			var list []tree.Summary
			for _, id := range []string{"aaaa0001", "bbbb0002", "cccc0003"} {
				if status, ok := sessions[id]; ok {
					list = append(list, tree.Summary{Addr: tree.Addr("session:" + id), Kind: "session", Status: status})
				}
			}
			return list, nil
		})
	})
	e.OK("project:api", "create-task", map[string]any{"title": "Feat", "slug": "feat"})
	e.OK("task:feat", "create-subtask", map[string]any{"name": "sub"})
	e.OK("artifact:feat/plan.md", "write", map[string]any{"content": "# Plan\n\nUse a **token bucket**.\n"})
	return e, &log
}

func commentsOf(n tree.Node) []comments.Comment {
	list, _ := n.Fields["comments"].([]comments.Comment)
	return list
}

func TestArtifactAndDiffCommentsCRUD(t *testing.T) {
	e, _ := newEnv(t, nil)
	r := e.OK("artifact:feat/plan.md", "add-comment", map[string]any{"body": " Why a bucket? ", "quote": "token bucket", "line": 3})
	art := r.Addr
	if !strings.HasPrefix(string(art), "comment:") || len(art.Path()) != 8 {
		t.Fatalf("addr = %s", art)
	}
	diff := e.OK("task:feat/sub", "add-comment", map[string]any{"path": "./main.go", "line": 1, "side": "old", "body": "rename"}).Addr

	c := e.Get(art)
	want := map[string]any{"target": "artifact", "task": "task:feat", "artifact": "artifact:feat/plan.md", "path": "plan.md",
		"quote": "token bucket", "body": "Why a bucket?", "status": "open", "author": "user", "line": float64(3), "lineEnd": float64(3)}
	for k, v := range want {
		if c.Fields[k] != v {
			t.Errorf("%s = %v, want %v", k, c.Fields[k], v)
		}
	}
	if c.Title != "Why a bucket?" || !testkit.Has(c, "resolve") || !testkit.Has(c, "edit") || testkit.Has(c, "reopen") {
		t.Fatalf("comment node = %+v", c)
	}

	n, err := e.Tree.Get(t.Context(), tree.Actor{Kind: "user"}, "artifact:feat/plan.md", 1)
	if err != nil || len(commentsOf(n)) != 1 || len(n.Children) != 1 || n.Children[0].Addr != art || n.Children[0].Status != "open" {
		t.Fatalf("artifact = %+v, %v", n, err)
	}
	if got := commentsOf(e.Get("task:feat")); len(got) != 1 || got[0].Addr != art {
		t.Fatalf("root comments = %+v", got)
	}
	sub := commentsOf(e.Get("task:feat/sub"))
	if len(sub) != 2 || sub[0].Addr != art || sub[1].Addr != diff || sub[1].Path != "main.go" || sub[1].Side != "old" || sub[1].Task != "task:feat/sub" {
		t.Fatalf("sub-task comments = %+v", sub)
	}

	e.OK(art, "edit", map[string]any{"body": "Why not a window?"})
	if e.Get(art).Fields["body"] != "Why not a window?" {
		t.Fatal("edit did not change the body")
	}
	if r := e.Do("artifact:feat/plan.md", "add-comment", map[string]any{"body": "  "}); r.OK {
		t.Fatal("empty body accepted")
	}
	if r := e.Do("task:feat", "add-comment", map[string]any{"path": "../etc/passwd", "body": "x"}); r.OK {
		t.Fatal("path outside the worktree accepted")
	}
	if r := e.OK(diff, "delete", nil); r.Addr != "task:feat/sub" {
		t.Fatalf("delete result addr = %s", r.Addr)
	}
	if _, err := e.Tree.Get(t.Context(), tree.Actor{Kind: "user"}, diff, 0); err == nil {
		t.Fatal("deleted comment still readable")
	}
}

func TestResolveBySessionAndReopen(t *testing.T) {
	e, _ := newEnv(t, nil)
	c := e.OK("task:feat", "add-comment", map[string]any{"path": "main.go", "line": 1, "body": "add a doc comment"}).Addr
	agent := tree.Actor{Kind: "session", ID: "aaaa0001"}
	args, _ := json.Marshal(map[string]string{"reply": "added it"})
	if r := e.Tree.Do(t.Context(), agent, []tree.Op{{Target: c, Action: "resolve", Args: args}})[0]; !r.OK {
		t.Fatal(r.Message)
	}
	n := e.Get(c)
	if n.Fields["status"] != "resolved" || n.Fields["reply"] != "added it" || n.Fields["resolvedBy"] != "session:aaaa0001" {
		t.Fatalf("resolved comment = %+v", n.Fields)
	}
	if testkit.Has(n, "resolve") || testkit.Has(n, "edit") || !testkit.Has(n, "reopen") {
		t.Fatalf("actions after resolve = %+v", n.Actions)
	}
	e.OK(c, "reopen", nil)
	if n := e.Get(c); n.Fields["status"] != "open" || n.Fields["reply"] != nil {
		t.Fatalf("reopened comment = %+v", n.Fields)
	}
}

func TestSendFormatsAndMarksSent(t *testing.T) {
	e, log := newEnv(t, map[string]string{"aaaa0001": "idle", "bbbb0002": "running", "cccc0003": "done"})
	if testkit.Has(e.Get("task:feat"), "send-comments") {
		t.Fatal("send-comments offered with no open comments")
	}
	art := e.OK("artifact:feat/plan.md", "add-comment", map[string]any{"body": "Why a bucket?\nExplain.", "quote": "token bucket", "line": 3}).Addr
	diff := e.OK("task:feat", "add-comment", map[string]any{"path": "main.go", "line": 1, "lineEnd": 2, "body": "rename"}).Addr
	done := e.OK("task:feat", "add-comment", map[string]any{"path": "main.go", "body": "already done"}).Addr
	e.OK(done, "resolve", nil)

	r := e.OK("task:feat", "send-comments", nil)
	if r.Addr != "session:bbbb0002" || len(*log) != 1 || (*log)[0].session != "session:bbbb0002" {
		t.Fatalf("sent to %s, log %+v", r.Addr, *log)
	}
	text := (*log)[0].text
	artifacts := e.Get("task:feat").Fields["artifacts"].(string)
	for _, want := range []string{
		"I left 2 review comments",
		"1. " + string(art) + " on " + filepath.Join(artifacts, "plan.md") + " line 3\n   > token bucket\n   Why a bucket?\n   Explain.\n",
		"2. " + string(diff) + " on main.go lines 1-2 (diff)\n   rename\n",
		`"action":"resolve"`,
	} {
		if !strings.Contains(text, want) {
			t.Errorf("message lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, string(done)) {
		t.Error("resolved comment was sent")
	}
	for _, c := range []tree.Addr{art, diff} {
		if n := e.Get(c); n.Fields["status"] != "sent" || n.Fields["sentTo"] != "session:bbbb0002" || !testkit.Has(n, "resolve") {
			t.Errorf("%s after send = %+v", c, n.Fields)
		}
	}
	if testkit.Has(e.Get("task:feat"), "send-comments") {
		t.Fatal("send-comments still offered after sending everything")
	}

	e.OK("task:feat", "add-comment", map[string]any{"path": "main.go", "body": "one more"})
	e.OK("task:feat", "send-comments", map[string]any{"session": "aaaa0001"})
	if got := (*log)[1]; got.session != "session:aaaa0001" || !strings.Contains(got.text, "I left 1 review comment in layer8") {
		t.Fatalf("second send = %+v", got)
	}
}

func TestSendNeedsALiveSession(t *testing.T) {
	e, log := newEnv(t, map[string]string{"cccc0003": "done"})
	e.OK("task:feat", "add-comment", map[string]any{"path": "main.go", "body": "x"})
	if r := e.Do("task:feat", "send-comments", nil); r.OK || !strings.Contains(r.Message, "no live session") {
		t.Fatalf("send = %+v", r)
	}
	if len(*log) != 0 || commentsOf(e.Get("task:feat"))[0].Status != "open" {
		t.Fatal("comments changed without a send")
	}
}
