package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a fake claude CLI when LAYER8_FAKE_CLAUDE is set: it records
// stdin, asks for one permission and echoes the decision, like the real CLI does.
func TestMain(m *testing.M) {
	if os.Getenv("LAYER8_FAKE_CLAUDE") == "1" {
		fakeCLI()
		return
	}
	os.Exit(m.Run())
}

func fakeCLI() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	rec, _ := os.Create(os.Getenv("LAYER8_FAKE_LOG"))
	defer rec.Close()
	fmt.Fprintln(rec, strings.Join(os.Args[1:], " "))
	for in.Scan() {
		line := in.Text()
		fmt.Fprintln(rec, line)
		var m map[string]any
		json.Unmarshal([]byte(line), &m)
		switch m["type"] {
		case "control_request":
			req := m["request"].(map[string]any)
			if req["subtype"] == "interrupt" {
				out.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": m["request_id"], "response": map[string]any{}}})
			}
		case "user":
			out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": "sess-1"})
			out.Encode(map[string]any{"type": "control_request", "request_id": "perm-1", "request": map[string]any{
				"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "echo hi"}, "tool_use_id": "tu-1"}})
			// Wait for our answer.
			in.Scan()
			ans := in.Text()
			fmt.Fprintln(rec, ans)
			out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"content": []map[string]any{{"type": "text", "text": "got: " + ans}}}})
			out.Encode(map[string]any{"type": "control_request", "request_id": "x-1", "request": map[string]any{"subtype": "something_new"}})
			out.Encode(map[string]any{"type": "result", "subtype": "success", "total_cost_usd": 0.25, "num_turns": 1})
		}
	}
}

func TestProtocolAgainstFakeCLI(t *testing.T) {
	exe, _ := os.Executable()
	logPath := t.TempDir() + "/log"
	t.Setenv("LAYER8_FAKE_CLAUDE", "1")
	t.Setenv("LAYER8_FAKE_LOG", logPath)
	msgs := make(chan Message, 10)
	p, err := Start(Options{Binary: exe, Cwd: t.TempDir(), Resume: "old", PermissionMode: "default", AddDirs: []string{"/art"}, AppendSystemPrompt: "CTX"}, Handlers{
		OnMessage: func(m Message) { msgs <- m },
		OnPermission: func(req PermissionRequest, respond func(Decision)) {
			if req.ToolName != "Bash" || req.Input["command"] != "echo hi" || req.ToolUseID != "tu-1" {
				t.Errorf("request %+v", req)
			}
			go respond(Decision{Allow: false, Message: "nope"})
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send("hello"); err != nil {
		t.Fatal(err)
	}
	var result Message
	var texts []string
	timeout := time.After(10 * time.Second)
	for result.Type == "" {
		select {
		case m := <-msgs:
			if m.Type == "result" {
				result = m
			}
			for _, b := range m.Message.Blocks() {
				texts = append(texts, b.Text)
			}
		case <-timeout:
			t.Fatal("no result")
		}
	}
	if result.TotalCostUSD != 0.25 {
		t.Fatalf("result %+v", result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	p.Shutdown(5 * time.Second)

	b, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	wantArgs := "--output-format stream-json --verbose --input-format stream-json --permission-prompt-tool=stdio --resume=old --setting-sources=user,project,local --permission-mode=default --add-dir=/art"
	if lines[0] != wantArgs {
		t.Fatalf("argv:\n got %s\nwant %s", lines[0], wantArgs)
	}
	var init map[string]any
	json.Unmarshal([]byte(lines[1]), &init)
	req := init["request"].(map[string]any)
	if init["type"] != "control_request" || req["subtype"] != "initialize" || req["appendSystemPrompt"] != "CTX" {
		t.Fatalf("initialize: %s", lines[1])
	}
	var user map[string]any
	json.Unmarshal([]byte(lines[2]), &user)
	if user["type"] != "user" || user["session_id"] != "" || user["parent_tool_use_id"] != nil {
		t.Fatalf("user: %s", lines[2])
	}
	var ans struct {
		Type     string `json:"type"`
		Response struct {
			Subtype   string         `json:"subtype"`
			RequestID string         `json:"request_id"`
			Response  map[string]any `json:"response"`
		} `json:"response"`
	}
	json.Unmarshal([]byte(lines[3]), &ans)
	r := ans.Response
	if ans.Type != "control_response" || r.Subtype != "success" || r.RequestID != "perm-1" ||
		r.Response["behavior"] != "deny" || r.Response["message"] != "nope" || r.Response["toolUseID"] != "tu-1" {
		t.Fatalf("permission answer: %s", lines[3])
	}
	// The unknown control request must get an error response so the CLI never hangs.
	found := false
	for _, l := range lines {
		if strings.Contains(l, `"request_id":"x-1"`) && strings.Contains(l, "Unsupported control request subtype: something_new") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no error response for unknown request:\n%s", b)
	}
}

func TestAllowEchoesInput(t *testing.T) {
	// Allow must carry updatedInput = the original input (what the SDK does).
	var written []byte
	p := &Process{answered: map[string]bool{}, pending: map[string]chan controlResult{}}
	p.stdin = writerFunc(func(b []byte) (int, error) { written = append(written, b...); return len(b), nil })
	p.h.OnPermission = func(req PermissionRequest, respond func(Decision)) {
		respond(Decision{Allow: true})
		respond(Decision{Allow: false})
	}
	p.handleControlRequest("id1", json.RawMessage(`{"subtype":"can_use_tool","tool_name":"Edit","input":{"file_path":"/x"},"tool_use_id":"t"}`))
	got := string(written)
	if strings.Count(got, "\n") != 1 || !strings.Contains(got, `"behavior":"allow"`) || !strings.Contains(got, `"updatedInput":{"file_path":"/x"}`) {
		t.Fatalf("got %s", got)
	}
}

func TestAlwaysSavesSuggestedRulesInUserSettings(t *testing.T) {
	var written []byte
	p := &Process{answered: map[string]bool{}, pending: map[string]chan controlResult{}}
	p.stdin = writerFunc(func(b []byte) (int, error) { written = append(written, b...); return len(b), nil })
	var rules []Rule
	p.h.OnPermission = func(req PermissionRequest, respond func(Decision)) {
		rules = req.AlwaysRules()
		respond(Decision{Allow: true, Always: true})
	}
	p.handleControlRequest("id1", json.RawMessage(`{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"npm test"},"tool_use_id":"t",
		"permission_suggestions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"localSettings"},{"type":"setMode","mode":"acceptEdits","destination":"session"}]}`))
	if len(rules) != 1 || rules[0].String() != "Bash(npm test:*)" {
		t.Fatalf("rules %+v", rules)
	}
	got := string(written)
	want := `"updatedPermissions":[{"type":"addRules","rules":[{"toolName":"Bash","ruleContent":"npm test:*"}],"behavior":"allow","destination":"userSettings"}]`
	if !strings.Contains(got, want) {
		t.Fatalf("got %s", got)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(b []byte) (int, error) { return f(b) }
func (f writerFunc) Close() error                { return nil }

func TestArgsWithModel(t *testing.T) {
	a := strings.Join(Args(Options{Model: "opus"}), " ")
	if !strings.Contains(a, "--model=opus") || strings.Contains(a, "--resume") || strings.Contains(a, "--permission-mode") {
		t.Fatal(a)
	}
}

func TestCleanEnv(t *testing.T) {
	got := CleanEnv([]string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=x", "CLAUDE_CONFIG_DIR=/c", "NODE_OPTIONS=--inspect", "ANTHROPIC_API_KEY=k"})
	if strings.Join(got, ",") != "PATH=/bin,CLAUDE_CONFIG_DIR=/c,ANTHROPIC_API_KEY=k" {
		t.Fatal(got)
	}
}

func TestArgsGiveTheSessionLayer8(t *testing.T) {
	a := Args(Options{MCP: &MCPServer{Name: "layer8", URL: "http://127.0.0.1:7878/api/mcp", Headers: map[string]string{"Authorization": "Bearer t"}}})
	joined := strings.Join(a, " ")
	if !strings.Contains(joined, `--mcp-config={"mcpServers":{"layer8":{"headers":{"Authorization":"Bearer t"},"type":"http","url":"http://127.0.0.1:7878/api/mcp"}}}`) || !strings.Contains(joined, "--allowedTools=mcp__layer8__look") {
		t.Fatal(joined)
	}
}
