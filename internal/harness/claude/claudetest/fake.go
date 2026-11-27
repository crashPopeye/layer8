// Package claudetest is a fake claude CLI for tests: a test binary calls Main from
// TestMain and is then started as claude with LogEnv set.
package claudetest

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
)

// LogEnv names the file the fake appends its argv and every stdin line to.
const LogEnv = "LAYER8_FAKE_CLAUDE_LOG"

// Main runs the fake and exits when LogEnv is set, and returns otherwise.
func Main() {
	if log := os.Getenv(LogEnv); log != "" {
		run(log)
		os.Exit(0)
	}
}

// run speaks a scripted subset of the stream-json protocol. User turns select the script:
// "hello", "write" (asks to use Write), "hook", "crash", and "mcp <ops json>" (calls the
// layer8 do tool through the --mcp-config server and replies with its text).
func run(logPath string) {
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
		reply := func(text string) {
			out.Encode(map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{
				"content": []any{map[string]any{"type": "text", "text": text}}}})
			out.Encode(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": text, "session_id": "s1", "total_cost_usd": 0.01})
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
			text := m.Message.Content[0].Text
			switch {
			case text == "hello":
				out.Encode(map[string]any{"type": "system", "subtype": "init", "session_id": "s1", "skills": []string{"review"}, "mcp_servers": []any{map[string]any{"name": "layer8", "status": "connected"}}})
				out.Encode(map[string]any{"type": "system", "subtype": "status", "status": "requesting"})
				out.Encode(map[string]any{"type": "rate_limit_event", "session_id": "s1", "rate_limit_info": map[string]any{"status": "allowed", "unifiedWindows": map[string]any{
					"five_hour": map[string]any{"utilization": 0.42, "resetsAt": 1764252000}, "seven_day": map[string]any{"utilization": 0.18, "resetsAt": 1764720000}}}})
				for _, d := range []string{"Hel", "lo"} {
					out.Encode(map[string]any{"type": "stream_event", "parent_tool_use_id": nil, "event": map[string]any{"type": "content_block_delta", "delta": map[string]any{"type": "text_delta", "text": d}}})
				}
				out.Encode(map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{
					"content": []any{map[string]any{"type": "thinking", "thinking": ""}, map[string]any{"type": "text", "text": "Hello"}},
					"usage":   map[string]int{"input_tokens": 2, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 1000, "output_tokens": 8}}})
				out.Encode(map[string]any{"type": "result", "subtype": "success", "is_error": false, "result": "Hello", "session_id": "s1",
					"total_cost_usd": 0.0125, "duration_ms": 900, "num_turns": 1, "usage": map[string]int{"input_tokens": 2, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 1000, "output_tokens": 8},
					"modelUsage": map[string]any{"claude-opus-5-5": map[string]int{"contextWindow": 200000}, "claude-haiku-5-5": map[string]int{"contextWindow": 100000}}})
			case text == "write":
				out.Encode(map[string]any{"type": "assistant", "parent_tool_use_id": nil, "message": map[string]any{"content": []any{
					map[string]any{"type": "tool_use", "id": "tu1", "name": "Write", "input": map[string]string{"file_path": "a.txt"}}}}})
				out.Encode(map[string]any{"type": "control_request", "request_id": "perm-1", "request": map[string]any{
					"subtype": "can_use_tool", "tool_name": "Write", "input": map[string]string{"file_path": "a.txt"}, "tool_use_id": "tu1",
					"description": "a.txt", "permission_suggestions": json.RawMessage(cmp.Or(os.Getenv("FAKE_SUGGESTIONS"), "[]"))}})
			case text == "hook":
				out.Encode(map[string]any{"type": "control_request", "request_id": "hook-1", "request": map[string]any{"subtype": "hook_callback"}})
			case text == "crash":
				fmt.Fprintln(os.Stderr, "fatal: out of cheese")
				os.Exit(2)
			case strings.HasPrefix(text, "mcp "):
				reply(callDo(json.RawMessage(strings.TrimPrefix(text, "mcp "))))
			}
		}
	}
}

// callDo calls the do tool of the layer8 server named in --mcp-config and returns its text.
func callDo(ops json.RawMessage) string {
	i := slices.Index(os.Args, "--mcp-config")
	if i < 0 {
		return "no --mcp-config"
	}
	var cfg struct {
		MCPServers map[string]struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"mcpServers"`
	}
	json.Unmarshal([]byte(os.Args[i+1]), &cfg)
	srv := cfg.MCPServers["layer8"]
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "do", "arguments": map[string]any{"ops": ops}}})
	req, _ := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
	for k, v := range srv.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if json.Unmarshal(raw, &r) != nil || len(r.Result.Content) == 0 {
		return string(raw)
	}
	return r.Result.Content[0].Text
}
