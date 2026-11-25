package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as a fake claude CLI when L8_FAKE_CLAUDE=1: it logs argv and
// stdin to L8_FAKE_LOG and plays one turn with a permission and a hook round-trip.
func TestMain(m *testing.M) {
	if os.Getenv("L8_FAKE_CLAUDE") == "1" {
		fakeCLI()
		return
	}
	os.Exit(m.Run())
}

func fakeCLI() {
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 1<<20), 1<<20)
	out := json.NewEncoder(os.Stdout)
	log, _ := os.Create(os.Getenv("L8_FAKE_LOG"))
	defer log.Close()
	json.NewEncoder(log).Encode(os.Args[1:])
	next := func() map[string]any {
		in.Scan()
		fmt.Fprintln(log, in.Text())
		var m map[string]any
		json.Unmarshal(in.Bytes(), &m)
		return m
	}
	for {
		m := next()
		if m == nil {
			return
		}
		if m["type"] == "control_request" {
			out.Encode(map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": m["request_id"], "response": map[string]any{}}})
			continue
		}
		if m["type"] != "user" {
			continue
		}
		out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": "sess-1"})
		out.Encode(map[string]any{"type": "control_request", "request_id": "perm-1", "request": map[string]any{
			"subtype": "can_use_tool", "tool_name": "Bash", "input": map[string]any{"command": "go version"}, "tool_use_id": "tu-1",
			"permission_suggestions": []any{map[string]any{"type": "addRules", "rules": []any{map[string]any{"toolName": "Bash", "ruleContent": "go version"}}, "behavior": "allow", "destination": "localSettings"}},
		}})
		next()
		out.Encode(map[string]any{"type": "control_request", "request_id": "hook-1", "request": map[string]any{
			"subtype": "hook_callback", "callback_id": "guard", "tool_use_id": "tu-2",
			"input": map[string]any{"hook_event_name": "PreToolUse", "tool_name": "Write", "tool_input": map[string]any{"file_path": "/x/.layer8/task/parent/a.md"}},
		}})
		next()
		out.Encode(map[string]any{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "done"}}}})
		out.Encode(map[string]any{"type": "result", "subtype": "success", "total_cost_usd": 0.25, "session_id": "sess-1"})
	}
}

func TestProtocolAgainstFakeCLI(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	logPath := t.TempDir() + "/log"
	t.Setenv("L8_FAKE_CLAUDE", "1")
	t.Setenv("L8_FAKE_LOG", logPath)

	msgs := make(chan Message, 16)
	exited := make(chan error, 1)
	hookCalls := make(chan HookCall, 1)
	p, err := Spawn(Options{
		Binary: exe, Cwd: t.TempDir(), PluginDir: "/plug", AddDirs: []string{"/art"}, PermissionMode: "default",
		SystemPromptAppend: "<layer8_session>CTX</layer8_session>",
		Hooks:              []HookMatcher{{Event: "PreToolUse", Matcher: "Write|Edit", ID: "guard"}},
	}, Handlers{
		OnMessage: func(m Message) { msgs <- m },
		OnPermission: func(req PermissionRequest) PermissionDecision {
			if req.ToolName != "Bash" || req.ToolUseID != "tu-1" || len(req.Suggestions) != 1 {
				t.Errorf("permission request %+v", req)
			}
			return PermissionDecision{Allow: true, UpdatedPermissions: req.Suggestions}
		},
		OnHook: func(c HookCall) HookResult {
			hookCalls <- c
			return HookResult{Deny: "parent artifacts are read-only"}
		},
		OnExit: func(err error) { exited <- err },
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Send("hello"); err != nil {
		t.Fatal(err)
	}
	var result Message
	for result.Type != "result" {
		select {
		case result = <-msgs:
		case <-time.After(10 * time.Second):
			t.Fatal("no result")
		}
	}
	if result.TotalCostUSD != 0.25 || result.SessionID != "sess-1" {
		t.Fatalf("result = %+v", result)
	}
	if err := p.Interrupt(); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPermissionMode("acceptEdits"); err != nil {
		t.Fatal(err)
	}
	if err := p.SetPermissionMode(""); err != nil {
		t.Fatal(err)
	}
	if err := p.SetModel("sonnet"); err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("OnExit not called")
	}
	if hookCall := <-hookCalls; hookCall.CallbackID != "guard" || hookCall.Input.ToolName != "Write" || hookCall.ToolUseID != "tu-2" {
		t.Fatalf("hook call = %+v", hookCall)
	}

	raw, _ := os.ReadFile(logPath)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	var argv []string
	json.Unmarshal([]byte(lines[0]), &argv)
	for _, want := range [][]string{
		{"--input-format", "stream-json"}, {"--output-format", "stream-json"}, {"--include-partial-messages"},
		{"--permission-prompt-tool=stdio"}, {"--setting-sources=user,project,local"},
		{"--plugin-dir", "/plug"}, {"--add-dir", "/art"}, {"--permission-mode", "default"},
	} {
		if !containsRun(argv, want) {
			t.Errorf("argv %v lacks %v", argv, want)
		}
	}

	var init struct {
		Type    string `json:"type"`
		Request struct {
			Subtype            string                       `json:"subtype"`
			AppendSystemPrompt string                       `json:"appendSystemPrompt"`
			Hooks              map[string][]json.RawMessage `json:"hooks"`
		} `json:"request"`
	}
	json.Unmarshal([]byte(lines[1]), &init)
	if init.Type != "control_request" || init.Request.Subtype != "initialize" || !strings.Contains(init.Request.AppendSystemPrompt, "<layer8_session>") {
		t.Fatalf("initialize = %s", lines[1])
	}
	if hooks := init.Request.Hooks["PreToolUse"]; len(hooks) != 1 || string(hooks[0]) != `{"hookCallbackIds":["guard"],"matcher":"Write|Edit"}` {
		t.Fatalf("hooks = %s", lines[1])
	}

	perm := response(t, lines, "perm-1")
	if perm["behavior"] != "allow" || perm["toolUseID"] != "tu-1" {
		t.Fatalf("permission response = %v", perm)
	}
	if in, _ := perm["updatedInput"].(map[string]any); in["command"] != "go version" {
		t.Fatalf("updatedInput = %v", perm["updatedInput"])
	}
	if ups, _ := perm["updatedPermissions"].([]any); len(ups) != 1 {
		t.Fatalf("updatedPermissions = %v", perm["updatedPermissions"])
	}

	var modes []any
	for _, l := range lines {
		var m struct {
			Request map[string]any `json:"request"`
		}
		if json.Unmarshal([]byte(l), &m) == nil && m.Request["subtype"] == "set_permission_mode" {
			modes = append(modes, m.Request["mode"])
		}
		if m.Request["subtype"] == "set_model" && m.Request["model"] != "sonnet" {
			t.Errorf("set_model = %s", l)
		}
	}
	if !slices.Equal(modes, []any{"acceptEdits", "default"}) {
		t.Fatalf("set_permission_mode modes = %v", modes)
	}

	hook := response(t, lines, "hook-1")
	specific, _ := hook["hookSpecificOutput"].(map[string]any)
	if specific["hookEventName"] != "PreToolUse" || specific["permissionDecision"] != "deny" || specific["permissionDecisionReason"] != "parent artifacts are read-only" {
		t.Fatalf("hook response = %v", hook)
	}
}

func TestArgsResumeAndFork(t *testing.T) {
	args := Args(Options{Resume: "abc", Fork: true, Model: "opus", Effort: "high"})
	for _, want := range [][]string{{"--resume", "abc", "--fork-session"}, {"--model", "opus"}, {"--effort", "high"}} {
		if !containsRun(args, want) {
			t.Errorf("args %v lacks %v", args, want)
		}
	}
	if slices.Contains(Args(Options{Fork: true}), "--fork-session") {
		t.Error("--fork-session without --resume")
	}
}

func TestCleanEnv(t *testing.T) {
	got := CleanEnv([]string{"PATH=/bin", "CLAUDECODE=1", "NODE_OPTIONS=--x", "CLAUDE_CONFIG_DIR=/c"})
	if !slices.Equal(got, []string{"PATH=/bin", "CLAUDE_CONFIG_DIR=/c"}) {
		t.Fatalf("CleanEnv = %v", got)
	}
}

func containsRun(list, run []string) bool {
	for i := range list {
		if i+len(run) <= len(list) && slices.Equal(list[i:i+len(run)], run) {
			return true
		}
	}
	return false
}

// response finds the control_response the host wrote for request id.
func response(t *testing.T, lines []string, id string) map[string]any {
	t.Helper()
	for _, l := range lines {
		var m struct {
			Type     string `json:"type"`
			Response struct {
				Subtype   string         `json:"subtype"`
				RequestID string         `json:"request_id"`
				Response  map[string]any `json:"response"`
			} `json:"response"`
		}
		if json.Unmarshal([]byte(l), &m) == nil && m.Type == "control_response" && m.Response.RequestID == id {
			if m.Response.Subtype != "success" {
				t.Fatalf("response to %s: %s", id, l)
			}
			return m.Response.Response
		}
	}
	t.Fatalf("no response to %s in:\n%s", id, strings.Join(lines, "\n"))
	return nil
}

func TestCommands(t *testing.T) {
	m := Message{SlashCommands: []string{"compact", "/l8:create-prd"}, Skills: json.RawMessage(`["show-me",{"name":"l8:create-prd"}]`)}
	if got := m.Commands(); !slices.Equal(got, []string{"compact", "l8:create-prd", "show-me"}) {
		t.Fatalf("Commands = %v", got)
	}
}
