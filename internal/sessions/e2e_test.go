package sessions_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/harness/claude"
	"github.com/crashPopeye/layer8/internal/harness/claude/claudetest"
	"github.com/crashPopeye/layer8/internal/mcp"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/server"
	"github.com/crashPopeye/layer8/internal/sessions"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

func TestMain(m *testing.M) {
	claudetest.Main()
	os.Exit(m.Run())
}

type client struct {
	t   *testing.T
	url string
}

func (c client) send(method, path, actor string, body any) []byte {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.url+path, r)
	req.Header.Set("Authorization", "Bearer tok")
	if actor != "" {
		req.Header.Set("X-Layer8-Actor", actor)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		c.t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, out)
	}
	return out
}

func (c client) do(actor string, target tree.Addr, action string, args any) tree.Result {
	c.t.Helper()
	raw, _ := json.Marshal(args)
	var out struct{ Results []tree.Result }
	json.Unmarshal(c.send(http.MethodPost, "/api/do", actor, map[string]any{"ops": []tree.Op{{Target: target, Action: action, Args: raw}}}), &out)
	return out.Results[0]
}

func (c client) node(addr tree.Addr) tree.Node {
	c.t.Helper()
	var n tree.Node
	json.Unmarshal(c.send(http.MethodGet, "/api/node?addr="+string(addr), "", nil), &n)
	return n
}

func (c client) waitStatus(addr tree.Addr, want string) tree.Node {
	c.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		n := c.node(addr)
		if n.Fields["status"] == want {
			return n
		}
		if time.Now().After(deadline) {
			c.t.Fatalf("%s status = %v, want %s", addr, n.Fields["status"], want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (c client) events(s tree.Addr) []sessions.Event {
	c.t.Helper()
	var out struct{ Events []sessions.Event }
	json.Unmarshal(c.send(http.MethodGet, "/api/sessions/"+s.Path()+"/events?after=0&limit=100", "", nil), &out)
	return out.Events
}

func TestEndToEndWithFakeClaude(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tr := tree.New(db)
	hub := server.NewHub()
	tr.OnChange = func(addrs []tree.Addr) { hub.Publish("node", server.NodeChange{Addrs: addrs}) }
	projects.Register(tr, db, "test")
	ts := tasks.Register(tr, db, tasks.Options{Home: t.TempDir()})
	srv := httptest.NewUnstartedServer(nil)
	url := "http://" + srv.Listener.Addr().String()
	log := filepath.Join(t.TempDir(), "fake.log")
	ss := sessions.Register(tr, db, ts, sessions.Options{
		Harnesses: map[string]harness.Harness{"claude": claude.Harness{Bin: os.Args[0], Env: append(os.Environ(), claudetest.LogEnv+"="+log)}},
		MCPURL:    url + "/mcp", Token: "tok", Publish: hub.Publish, Grace: time.Second,
	})
	srv.Config.Handler = server.New(server.Options{Tree: tr, Hub: hub, Token: "tok", Version: "test", MCP: mcp.New(tr, hub, "test"),
		Routes: map[string]http.Handler{
			"GET /api/sessions/{id8}/events": http.HandlerFunc(ss.ServeEvents),
			"GET /api/capabilities":          http.HandlerFunc(ss.ServeCapabilities),
		}})
	srv.Start()
	defer srv.Close()
	c := client{t, url}

	var mu sync.Mutex
	var stream strings.Builder
	resp, err := http.Get(url + "/api/events?token=tok")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<20)
		for sc.Scan() {
			mu.Lock()
			stream.WriteString(sc.Text() + "\n")
			mu.Unlock()
		}
	}()

	repo := gitRepo(t)
	c.do("", "app", "add-project", map[string]string{"path": repo})
	task := c.do("", "project:api-gateway", "create-task", map[string]string{"title": "Eng 482"}).Addr

	s := c.do("", task, "start-session", map[string]string{"prompt": "hello"}).Addr
	n := c.waitStatus(s, "idle")
	if caps, _ := json.Marshal(n.Fields["capabilities"]); !strings.Contains(string(caps), `"mcpServers":[{"name":"layer8"`) || n.Fields["harnessSessionId"] != "s1" {
		t.Fatalf("session after hello = %s %+v", caps, n.Fields)
	}
	var types []string
	for _, e := range c.events(s) {
		types = append(types, string(e.Type))
	}
	if !slices.Equal(types, []string{"user", "system", "text", "result"}) {
		t.Fatalf("stored events = %v", types)
	}

	c.do("", s, "say", map[string]string{"text": "write"})
	c.waitStatus(s, "waiting")
	if r := c.do("plugin:evil", s, "allow", map[string]any{}); r.OK {
		t.Fatal("a plugin answered an approval")
	}
	if r := c.do("", s, "allow", map[string]any{}); !r.OK {
		t.Fatalf("allow = %+v", r)
	}
	c.waitStatus(s, "idle")

	c.do("", s, "say", map[string]string{"text": `mcp [{"target":"here","action":"start-session","args":{"title":"Implement"}}]`})
	deadline := time.Now().Add(5 * time.Second)
	var child tree.Summary
	for child.Addr == "" && time.Now().Before(deadline) {
		for _, k := range c.node(task).Children {
			if k.Title == "Implement" {
				child = k
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if child.Addr == "" {
		t.Fatalf("the session did not start a sibling through MCP; events %+v", c.events(s))
	}
	if by := c.node(child.Addr).Fields["createdBy"]; by != "session:"+s.Path() {
		t.Fatalf("child createdBy = %v", by)
	}
	c.waitStatus(s, "idle")
	last := c.events(s)
	if reply, _ := json.Marshal(last[len(last)-2].Data); !strings.Contains(string(reply), "started "+string(child.Addr)) {
		t.Fatalf("mcp reply = %s", reply)
	}

	var caps harness.Capabilities
	json.Unmarshal(c.send(http.MethodGet, "/api/capabilities?task="+string(task), "", nil), &caps)
	if len(caps.Commands) == 0 || caps.Commands[0].Name != "review" {
		t.Fatalf("capabilities = %+v", caps)
	}

	c.do("", s, "close", nil)
	c.waitStatus(s, "done")

	argvLine, _, _ := strings.Cut(string(must(os.ReadFile(log))), "\n")
	var argv []string
	json.Unmarshal([]byte(argvLine), &argv)
	if i := slices.Index(argv, "--allowedTools"); i < 0 || argv[i+1] != "ListMcpResourcesTool,ReadMcpResourceTool" ||
		!strings.Contains(strings.Join(argv, " "), `"X-Layer8-Session":"`+s.Path()+`"`) {
		t.Fatalf("argv = %q", argv)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := stream.String(); !strings.Contains(got, "event: session") || !strings.Contains(got, `"partial":true`) || !strings.Contains(got, `"text":"Hel"`) {
		t.Fatalf("sse stream = %s", got)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
