package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
)

// TestMain doubles as a scripted `codex app-server` when FAKE_CODEX is set.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_CODEX") != "" {
		fakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

type rpc struct {
	ID     any             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
}

func fakeServer() {
	in := bufio.NewScanner(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	next := func() (rpc, bool) {
		var r rpc
		if !in.Scan() {
			return r, false
		}
		_ = json.Unmarshal(in.Bytes(), &r)
		return r, true
	}
	send := func(v map[string]any) { _ = out.Encode(v) }
	notify := func(method string, params map[string]any) { send(map[string]any{"method": method, "params": params}) }
	completed := func(status string) {
		notify("turn/completed", map[string]any{"threadId": "thr", "turn": map[string]any{"id": "t1", "status": status, "durationMs": 5}})
	}
	for {
		r, ok := next()
		if !ok {
			return
		}
		switch r.Method {
		case "initialize":
			send(map[string]any{"id": r.ID, "result": map[string]any{}})
		case "thread/start":
			send(map[string]any{"id": r.ID, "result": map[string]any{"thread": map[string]any{"id": "thr"}}})
		case "turn/interrupt":
			send(map[string]any{"id": r.ID, "result": map[string]any{}})
			completed("interrupted")
		case "turn/start":
			var p struct {
				Input []struct {
					Text string `json:"text"`
				} `json:"input"`
			}
			_ = json.Unmarshal(r.Params, &p)
			send(map[string]any{"id": r.ID, "result": map[string]any{"turn": map[string]any{"id": "t1"}}})
			notify("turn/started", map[string]any{"threadId": "thr", "turn": map[string]any{"id": "t1", "status": "inProgress"}})
			if p.Input[0].Text != "hello" {
				continue
			}
			notify("item/completed", map[string]any{"item": map[string]any{"type": "agentMessage", "id": "m1", "text": "hi"}})
			notify("item/started", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "status": "inProgress"}})
			send(map[string]any{"id": "ap1", "method": "item/commandExecution/requestApproval", "params": map[string]any{"itemId": "c1", "command": "ls"}})
			answer, _ := next()
			var d struct {
				Decision string `json:"decision"`
			}
			_ = json.Unmarshal(answer.Result, &d)
			if d.Decision != "accept" {
				notify("item/completed", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "status": "declined"}})
			} else {
				notify("item/completed", map[string]any{"item": map[string]any{"type": "commandExecution", "id": "c1", "command": "ls", "status": "completed", "aggregatedOutput": "a.txt", "exitCode": 0}})
			}
			notify("thread/tokenUsage/updated", map[string]any{"threadId": "thr", "turnId": "t1", "tokenUsage": map[string]any{
				"last":               map[string]any{"inputTokens": 100, "cachedInputTokens": 40, "outputTokens": 10, "totalTokens": 110},
				"modelContextWindow": 1000,
			}})
			completed("completed")
		}
	}
}

func startFake(t *testing.T, onPermission func(claude.PermissionRequest, func(claude.Decision))) (*Process, chan claude.Message) {
	t.Helper()
	msgs := make(chan claude.Message, 100)
	p, err := Start(claude.Options{Binary: os.Args[0], Env: []string{"FAKE_CODEX=1"}}, claude.Handlers{
		OnMessage:    func(m claude.Message) { msgs <- m },
		OnPermission: onPermission,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Shutdown(5 * time.Second) })
	return p, msgs
}

func waitFor(t *testing.T, msgs chan claude.Message, typ string) []claude.Message {
	t.Helper()
	var seen []claude.Message
	for {
		select {
		case m := <-msgs:
			seen = append(seen, m)
			if m.Type == typ {
				return seen
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no %s message; saw %v", typ, seen)
		}
	}
}

// summary renders a message as one comparable line.
func summary(m claude.Message) string {
	switch m.Type {
	case "system":
		return m.Subtype + " " + m.SessionID
	case "result":
		return fmt.Sprintf("result %s %d", m.Subtype, m.ModelUsage["codex"].ContextWindow)
	}
	if u := m.Message.Usage; u != nil {
		return fmt.Sprintf("usage %d", u.Context())
	}
	b := m.Message.Blocks()[0]
	switch b.Type {
	case "tool_use":
		return fmt.Sprintf("tool_use %s %s %v", b.ID, b.Name, b.Input["command"])
	case "tool_result":
		return fmt.Sprintf("tool_result %s %q %v", b.ToolUseID, b.ResultText(), b.IsError)
	}
	return b.Type + " " + b.Text
}

func TestTurnWithApproval(t *testing.T) {
	asked := make(chan claude.PermissionRequest, 1)
	p, msgs := startFake(t, func(req claude.PermissionRequest, respond func(claude.Decision)) {
		asked <- req
		go respond(claude.Decision{Allow: true})
	})
	if err := p.Send("hello"); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range waitFor(t, msgs, "result") {
		got = append(got, summary(m))
	}
	want := []string{
		"init thr",
		"text hi",
		"tool_use c1 Bash ls",
		`tool_result c1 "a.txt" false`,
		"usage 110",
		"result success 1000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("messages:\n got %q\nwant %q", got, want)
	}
	req := <-asked
	if req.RequestID != `"ap1"` || req.ToolName != "Bash" || req.Input["command"] != "ls" || req.ToolUseID != "c1" {
		t.Fatalf("permission request = %+v", req)
	}
}

func TestInterruptEndsTurnWithResult(t *testing.T) {
	p, msgs := startFake(t, nil)
	if err := p.Send("wait"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, msgs, "system")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := p.Interrupt(ctx); err != nil {
		t.Fatal(err)
	}
	seen := waitFor(t, msgs, "result")
	if r := seen[len(seen)-1]; r.Subtype != "error_during_execution" || r.IsError {
		t.Fatalf("result = %+v", r)
	}
	p.Shutdown(5 * time.Second)
	select {
	case <-p.Done():
	default:
		t.Fatal("process still running after Shutdown")
	}
}
