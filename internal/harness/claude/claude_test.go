package claude

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

const fakeEnv = "LAYER8_FAKE_CLAUDE_LOG"

func TestMain(m *testing.M) {
	if log := os.Getenv(fakeEnv); log != "" {
		fakeClaude(log)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeClaude speaks a scripted subset of the stream-json protocol, appending its
// argv and every stdin line to the log file.
func fakeClaude(logPath string) {
	logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	argv, _ := json.Marshal(os.Args[1:])
	fmt.Fprintf(logf, "%s\n", argv)
	out := json.NewEncoder(os.Stdout)
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(nil, 1<<20)
	for in.Scan() {
		fmt.Fprintf(logf, "%s\n", in.Bytes())
		var m struct {
			Type      string `json:"type"`
			RequestID string `json:"request_id"`
			Request   struct {
				Subtype string `json:"subtype"`
			} `json:"request"`
			Message struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		json.Unmarshal(in.Bytes(), &m)
		ok := func(body any) {
			out.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": m.RequestID, "response": body}})
		}
		switch {
		case m.Request.Subtype == "initialize" && os.Getenv("FAKE_INIT_CRASH") != "":
			fmt.Fprintln(os.Stderr, "not logged in")
			os.Exit(1)
		case m.Request.Subtype == "initialize":
			ok(map[string]any{
				"commands": []any{
					map[string]any{"name": "review", "description": "Review code (user)", "argumentHint": "[pr]"},
					map[string]any{"name": "layer8:plan", "description": "(layer8) Plan the work"},
					map[string]any{"name": "context", "description": "Show context usage", "builtin": true},
				},
				"models": []any{map[string]any{"value": "opus", "displayName": "Opus", "description": "Best", "supportedEffortLevels": []string{"low", "high"}}},
				"agents": []any{map[string]any{"name": "Explore", "description": "Search code"}},
			})
		case m.Request.Subtype == "mcp_status":
			ok(map[string]any{"mcpServers": []any{
				map[string]any{"name": "layer8", "status": "connected", "source": "dynamic", "config": map[string]any{"headers": map[string]string{"Authorization": "secret"}}},
				map[string]any{"name": "claude.ai Linear", "status": "needs-auth", "source": "claudeai"},
			}})
		case m.Request.Subtype == "interrupt":
			ok(nil)
			out.Encode(map[string]any{"type": "result", "subtype": "error_during_execution", "is_error": true, "session_id": "s1"})
		case m.Type == "control_response":
			out.Encode(map[string]any{"type": "user", "message": map[string]any{"content": []any{
				map[string]any{"type": "tool_result", "tool_use_id": "tu1", "content": "written", "is_error": false}}}})
			out.Encode(map[string]any{"type": "result", "subtype": "success", "session_id": "s1", "total_cost_usd": 0.02})
		case m.Type == "user" && len(m.Message.Content) > 0:
			switch m.Message.Content[0].Text {
			case "hello":
				out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": "s1", "skills": []string{"review"}, "mcp_servers": []any{map[string]any{"name": "layer8", "status": "connected"}}})
				out.Encode(map[string]any{"type": "system", "subtype": "status", "status": "requesting"})
				for _, d := range []string{"Hel", "lo"} {
					out.Encode(map[string]any{"type": "stream_event", "parent_tool_use_id": nil, "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": d}}})
				}
				out.Encode(map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{
					"content": []any{map[string]any{"type": "thinking", "thinking": ""}, map[string]any{"type": "text", "text": "Hello"}},
					"usage":   map[string]int{"input_tokens": 2, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 1000, "output_tokens": 8}}})
				out.Encode(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": "Hello", "session_id": "s1",
					"total_cost_usd": 0.0125, "duration_ms": 900, "num_turns": 1, "usage": map[string]int{"output_tokens": 8}})
			case "write":
				out.Encode(map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{"content": []any{
					map[string]any{"type": "tool_use", "id": "tu1", "name": "Write", "input": map[string]string{"file_path": "a.txt"}}}}})
				out.Encode(map[string]any{"type": "control_request", "request_id": "perm-1", "request": map[string]any{
					"subtype": "can_use_tool", "tool_name": "Write", "input": map[string]string{"file_path": "a.txt"}, "tool_use_id": "tu1",
					"description": "a.txt", "permission_suggestions": json.RawMessage(os.Getenv("FAKE_SUGGESTIONS"))}})
			case "hook":
				out.Encode(map[string]any{"type": "control_request", "request_id": "hook-1", "request": map[string]any{"subtype": "hook_callback"}})
			case "crash":
				fmt.Fprintln(os.Stderr, "fatal: out of cheese")
				os.Exit(2)
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

func (r *recorder) until(t *testing.T, typ harness.EventType) []harness.Event {
	t.Helper()
	var got []harness.Event
	for {
		select {
		case e := <-r.events:
			got = append(got, e)
			if e.Type == typ {
				return got
			}
		case <-time.After(5 * time.Second):
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

func start(t *testing.T, h Harness, rec *recorder, o harness.StartOptions) harness.Proc {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.Cwd = t.TempDir()
	p, err := h.Start(ctx, o, rec)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close(time.Second) })
	return p
}

func TestStartArgvAndInitialize(t *testing.T) {
	h, log := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{
		AddDirs: []string{"/home/a/.layer8/tasks/eng-482"}, Model: "opus", Effort: "high", PermissionMode: "acceptEdits",
		ResumeID: "5f0c", MCPURL: "http://127.0.0.1:7979/mcp", MCPHeaders: map[string]string{"X-Layer8-Session": "9c41e0aa"},
		PluginDirs: []string{"/tmp/extra"}, AppendSystemPrompt: "You run inside layer8.",
	})
	p.Close(time.Second)

	lines := logLines(t, log)
	var argv []string
	json.Unmarshal([]byte(lines[0]), &argv)
	want := []string{"--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
		"--include-partial-messages", "--permission-prompt-tool=stdio", "--setting-sources=user,project,local",
		"--plugin-dir", "/opt/layer8/plugin", "--plugin-dir", "/tmp/extra", "--add-dir", "/home/a/.layer8/tasks/eng-482",
		"--mcp-config", `{"mcpServers":{"layer8":{"type":"http","url":"http://127.0.0.1:7979/mcp","headers":{"X-Layer8-Session":"9c41e0aa"}}}}`,
		"--allowedTools", "mcp__layer8", "--resume", "5f0c", "--model", "opus", "--effort", "high", "--permission-mode", "acceptEdits"}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv\n got %q\nwant %q", argv, want)
	}
	if slices.Contains(argv, "--bare") || slices.Contains(argv, "--strict-mcp-config") {
		t.Fatal("argv must not isolate claude from user config")
	}
	if want := `{"request":{"appendSystemPrompt":"You run inside layer8.","hooks":null,"subtype":"initialize"},"request_id":"layer8-1","type":"control_request"}`; lines[1] != want {
		t.Fatalf("initialize\n got %s\nwant %s", lines[1], want)
	}
	if err := <-rec.exit; err != nil {
		t.Fatalf("exit after Close = %v, want nil", err)
	}
}

func TestSendWireFormat(t *testing.T) {
	h, log := fake(t)
	p := start(t, h, newRecorder(nil), harness.StartOptions{})
	err := p.Send(harness.UserMessage{Text: "look", Files: []string{"/w/a.go", "/w/b.md"}, Images: []harness.Image{{MediaType: "image/png", Data: []byte("png")}}})
	if err != nil {
		t.Fatal(err)
	}
	p.Close(time.Second)
	lines := logLines(t, log)
	want := `{"message":{"content":[{"text":"look\n\nAttached files:\n- /w/a.go\n- /w/b.md","type":"text"},{"source":{"data":"cG5n","media_type":"image/png","type":"base64"},"type":"image"}],"role":"user"},"parent_tool_use_id":null,"session_id":"","type":"user"}`
	if got := lines[len(lines)-1]; got != want {
		t.Fatalf("user message\n got %s\nwant %s", got, want)
	}
}

func TestTurnEvents(t *testing.T) {
	h, _ := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{})
	p.Send(harness.UserMessage{Text: "hello"})
	got := rec.until(t, harness.EventResult)

	var summary []string
	for _, e := range got {
		s := string(e.Type)
		if e.Partial {
			s += "~"
		}
		switch e.Type {
		case harness.EventText:
			s += ":" + e.Data["text"].(string)
		case harness.EventSystem:
			s += ":" + e.Data["subtype"].(string)
		}
		summary = append(summary, s)
	}
	want := []string{"system:init", "system~:status", "text~:Hel", "text~:lo", "text:Hello", "result"}
	if !slices.Equal(summary, want) {
		t.Fatalf("events\n got %v\nwant %v", summary, want)
	}
	init := got[0].Data
	if init["session_id"] != "s1" || init["mcp_servers"] == nil || init["skills"] == nil {
		t.Fatalf("init data = %v", init)
	}
	res := got[len(got)-1].Data
	if res["costUsd"] != 0.0125 || res["contextTokens"] != 1110 || res["sessionId"] != "s1" || res["numTurns"] != 1 || res["isError"] != false {
		t.Fatalf("result data = %v", res)
	}
}

func TestApprovals(t *testing.T) {
	addRules := `[{"type":"setMode","mode":"acceptEdits","destination":"session"},{"type":"addRules","rules":[{"toolName":"Write","ruleContent":"a.txt"}],"behavior":"allow","destination":"localSettings"}]`
	for _, tc := range []struct {
		name        string
		suggestions string
		decision    harness.Decision
		want        string
	}{
		{"allow", "[]", harness.Decision{Allow: true},
			`{"behavior":"allow","updatedInput":{"file_path":"a.txt"}}`},
		{"allow with edited input", "[]", harness.Decision{Allow: true, UpdatedInput: json.RawMessage(`{"file_path":"b.txt"}`)},
			`{"behavior":"allow","updatedInput":{"file_path":"b.txt"}}`},
		{"deny", "[]", harness.Decision{Message: "not that file"},
			`{"behavior":"deny","message":"not that file"}`},
		{"always without suggestions", `[{"type":"setMode","mode":"acceptEdits","destination":"session"}]`, harness.Decision{Allow: true, Always: true},
			`{"behavior":"allow","updatedInput":{"file_path":"a.txt"},"updatedPermissions":[{"behavior":"allow","destination":"session","rules":[{"toolName":"Write"}],"type":"addRules"}]}`},
		{"always with suggested rules", addRules, harness.Decision{Allow: true, Always: true},
			`{"behavior":"allow","updatedInput":{"file_path":"a.txt"},"updatedPermissions":[{"behavior":"allow","destination":"localSettings","rules":[{"ruleContent":"a.txt","toolName":"Write"}],"type":"addRules"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, log := fake(t, "FAKE_SUGGESTIONS="+tc.suggestions)
			rec := newRecorder(func(harness.PermissionRequest) harness.Decision { return tc.decision })
			p := start(t, h, rec, harness.StartOptions{})
			p.Send(harness.UserMessage{Text: "write"})
			got := rec.until(t, harness.EventResult)
			req := <-rec.perms
			if req.ID != "perm-1" || req.Tool != "Write" || req.ToolUseID != "tu1" || string(req.Input) != `{"file_path":"a.txt"}` {
				t.Fatalf("permission request = %+v", req)
			}
			if got[0].Type != harness.EventToolUse || got[1].Type != harness.EventToolResult || got[1].Data["toolUseId"] != "tu1" {
				t.Fatalf("events = %+v", got)
			}
			p.Close(time.Second)
			var resp struct {
				Response struct {
					Subtype   string          `json:"subtype"`
					RequestID string          `json:"request_id"`
					Response  json.RawMessage `json:"response"`
				} `json:"response"`
			}
			lines := logLines(t, log)
			json.Unmarshal([]byte(lines[len(lines)-1]), &resp)
			if resp.Response.Subtype != "success" || resp.Response.RequestID != "perm-1" || string(resp.Response.Response) != tc.want {
				t.Fatalf("control_response\n got %+v %s\nwant %s", resp.Response, resp.Response.Response, tc.want)
			}
		})
	}
}

func TestUnhandledControlRequestGetsAnError(t *testing.T) {
	h, log := fake(t)
	p := start(t, h, newRecorder(nil), harness.StartOptions{})
	p.Send(harness.UserMessage{Text: "hook"})
	deadline := time.Now().Add(5 * time.Second)
	for {
		lines := logLines(t, log)
		if last := lines[len(lines)-1]; strings.Contains(last, `"request_id":"hook-1"`) {
			if !strings.Contains(last, `"subtype":"error"`) {
				t.Fatalf("response = %s", last)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no response to hook_callback")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestInterrupt(t *testing.T) {
	h, _ := fake(t)
	rec := newRecorder(nil)
	p := start(t, h, rec, harness.StartOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	got := rec.until(t, harness.EventResult)
	if res := got[len(got)-1].Data; res["subtype"] != "error_during_execution" || res["isError"] != true {
		t.Fatalf("result = %v", res)
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
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.Interrupt(ctx); err == nil {
		t.Fatal("interrupt on a dead process succeeded")
	}
}

func TestStartFailsWhenClaudeDiesDuringInitialize(t *testing.T) {
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
	h := Harness{Bin: "claude", Env: []string{"PATH=" + t.TempDir()}}
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
	want := `{"commands":[{"name":"review","description":"Review code (user)","argumentHint":"[pr]"},{"name":"layer8:plan","description":"(layer8) Plan the work","plugin":"layer8"},{"name":"context","description":"Show context usage","builtin":true}],` +
		`"models":[{"value":"opus","displayName":"Opus","description":"Best","efforts":["low","high"]}],` +
		`"agents":[{"name":"Explore","description":"Search code"}],` +
		`"mcpServers":[{"name":"layer8","status":"connected","source":"dynamic"},{"name":"claude.ai Linear","status":"needs-auth","source":"claudeai"}]}`
	if string(got) != want {
		t.Fatalf("capabilities\n got %s\nwant %s", got, want)
	}
	var argv []string
	json.Unmarshal([]byte(logLines(t, log)[0]), &argv)
	if slices.Contains(argv, "--mcp-config") || !slices.Contains(argv, "/opt/layer8/plugin") {
		t.Fatalf("probe argv = %q", argv)
	}
}
