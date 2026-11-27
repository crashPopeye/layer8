package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
)

const fakeEnv = "LAYER8_FAKE_CODEX_LOG"

func TestMain(m *testing.M) {
	if log := os.Getenv(fakeEnv); log != "" {
		fakeCodex(log)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeCodex speaks a scripted subset of the app-server protocol, appending its argv
// and every stdin line to the log file.
func fakeCodex(logPath string) {
	logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	argv, _ := json.Marshal(os.Args[1:])
	fmt.Fprintf(logf, "%s\n", argv)
	out := json.NewEncoder(os.Stdout)
	notify := func(method string, params any) { out.Encode(map[string]any{"method": method, "params": params}) }
	turnDone := func(id, status string) {
		notify("turn/completed", map[string]any{"threadId": "th-1", "turn": map[string]any{"id": id, "status": status, "durationMs": 900, "items": []any{}}})
	}
	active := false
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 1<<20)
	for in.Scan() {
		fmt.Fprintf(logf, "%s\n", in.Bytes())
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				ThreadID string `json:"threadId"`
				Cursor   string `json:"cursor"`
				Input    []struct {
					Text string `json:"text"`
				} `json:"input"`
			} `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		json.Unmarshal(in.Bytes(), &m)
		ok := func(result any) { out.Encode(map[string]any{"id": m.ID, "result": result}) }
		switch m.Method {
		case "initialize":
			if os.Getenv("FAKE_INIT_CRASH") != "" {
				fmt.Fprintln(os.Stderr, "not logged in")
				os.Exit(1)
			}
			ok(map[string]any{"userAgent": "fake"})
		case "skills/extraRoots/set":
			ok(map[string]any{})
		case "skills/list":
			ok(map[string]any{"data": []any{map[string]any{"cwd": "/w", "errors": []any{}, "skills": []any{
				map[string]any{"name": "review", "description": "Review the diff in depth", "path": "/s/review/SKILL.md", "scope": "user", "enabled": true, "interface": map[string]any{"shortDescription": "Review code"}},
				map[string]any{"name": "layer8:plan", "description": "Plan the work", "shortDescription": "Plan", "path": "/s/plan/SKILL.md", "scope": "user", "enabled": true},
				map[string]any{"name": "off", "description": "Disabled", "path": "/s/off/SKILL.md", "scope": "user", "enabled": false},
			}}}})
		case "thread/start":
			ok(map[string]any{"thread": map[string]any{"id": "th-1"}, "model": "gpt-5"})
		case "thread/resume":
			ok(map[string]any{"thread": map[string]any{"id": m.Params.ThreadID}, "model": "gpt-5"})
		case "thread/fork":
			ok(map[string]any{"thread": map[string]any{"id": "th-fork"}, "model": "gpt-5"})
		case "model/list":
			if m.Params.Cursor == "" {
				ok(map[string]any{"data": []any{map[string]any{"id": "gpt-5", "model": "gpt-5", "displayName": "GPT-5", "description": "Smart", "hidden": false, "isDefault": true,
					"defaultReasoningEffort": "medium", "supportedReasoningEfforts": []any{map[string]any{"reasoningEffort": "low", "description": "Fast"}, map[string]any{"reasoningEffort": "high", "description": "Deep"}}}},
					"nextCursor": "p2"})
			} else {
				ok(map[string]any{"data": []any{map[string]any{"id": "gpt-5-mini", "model": "gpt-5-mini", "displayName": "GPT-5 mini", "description": "Cheap", "supportedReasoningEfforts": []any{}}}, "nextCursor": nil})
			}
		case "mcpServerStatus/list":
			ok(map[string]any{"data": []any{
				map[string]any{"name": "layer8", "authStatus": "bearerToken", "tools": map[string]any{}},
				map[string]any{"name": "linear", "authStatus": "notLoggedIn", "tools": map[string]any{}},
			}, "nextCursor": nil})
		case "turn/interrupt":
			ok(map[string]any{})
			turnDone("t9", "interrupted")
			active = false
		case "turn/start":
			if active {
				out.Encode(map[string]any{"id": m.ID, "error": map[string]any{"code": -32600, "message": "a turn is already running"}})
				continue
			}
			text := m.Params.Input[0].Text
			if text == "fail" {
				out.Encode(map[string]any{"id": m.ID, "error": map[string]any{"code": -32600, "message": "thread not loaded"}})
				continue
			}
			ok(map[string]any{"turn": map[string]any{"id": "t1", "status": "inProgress", "items": []any{}}})
			switch {
			case text == "hello":
				notify("turn/started", map[string]any{"threadId": "th-1", "turn": map[string]any{"id": "t1"}})
				notify("item/reasoning/summaryTextDelta", map[string]any{"itemId": "r1", "delta": "Thin"})
				notify("item/completed", map[string]any{"item": map[string]any{"type": "reasoning", "id": "r1", "summary": []string{"Thinking"}, "content": []string{}}})
				notify("item/completed", map[string]any{"item": map[string]any{"type": "reasoning", "id": "r2", "summary": []string{}, "content": []string{}}})
				notify("item/completed", map[string]any{"item": map[string]any{"type": "userMessage", "id": "u1", "content": []any{map[string]any{"type": "text", "text": "hello"}}}})
				for _, d := range []string{"Hel", "lo"} {
					notify("item/agentMessage/delta", map[string]any{"itemId": "a1", "delta": d})
				}
				notify("item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "a1", "text": "Hello"}})
				notify("thread/tokenUsage/updated", map[string]any{"threadId": "th-1", "turnId": "t1", "tokenUsage": map[string]any{
					"total": map[string]int{"totalTokens": 5000}, "last": map[string]int{"inputTokens": 1200, "cachedInputTokens": 1000, "outputTokens": 30, "totalTokens": 1230}, "modelContextWindow": 272000}})
				notify("error", map[string]any{"willRetry": true, "error": map[string]any{"message": "reconnecting"}})
				turnDone("t1", "completed")
			case text == "tools":
				active = true
				notify("turn/started", map[string]any{"turn": map[string]any{"id": "t1"}})
				notify("item/started", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "cwd": "/w", "status": "inProgress", "commandActions": []any{}}})
				out.Encode(map[string]any{"id": 100, "method": "item/commandExecution/requestApproval", "params": map[string]any{"itemId": "c1", "threadId": "th-1", "turnId": "t1", "command": "ls", "cwd": "/w", "reason": "outside the sandbox"}})
			case text == "elicit":
				active = true
				out.Encode(map[string]any{"id": "e-1", "method": "mcpServer/elicitation/request", "params": map[string]any{"serverName": "layer8", "threadId": "th-1", "mode": "form", "message": "Allow the do tool?", "requestedSchema": map[string]any{"type": "object", "properties": map[string]any{}}}})
			case text == "ask":
				active = true
				out.Encode(map[string]any{"id": 103, "method": "item/tool/requestUserInput", "params": map[string]any{"itemId": "x"}})
			case text == "slow":
				active = true
				notify("turn/started", map[string]any{"turn": map[string]any{"id": "t9"}})
			case text == "crash":
				fmt.Fprintln(os.Stderr, "fatal: out of cheese")
				os.Exit(2)
			default:
				active = true
				time.Sleep(100 * time.Millisecond)
				notify("item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "a", "text": strings.TrimPrefix(text, "echo ")}})
				turnDone("t1", "completed")
				active = false
			}
		case "":
			switch string(m.ID) {
			case "100":
				notify("item/completed", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "cwd": "/w", "status": "completed", "exitCode": 0, "aggregatedOutput": "a.txt\n", "commandActions": []any{}}})
				notify("item/started", map[string]any{"item": map[string]any{"type": "fileChange", "id": "f1", "status": "inProgress", "changes": []any{
					map[string]any{"path": "/w/a.txt", "kind": map[string]any{"type": "update", "move_path": nil}, "diff": "-a\n+b"},
					map[string]any{"path": "/w/b.txt", "kind": map[string]any{"type": "add"}, "diff": "b"}}}})
				out.Encode(map[string]any{"id": 101, "method": "item/fileChange/requestApproval", "params": map[string]any{"itemId": "f1", "threadId": "th-1", "turnId": "t1", "reason": "write files"}})
			case "101":
				notify("item/completed", map[string]any{"item": map[string]any{"type": "fileChange", "id": "f1", "status": "declined", "changes": []any{}}})
				notify("item/started", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "m1", "server": "layer8", "tool": "do", "status": "inProgress", "arguments": map[string]any{"ops": []any{}}}})
				notify("item/completed", map[string]any{"item": map[string]any{"type": "mcpToolCall", "id": "m1", "server": "layer8", "tool": "do", "status": "completed", "arguments": map[string]any{"ops": []any{}},
					"result": map[string]any{"content": []any{map[string]any{"type": "text", "text": "ok"}}}}})
				turnDone("t1", "completed")
				active = false
			case `"e-1"`, "103":
				turnDone("t1", "completed")
				active = false
			}
		}
	}
}

type recorder struct {
	events chan harness.Event
	perms  chan harness.PermissionRequest
	decide func(harness.PermissionRequest) harness.Decision
	exit   chan error
}

func newRecorder(decide func(harness.PermissionRequest) harness.Decision) *recorder {
	return &recorder{make(chan harness.Event, 100), make(chan harness.PermissionRequest, 10), decide, make(chan error, 1)}
}

func (r *recorder) OnEvent(e harness.Event) { r.events <- e }

func (r *recorder) OnPermission(req harness.PermissionRequest, respond func(harness.Decision)) {
	r.perms <- req
	go respond(r.decide(req))
}

func (r *recorder) OnExit(err error) { r.exit <- err }

func (r *recorder) until(t *testing.T, typ harness.EventType, timeout time.Duration) []harness.Event {
	t.Helper()
	var got []harness.Event
	for {
		select {
		case e := <-r.events:
			got = append(got, e)
			if e.Type == typ {
				return got
			}
		case <-time.After(timeout):
			t.Fatalf("no %s event, got %+v", typ, got)
		}
	}
}

func fake(t *testing.T, extraEnv ...string) (Harness, string) {
	t.Helper()
	log := filepath.Join(t.TempDir(), "fake.log")
	return Harness{Bin: os.Args[0], Env: append(os.Environ(), append(extraEnv, fakeEnv+"="+log)...), PluginDirs: []string{"/opt/layer8/plugin"}}, log
}

func logLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// sent returns the params of every request with method the fake received.
func sent(t *testing.T, path, method string) []string {
	t.Helper()
	var out []string
	for _, l := range logLines(t, path)[1:] {
		var m struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		json.Unmarshal([]byte(l), &m)
		if m.Method == method {
			out = append(out, string(m.Params))
		}
	}
	return out
}

func start(t *testing.T, h Harness, rec *recorder, o harness.StartOptions) harness.Proc {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if o.Cwd == "" {
		o.Cwd = t.TempDir()
	}
	p, err := h.Start(ctx, o, rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close(time.Second) })
	return p
}

func TestStartHandshakeAndThreadParams(t *testing.T) {
	h, log := fake(t)
	rec := newRecorder(nil)
	cwd := t.TempDir()
	p := start(t, h, rec, harness.StartOptions{
		Cwd: cwd, AddDirs: []string{"/home/a/.layer8/tasks/eng-482"}, Model: "gpt-5", Effort: "high", PermissionMode: "acceptEdits",
		MCPURL: "http://127.0.0.1:7979/mcp", MCPHeaders: map[string]string{"Authorization": "Bearer tok", "X-Layer8-Session": "9c41e0aa"},
		PluginDirs: []string{"/tmp/extra"}, AppendSystemPrompt: "You run inside layer8.",
	})
	init := rec.until(t, harness.EventSystem, 5*time.Second)[0].Data
	if init["subtype"] != "init" || init["session_id"] != "th-1" || init["model"] != "gpt-5" || !slices.Equal(init["skills"].([]string), []string{"review", "layer8:plan"}) {
		t.Fatalf("init = %v", init)
	}
	p.Close(time.Second)

	lines := logLines(t, log)
	if lines[0] != `["app-server"]` {
		t.Fatalf("argv = %s", lines[0])
	}
	want := []string{
		`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"layer8","title":"layer8","version":"1"}}}`,
		`{"method":"initialized"}`,
		`{"id":2,"method":"skills/extraRoots/set","params":{"extraRoots":["/opt/layer8/plugin/skills","/tmp/extra/skills"]}}`,
		`{"id":3,"method":"skills/list","params":{"cwds":["` + cwd + `"]}}`,
		`{"id":4,"method":"thread/start","params":{"approvalPolicy":"on-request","config":{"mcp_servers":{"layer8":{"http_headers":{"Authorization":"Bearer tok","X-Layer8-Session":"9c41e0aa"},"url":"http://127.0.0.1:7979/mcp"}},"sandbox_workspace_write":{"writable_roots":["/home/a/.layer8/tasks/eng-482"]}},"cwd":"` + cwd + `","developerInstructions":"You run inside layer8.","model":"gpt-5","sandbox":"workspace-write"}}`,
	}
	if !slices.Equal(lines[1:], want) {
		t.Fatalf("requests\n got %q\nwant %q", lines[1:], want)
	}
	if err := <-rec.exit; err != nil {
		t.Fatalf("exit after Close = %v, want nil", err)
	}
}

func TestPermissionModes(t *testing.T) {
	for mode, want := range map[string]string{
		"":                  `"approvalPolicy":"untrusted","config":{},"cwd":"/w","sandbox":"workspace-write"`,
		"default":           `"approvalPolicy":"untrusted","config":{},"cwd":"/w","sandbox":"workspace-write"`,
		"plan":              `"approvalPolicy":"untrusted","config":{},"cwd":"/w","sandbox":"read-only"`,
		"acceptEdits":       `"approvalPolicy":"on-request","config":{},"cwd":"/w","sandbox":"workspace-write"`,
		"auto":              `"approvalPolicy":"on-request","config":{},"cwd":"/w","sandbox":"workspace-write"`,
		"bypassPermissions": `"approvalPolicy":"never","config":{},"cwd":"/w","sandbox":"danger-full-access"`,
		"unknown":           `"approvalPolicy":"untrusted","config":{},"cwd":"/w","sandbox":"workspace-write"`,
	} {
		method, params := threadParams(harness.StartOptions{Cwd: "/w", PermissionMode: mode})
		got, _ := json.Marshal(params)
		if method != "thread/start" || string(got) != "{"+want+"}" {
			t.Errorf("%q: %s %s, want {%s}", mode, method, got, want)
		}
	}
}

func TestResume(t *testing.T) {
	h, log := fake(t)
	rec := newRecorder(nil)
	cwd := t.TempDir()
	start(t, h, rec, harness.StartOptions{Cwd: cwd, ResumeID: "th-old"})
	if init := rec.until(t, harness.EventSystem, 5*time.Second)[0].Data; init["session_id"] != "th-old" {
		t.Fatalf("init = %v", init)
	}
	got := sent(t, log, "thread/resume")
	if want := `{"approvalPolicy":"untrusted","config":{},"cwd":"` + cwd + `","sandbox":"workspace-write","threadId":"th-old"}`; len(got) != 1 || got[0] != want {
		t.Fatalf("thread/resume = %q, want %s", got, want)
	}
}

func TestFork(t *testing.T) {
	h, log := fake(t)
	rec := newRecorder(nil)
	start(t, h, rec, harness.StartOptions{ResumeID: "th-old", Fork: true})
	if init := rec.until(t, harness.EventSystem, 5*time.Second)[0].Data; init["session_id"] != "th-fork" {
		t.Fatalf("init = %v", init)
	}
	if got := sent(t, log, "thread/fork"); len(got) != 1 || !strings.Contains(got[0], `"threadId":"th-old"`) {
		t.Fatalf("thread/fork = %q", got)
	}
}

func TestSendInput(t *testing.T) {
	h, log := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{Effort: "low"})
	for _, msg := range []harness.UserMessage{
		{Text: "echo look", Files: []string{"/w/a.go", "/w/b.md"}, Images: []harness.Image{{MediaType: "image/png", Data: []byte("png")}}},
		{Text: "/review the auth change"},
		{Text: "$layer8:plan\nsplit it"},
		{Text: "/unknown stays text"},
	} {
		if err := p.Send(msg); err != nil {
			t.Fatal(err)
		}
	}
	for range 4 {
		rec.until(t, harness.EventResult, 5*time.Second)
	}
	want := []string{
		`{"effort":"low","input":[{"text":"echo look\n\nAttached files:\n- /w/a.go\n- /w/b.md","type":"text"},{"type":"image","url":"data:image/png;base64,cG5n"}],"threadId":"th-1"}`,
		`{"effort":"low","input":[{"text":"$review the auth change","type":"text"},{"name":"review","path":"/s/review/SKILL.md","type":"skill"}],"threadId":"th-1"}`,
		`{"effort":"low","input":[{"text":"$layer8:plan\nsplit it","type":"text"},{"name":"layer8:plan","path":"/s/plan/SKILL.md","type":"skill"}],"threadId":"th-1"}`,
		`{"effort":"low","input":[{"text":"/unknown stays text","type":"text"}],"threadId":"th-1"}`,
	}
	if got := sent(t, log, "turn/start"); !slices.Equal(got, want) {
		t.Fatalf("turn/start\n got %q\nwant %q", got, want)
	}
}

func summary(events []harness.Event) []string {
	var out []string
	for _, e := range events {
		s := string(e.Type)
		if e.Partial {
			s += "~"
		}
		switch e.Type {
		case harness.EventText, harness.EventThinking:
			s += ":" + e.Data["text"].(string)
		case harness.EventSystem:
			s += ":" + e.Data["subtype"].(string)
		case harness.EventToolUse:
			s += ":" + e.Data["name"].(string)
		case harness.EventToolResult:
			s += fmt.Sprintf(":%v:%v", e.Data["toolUseId"], e.Data["isError"])
		}
		out = append(out, s)
	}
	return out
}

func TestTurnEvents(t *testing.T) {
	h, _ := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{})
	rec.until(t, harness.EventSystem, 5*time.Second)
	p.Send(harness.UserMessage{Text: "hello"})
	got := rec.until(t, harness.EventResult, 5*time.Second)
	want := []string{"thinking~:Thin", "thinking:Thinking", "text~:Hel", "text~:lo", "text:Hello", "result"}
	if s := summary(got); !slices.Equal(s, want) {
		t.Fatalf("events\n got %v\nwant %v", s, want)
	}
	res, _ := json.Marshal(got[len(got)-1].Data)
	if want := `{"contextTokens":1230,"contextWindow":272000,"costUsd":0,"durationMs":900,"isError":false,"result":"Hello","sessionId":"th-1","subtype":"success",` +
		`"usage":{"total":{"totalTokens":5000},"last":{"inputTokens":1200,"cachedInputTokens":1000,"outputTokens":30,"totalTokens":1230},"modelContextWindow":272000}}`; string(res) != want {
		t.Fatalf("result\n got %s\nwant %s", res, want)
	}
}

func TestToolsAndApprovals(t *testing.T) {
	h, log := fake(t)
	decisions := map[string]harness.Decision{"Bash": {Allow: true, Always: true}, "Edit": {Message: "no"}}
	rec := newRecorder(func(r harness.PermissionRequest) harness.Decision { return decisions[r.Tool] })
	p := start(t, h, rec, harness.StartOptions{})
	rec.until(t, harness.EventSystem, 5*time.Second)
	p.Send(harness.UserMessage{Text: "tools"})
	got := rec.until(t, harness.EventResult, 5*time.Second)
	want := []string{"tool_use:Bash", "tool_result:c1:false", "tool_use:Edit", "tool_result:f1:true", "tool_use:mcp__layer8__do", "tool_result:m1:false", "result"}
	if s := summary(got); !slices.Equal(s, want) {
		t.Fatalf("events\n got %v\nwant %v", s, want)
	}
	if in, _ := json.Marshal(got[0].Data["input"]); string(in) != `{"command":"ls","cwd":"/w"}` || got[1].Data["content"] != "a.txt\n" {
		t.Fatalf("command events = %v %v", got[0].Data, got[1].Data)
	}
	if in, _ := json.Marshal(got[4].Data["input"]); string(in) != `{"ops":[]}` {
		t.Fatalf("mcp input = %s", in)
	}
	if c, _ := json.Marshal(got[5].Data["content"]); string(c) != `[{"text":"ok","type":"text"}]` {
		t.Fatalf("mcp result = %s", c)
	}

	bash, edit := <-rec.perms, <-rec.perms
	if bash.ID != "100" || bash.ToolUseID != "c1" || string(bash.Input) != `{"command":"ls","cwd":"/w"}` || bash.Description != "outside the sandbox" {
		t.Fatalf("command approval = %+v", bash)
	}
	wantEdit := `{"changes":[{"diff":"-a\n+b","kind":"update","path":"/w/a.txt"},{"diff":"b","kind":"add","path":"/w/b.txt"}],"file_path":"/w/a.txt"}`
	if edit.ID != "101" || edit.Tool != "Edit" || edit.ToolUseID != "f1" || string(edit.Input) != wantEdit {
		t.Fatalf("file approval = %+v %s", edit, edit.Input)
	}
	p.Close(time.Second)
	var replies []string
	for _, l := range logLines(t, log) {
		if strings.HasPrefix(l, `{"id":10`) && strings.Contains(l, `"result"`) {
			replies = append(replies, l)
		}
	}
	if want := []string{`{"id":100,"result":{"decision":"acceptForSession"}}`, `{"id":101,"result":{"decision":"decline"}}`}; !slices.Equal(replies, want) {
		t.Fatalf("replies\n got %q\nwant %q", replies, want)
	}
}

func TestApprovalDecisions(t *testing.T) {
	for _, tc := range []struct {
		d    harness.Decision
		want string
	}{
		{harness.Decision{Allow: true}, `{"decision":"accept"}`},
		{harness.Decision{Allow: true, Always: true}, `{"decision":"acceptForSession"}`},
		{harness.Decision{Message: "no"}, `{"decision":"decline"}`},
	} {
		if got, _ := json.Marshal(approvalAnswer(tc.d)); string(got) != tc.want {
			t.Errorf("%+v: %s, want %s", tc.d, got, tc.want)
		}
	}
}

func TestElicitationAndUnhandledRequests(t *testing.T) {
	h, log := fake(t)
	rec := newRecorder(func(harness.PermissionRequest) harness.Decision { return harness.Decision{Allow: true} })
	p := start(t, h, rec, harness.StartOptions{})
	p.Send(harness.UserMessage{Text: "elicit"})
	p.Send(harness.UserMessage{Text: "ask"})
	rec.until(t, harness.EventResult, 5*time.Second)
	rec.until(t, harness.EventResult, 5*time.Second)
	req := <-rec.perms
	if req.ID != `"e-1"` || req.Tool != "mcp__layer8" || req.Description != "Allow the do tool?" {
		t.Fatalf("elicitation = %+v", req)
	}
	p.Close(time.Second)
	lines := logLines(t, log)
	if !slices.Contains(lines, `{"id":"e-1","result":{"action":"accept","content":{}}}`) ||
		!slices.Contains(lines, `{"error":{"code":-32601,"message":"layer8 does not handle item/tool/requestUserInput"},"id":103}`) {
		t.Fatalf("replies = %q", lines)
	}
}

func TestSendQueuesWhileATurnRuns(t *testing.T) {
	h, _ := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{})
	rec.until(t, harness.EventSystem, 5*time.Second)
	p.Send(harness.UserMessage{Text: "echo one"})
	p.Send(harness.UserMessage{Text: "echo two"})
	got := slices.Concat(rec.until(t, harness.EventResult, 5*time.Second), rec.until(t, harness.EventResult, 5*time.Second))
	if s := summary(got); !slices.Equal(s, []string{"text:one", "result", "text:two", "result"}) {
		t.Fatalf("events = %v", s)
	}
}

func TestRejectedTurnEndsWithAnErrorResult(t *testing.T) {
	h, _ := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{})
	rec.until(t, harness.EventSystem, 5*time.Second)
	p.Send(harness.UserMessage{Text: "fail"})
	got := rec.until(t, harness.EventResult, 5*time.Second)
	if s := summary(got); !slices.Equal(s, []string{"error", "result"}) || got[1].Data["isError"] != true || got[1].Data["result"] != "thread not loaded" {
		t.Fatalf("events = %v %v", s, got)
	}
	p.Send(harness.UserMessage{Text: "echo next"})
	if s := summary(rec.until(t, harness.EventResult, 5*time.Second)); !slices.Equal(s, []string{"text:next", "result"}) {
		t.Fatalf("after a rejected turn: %v", s)
	}
}

func TestInterrupt(t *testing.T) {
	h, log := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Interrupt(ctx); err != nil {
		t.Fatalf("interrupt while idle: %v", err)
	}
	p.Send(harness.UserMessage{Text: "slow"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		p.Interrupt(ctx)
		if len(sent(t, log, "turn/interrupt")) > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := rec.until(t, harness.EventResult, 5*time.Second)
	if res := got[len(got)-1].Data; res["subtype"] != "interrupted" || res["isError"] != true {
		t.Fatalf("result = %v", res)
	}
	if got := sent(t, log, "turn/interrupt"); len(got) != 1 || got[0] != `{"threadId":"th-1","turnId":"t9"}` {
		t.Fatalf("turn/interrupt = %q", got)
	}
}

func TestUnexpectedExit(t *testing.T) {
	h, _ := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{})
	p.Send(harness.UserMessage{Text: "crash"})
	select {
	case err := <-rec.exit:
		if err == nil || !strings.Contains(err.Error(), "exit status 2") || !strings.Contains(err.Error(), "out of cheese") {
			t.Fatalf("exit err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no exit")
	}
	<-p.Done()
	if err := p.Interrupt(context.Background()); err == nil {
		t.Fatal("interrupt on a dead process succeeded")
	}
}

func TestStartFailsWhenCodexDiesDuringInitialize(t *testing.T) {
	h, _ := fake(t, "FAKE_INIT_CRASH=1")
	rec := newRecorder(nil)
	_, err := h.Start(context.Background(), harness.StartOptions{Cwd: t.TempDir()}, rec)
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
	select {
	case err := <-rec.exit:
		t.Fatalf("OnExit called after a failed Start: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStartFailsWithoutBinary(t *testing.T) {
	h := Harness{Bin: "codex", Env: []string{"PATH=" + t.TempDir()}}
	if _, err := h.Start(context.Background(), harness.StartOptions{}, newRecorder(nil)); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
}

func TestProbe(t *testing.T) {
	h, log := fake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := h.Probe(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(c)
	want := `{"commands":[{"name":"review","description":"Review code"},{"name":"layer8:plan","description":"Plan","plugin":"layer8"}],` +
		`"models":[{"value":"gpt-5","displayName":"GPT-5","description":"Smart","efforts":["low","high"]},{"value":"gpt-5-mini","displayName":"GPT-5 mini","description":"Cheap"}],` +
		`"agents":null,"mcpServers":[{"name":"layer8","status":"connected"},{"name":"linear","status":"needs-auth"}]}`
	if string(got) != want {
		t.Fatalf("capabilities\n got %s\nwant %s", got, want)
	}
	if pages := sent(t, log, "model/list"); !slices.Equal(pages, []string{`{}`, `{"cursor":"p2"}`}) {
		t.Fatalf("model/list pages = %q", pages)
	}
	if len(sent(t, log, "thread/start")) != 0 {
		t.Fatal("probe started a thread")
	}
	if roots := sent(t, log, "skills/extraRoots/set"); !slices.Equal(roots, []string{`{"extraRoots":["/opt/layer8/plugin/skills"]}`}) {
		t.Fatalf("probe skill roots = %q", roots)
	}
}
