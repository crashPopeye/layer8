package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLookAndActOverMCP(t *testing.T) {
	h := newApp(t)
	a := h.app
	p, _ := a.AddProject("Notes")
	f, err := a.AddFolder(t.Context(), p.ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	task, err := a.CreateTask(t.Context(), NewTask{FolderID: f.ID, Title: "Write notes"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.Handler(Hooks{}))
	defer srv.Close()

	rpc := func(session, method, params string) (out struct {
		Result struct {
			IsError bool
			Content []struct{ Text string }
			Tools   []struct{ Name string }
		}
		Error *struct{ Message string }
	}) {
		req, _ := http.NewRequest("POST", srv.URL+"/api/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"`+method+`","params":`+params+`}`))
		req.Header.Set(sessionHeader, session)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if tools := rpc("", "tools/list", "{}").Result.Tools; len(tools) != 2 || tools[0].Name != "look" || tools[1].Name != "act" {
		t.Fatalf("tools %+v", tools)
	}
	app := rpc("", "tools/call", `{"name":"look","arguments":{}}`).Result.Content[0].Text
	if !strings.Contains(app, "task:"+task.Slug+"  Write notes") || !strings.Contains(app, "run(kind=research|plan|implement, prompt?)") || strings.Contains(app, "split") {
		t.Fatalf("look app:\n%s", app)
	}

	started := rpc("", "tools/call", `{"name":"act","arguments":{"ref":"task:`+task.Slug+`","verb":"chat","args":{"prompt":"hello"}}}`).Result
	if started.IsError || !strings.HasPrefix(started.Content[0].Text, "started session:") {
		t.Fatalf("act run: %+v", started)
	}
	sessions, _ := a.Store.Sessions()
	h.waitIdle(sessions[0].ID)

	// A session's own look starts at its task and lists the session under it.
	mine := rpc(sessions[0].ID, "tools/call", `{"name":"look","arguments":{}}`).Result.Content[0].Text
	if !strings.HasPrefix(mine, "task:"+task.Slug) || !strings.Contains(mine, "session:"+short(sessions[0].ID)) {
		t.Fatalf("look from session:\n%s", mine)
	}

	for _, c := range []struct{ args, want string }{
		{`{"ref":"task:` + task.Slug + `","verb":"split"}`, "has no verb"},
		{`{"ref":"task:` + task.Slug + `","verb":"run","args":{"kind":"deploy"}}`, "kind must be one of"},
		{`{"ref":"task:nope","verb":"run"}`, "no task matches"},
	} {
		r := rpc("", "tools/call", `{"name":"act","arguments":`+c.args+`}`).Result
		if !r.IsError || !strings.Contains(r.Content[0].Text, c.want) {
			t.Fatalf("%s: %+v", c.args, r)
		}
	}

	a.SetFocus(Focus{TaskID: task.ID})
	root, _ := a.Tree()
	if v := root.Fields[len(root.Fields)-1]; v.Key != "focus" || v.Value != "task:"+task.Slug {
		t.Fatalf("focus %+v", root.Fields)
	}
}
