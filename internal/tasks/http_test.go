package tasks_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/daemon"
	"github.com/crashPopeye/layer8/internal/store"
)

func TestDaemonEndToEnd(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	home := t.TempDir()
	ts := httptest.NewServer(daemon.Build(t.Context(), config.Config{Home: home}, db, "tok", "test"))
	t.Cleanup(ts.Close)
	repo := gitRepo(t, "api-gateway")

	post := func(path string, body any, header map[string]string) map[string]any {
		t.Helper()
		data, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", ts.URL+path, strings.NewReader(string(data)))
		req.Header.Set("Authorization", "Bearer tok")
		req.Header.Set("Content-Type", "application/json")
		for k, v := range header {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	get := func(path string) (int, string, string) {
		t.Helper()
		resp, err := http.Get(ts.URL + path + "&token=tok")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
	}

	post("/api/do", map[string]any{"ops": []any{map[string]any{"target": "app", "action": "add-project", "args": map[string]any{"path": repo}}}}, nil)

	mcp := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": "do", "arguments": map[string]any{"ops": []any{
		map[string]any{"target": "project:api-gateway", "action": "create-task", "args": map[string]any{"title": "Rate-limit uploads", "slug": "eng-482"}},
		map[string]any{"target": "artifact:eng-482/05-plan.md", "action": "write", "args": map[string]any{"content": "# Plan"}},
		map[string]any{"target": "artifact:eng-482/mock.html", "action": "write", "args": map[string]any{"content": "<p>hi</p>"}},
	}}}}
	res := post("/mcp", mcp, map[string]string{"X-Layer8-Session": "9c41e0aa", "Accept": "application/json, text/event-stream"})
	text := res["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if strings.Count(text, "ok ") != 3 || !strings.Contains(text, "ok task:eng-482: created task:eng-482 on branch eng-482 from trunk") {
		t.Fatalf("mcp do = %s", text)
	}
	wt := filepath.Join(home, "worktrees", "api-gateway", "eng-482")
	if git(t, wt, "rev-parse", "--abbrev-ref", "HEAD") != "eng-482" {
		t.Fatal("no worktree on the task branch")
	}
	var by string
	db.QueryRow(`SELECT created_by FROM tasks WHERE slug = 'eng-482'`).Scan(&by)
	if by != "session:9c41e0aa" {
		t.Fatalf("created_by = %q", by)
	}

	os.MkdirAll(filepath.Join(wt, "src"), 0o755)
	os.WriteFile(filepath.Join(wt, "src", "main.go"), []byte("package main"), 0o644)
	os.Symlink("/etc/hosts", filepath.Join(wt, "hosts"))

	cases := []struct {
		path, wantType, wantBody string
		wantCode                 int
	}{
		{"/api/artifact?addr=artifact:eng-482/05-plan.md", "text/markdown; charset=utf-8", "# Plan", 200},
		{"/api/artifact?addr=artifact:eng-482/mock.html", "text/html; charset=utf-8", "<p>hi</p>", 200},
		{"/api/artifact?addr=artifact:eng-482/../../token", "", "", 404},
		{"/api/artifact?addr=artifact:ghost/x.md", "", "", 404},
		{"/api/file?task=task:eng-482&path=src/main.go", "", "package main", 200},
		{"/api/file?task=task:eng-482&path=src", "application/json", `[{"name":"main.go","dir":false,"size":12}]`, 200},
		{"/api/file?task=task:eng-482&path=../../../token", "", "", 400},
		{"/api/file?task=task:eng-482&path=/etc/hosts", "", "", 400},
		{"/api/file?task=task:eng-482&path=hosts", "", "", 400},
		{"/api/file?task=task:ghost&path=x", "", "", 404},
	}
	for _, c := range cases {
		code, ctype, body := get(c.path)
		if code != c.wantCode || (c.wantType != "" && ctype != c.wantType) || (c.wantBody != "" && strings.TrimSpace(body) != c.wantBody) {
			t.Errorf("%s: %d %q %q", c.path, code, ctype, body)
		}
	}
}
