package server_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/server"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tree"
)

func newServer(t *testing.T) (*httptest.Server, *server.Hub) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tr := tree.New(db)
	tr.Register(tree.Kind{
		Name: "app",
		Get:  func(ctx context.Context, a tree.Addr) (tree.Node, error) { return tree.Node{Title: "layer8"}, nil },
		Actions: []tree.Action{{Name: "echo", Run: func(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
			return tree.Result{Message: by.String()}, nil
		}}},
	})
	hub := server.NewHub()
	mcp := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "mcp") })
	ts := httptest.NewServer(server.New(server.Options{Tree: tr, Hub: hub, Token: "s3cret", Version: "1.2.3", MCP: mcp}))
	t.Cleanup(ts.Close)
	return ts, hub
}

func TestAccessRules(t *testing.T) {
	ts, _ := newServer(t)
	cases := []struct {
		name, method, path string
		header             map[string]string
		host               string
		want               int
	}{
		{"version is public", "GET", "/api/version", nil, "", 200},
		{"node needs a token", "GET", "/api/node", nil, "", 401},
		{"wrong token", "GET", "/api/node", map[string]string{"Authorization": "Bearer nope"}, "", 401},
		{"bearer token", "GET", "/api/node", map[string]string{"Authorization": "Bearer s3cret"}, "", 200},
		{"query token", "GET", "/api/node?token=s3cret", nil, "", 200},
		{"mcp needs a token", "POST", "/mcp", nil, "", 401},
		{"mcp with token", "POST", "/mcp", map[string]string{"Authorization": "Bearer s3cret"}, "", 200},
		{"foreign host", "GET", "/api/version", nil, "evil.example:7979", 403},
		{"localhost host", "GET", "/api/version", nil, "localhost:7979", 200},
		{"foreign origin", "GET", "/api/version", map[string]string{"Origin": "https://evil.example"}, "", 403},
		{"tauri origin", "GET", "/api/version", map[string]string{"Origin": "tauri://localhost"}, "", 200},
		{"vite origin", "GET", "/api/version", map[string]string{"Origin": "http://localhost:5173"}, "", 200},
		{"own origin", "GET", "/api/version", map[string]string{"Origin": "http://localhost:7979"}, "localhost:7979", 200},
		{"preflight skips token", "OPTIONS", "/api/node", map[string]string{"Origin": "tauri://localhost"}, "", 204},
		{"unknown node", "GET", "/api/node?addr=ghost:x&token=s3cret", nil, "", 404},
		{"bad depth", "GET", "/api/node?depth=9&token=s3cret", nil, "", 400},
		{"session cannot pose over http", "POST", "/api/do?token=s3cret", map[string]string{"X-Layer8-Actor": "session:9c41e0aa"}, "", 400},
	}
	for _, c := range cases {
		req, _ := http.NewRequest(c.method, ts.URL+c.path, strings.NewReader(`{"ops":[]}`))
		for k, v := range c.header {
			req.Header.Set(k, v)
		}
		if c.host != "" {
			req.Host = c.host
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%s: status %d, want %d", c.name, resp.StatusCode, c.want)
		}
		if c.header["Origin"] != "" && c.want < 400 && resp.Header.Get("Access-Control-Allow-Origin") != c.header["Origin"] {
			t.Errorf("%s: missing CORS header", c.name)
		}
	}
}

func TestDoRecordsHTTPActor(t *testing.T) {
	ts, _ := newServer(t)
	for actor, want := range map[string]string{"": "user", "cli": "cli", "plugin:prs": "plugin:prs"} {
		req, _ := http.NewRequest("POST", ts.URL+"/api/do?token=s3cret", strings.NewReader(`{"ops":[{"target":"app","action":"echo"}]}`))
		req.Header.Set("X-Layer8-Actor", actor)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Results []tree.Result }
		json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if len(body.Results) != 1 || !body.Results[0].OK || body.Results[0].Message != want {
			t.Errorf("actor %q: results %+v", actor, body.Results)
		}
	}
}

func TestEventsStream(t *testing.T) {
	ts, hub := newServer(t)
	resp, err := http.Get(ts.URL + "/api/events?token=s3cret")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	r := bufio.NewReader(resp.Body)
	r.ReadString('\n')
	r.ReadString('\n')
	hub.Publish("node", server.NodeChange{Addrs: []tree.Addr{"project:api"}})
	hub.Publish("toast", map[string]string{"level": "info", "text": "hi"})
	var got []string
	for len(got) < 4 {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line != "\n" {
			got = append(got, strings.TrimSpace(line))
		}
	}
	want := []string{"event: node", `data: {"addrs":["project:api"]}`, "event: toast", `data: {"level":"info","text":"hi"}`}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q", got)
	}
}
