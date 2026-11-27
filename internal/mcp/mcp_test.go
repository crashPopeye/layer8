package mcp_test

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/daemon"
	"github.com/crashPopeye/layer8/internal/store"
)

type rpc struct {
	t       *testing.T
	url     string
	session string
	header  map[string]string
}

func (c *rpc) call(method string, params any) (result map[string]any, rpcErr map[string]any, resp *http.Response) {
	c.t.Helper()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	req, _ := http.NewRequest("POST", c.url, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.session != "" {
		req.Header.Set("Mcp-Session-Id", c.session)
	}
	for k, v := range c.header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Result map[string]any
		Error  map[string]any
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Result, out.Error, resp
}

func setup(t *testing.T) (*rpc, *sql.DB, string) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ts := httptest.NewServer(daemon.Build(t.Context(), config.Config{Home: t.TempDir()}, db, "tok", "test"))
	t.Cleanup(ts.Close)
	repo := filepath.Join(t.TempDir(), "api-gateway")
	if out, err := exec.Command("git", "init", "-q", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	return &rpc{t: t, url: ts.URL + "/mcp"}, db, repo
}

func TestMCPReadAndDo(t *testing.T) {
	c, db, repo := setup(t)

	init, _, resp := c.call("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}})
	if init["protocolVersion"] != "2025-06-18" {
		t.Fatalf("initialize = %v", init)
	}
	c.session = resp.Header.Get("Mcp-Session-Id")
	if c.session == "" {
		t.Fatal("no Mcp-Session-Id")
	}
	c.header = map[string]string{"X-Layer8-Session": "9c41e0aa"}

	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	req, _ := http.NewRequest("POST", c.url, strings.NewReader(string(body)))
	req.Header.Set("Authorization", "Bearer tok")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusAccepted {
		t.Fatalf("notification: %v %v", r.StatusCode, err)
	}

	if res, _, _ := c.call("ping", nil); res == nil {
		t.Fatal("ping returned no result")
	}
	tools, _, _ := c.call("tools/list", nil)
	if list := tools["tools"].([]any); len(list) != 1 || list[0].(map[string]any)["name"] != "do" {
		t.Fatalf("tools = %v", tools)
	}

	read, _, _ := c.call("resources/read", map[string]any{"uri": "layer8://app"})
	contents := read["contents"].([]any)
	var app struct {
		Addr    string
		Actions []struct{ Name string }
	}
	json.Unmarshal([]byte(contents[0].(map[string]any)["text"].(string)), &app)
	if app.Addr != "app" || len(app.Actions) == 0 || app.Actions[0].Name != "add-project" {
		t.Fatalf("app = %+v", app)
	}
	if text := contents[1].(map[string]any)["text"].(string); !strings.Contains(text, "add-project {path, name?}") {
		t.Fatalf("text = %s", text)
	}

	streamReq, _ := http.NewRequest("GET", c.url, nil)
	streamReq.Header.Set("Authorization", "Bearer tok")
	streamReq.Header.Set("Mcp-Session-Id", c.session)
	stream, err := http.DefaultClient.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if _, e, _ := c.call("resources/subscribe", map[string]any{"uri": "layer8://app"}); e != nil {
		t.Fatalf("subscribe: %v", e)
	}

	call, _, _ := c.call("tools/call", map[string]any{"name": "do", "arguments": map[string]any{"ops": []any{
		map[string]any{"target": "app", "action": "add-project", "args": map[string]any{"path": repo}},
		map[string]any{"target": "app", "action": "add-project", "args": map[string]any{"path": "nope"}},
	}}})
	text := call["content"].([]any)[0].(map[string]any)["text"].(string)
	if call["isError"] != true || !strings.Contains(text, "ok project:api-gateway: added project api-gateway") || !strings.Contains(text, "error app: path must be absolute") {
		t.Fatalf("tools/call = %v", call)
	}

	line := ""
	for sc := bufio.NewScanner(stream.Body); sc.Scan(); {
		if line = sc.Text(); strings.HasPrefix(line, "data: ") {
			break
		}
	}
	if !strings.Contains(line, `"method":"notifications/resources/updated"`) || !strings.Contains(line, `"uri":"layer8://app"`) {
		t.Fatalf("notification = %q", line)
	}

	list, _, _ := c.call("resources/list", nil)
	uris := ""
	for _, r := range list["resources"].([]any) {
		uris += r.(map[string]any)["uri"].(string) + " "
	}
	if uris != "layer8://app layer8://here layer8://project/api-gateway layer8://workflow/feature layer8://workflow/pr-care layer8://workflow/product-feature layer8://workflow/quick-fix layer8://ui/layout layer8://ui/theme layer8://ui/plugins " {
		t.Fatalf("resources = %q", uris)
	}

	templates, _, _ := c.call("resources/templates/list", nil)
	if s, _ := json.Marshal(templates); !strings.Contains(string(s), "layer8://project/{path}") {
		t.Fatalf("templates = %s", s)
	}

	project, _, _ := c.call("resources/read", map[string]any{"uri": "layer8://project/api-gateway"})
	if s, _ := json.Marshal(project); !strings.Contains(string(s), `rename {name}`) {
		t.Fatalf("project = %s", s)
	}

	if _, e, _ := c.call("resources/read", map[string]any{"uri": "layer8://project/ghost"}); e == nil || e["code"] != float64(-32002) {
		t.Fatalf("missing resource error = %v", e)
	}
	if _, e, _ := c.call("resources/read", map[string]any{"uri": "layer8://here"}); e == nil {
		t.Fatal("here without a session hook should fail")
	}
	if _, e, _ := c.call("nope/nope", nil); e == nil || e["code"] != float64(-32601) {
		t.Fatalf("unknown method error = %v", e)
	}

	var actor string
	db.QueryRow(`SELECT actor_kind || ':' || actor_id FROM actions_log WHERE ok = 1`).Scan(&actor)
	if actor != "session:9c41e0aa" {
		t.Fatalf("logged actor = %q", actor)
	}
}
