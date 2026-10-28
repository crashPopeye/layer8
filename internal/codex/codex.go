// Package codex drives the Codex CLI over its app-server protocol (JSON-RPC,
// one object per line on stdio; verified against codex-cli 0.147.0):
//
//	codex app-server
//
// One long-lived server runs one thread. Its notifications are translated into
// the claude.Message values the Claude CLI produces, so the session manager
// drives either harness the same way.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
)

type Process struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	h     claude.Handlers
	o     claude.Options

	writeMu  sync.Mutex
	mu       sync.Mutex
	nextID   int64
	pending  map[int64]chan rpcResult
	answered map[string]bool // server requests answered or withdrawn
	closed   bool

	thread    string
	turn      string
	turnStart time.Time
	tools     map[string]toolCall // started tool items, by item id
	window    int
	lastText  string

	done chan struct{}
	err  error
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResult struct {
	result json.RawMessage
	err    *rpcError
}

type toolCall struct {
	name  string
	input map[string]any
}

// Start spawns `codex app-server`, initializes it and starts (or resumes) the thread.
func Start(o claude.Options, h claude.Handlers) (*Process, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p, err := spawn(ctx, o, h)
	if err != nil {
		return nil, err
	}
	if err := p.openThread(ctx); err != nil {
		p.Kill()
		<-p.done
		return nil, err
	}
	return p, nil
}

func spawn(ctx context.Context, o claude.Options, h claude.Handlers) (*Process, error) {
	bin := o.Binary
	if bin == "" {
		bin = "codex"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("codex CLI not found on PATH (install Codex and log in): %w", err)
	}
	cmd := exec.Command(bin, "app-server")
	cmd.Dir = o.Cwd
	cmd.Env = append(claude.CleanEnv(os.Environ()), o.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Process{
		cmd: cmd, stdin: stdin, h: h, o: o,
		pending: map[int64]chan rpcResult{}, answered: map[string]bool{}, tools: map[string]toolCall{},
		done: make(chan struct{}),
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); p.readStdout(stdout) }()
	go func() { defer readers.Done(); p.readStderr(stderr) }()
	go func() {
		readers.Wait()
		p.err = cmd.Wait()
		p.mu.Lock()
		p.closed = true
		for id, ch := range p.pending {
			ch <- rpcResult{err: &rpcError{Message: "codex app-server exited"}}
			delete(p.pending, id)
		}
		p.mu.Unlock()
		close(p.done)
	}()

	init := map[string]any{"clientInfo": map[string]any{"name": "layer8", "title": "layer8", "version": "1"}, "capabilities": nil}
	err = p.call(ctx, "initialize", init, nil)
	if err == nil {
		err = p.write(map[string]any{"method": "initialized"})
	}
	if err != nil {
		p.Kill()
		<-p.done
		return nil, err
	}
	return p, nil
}

// permissions maps a layer8 permission mode to Codex's approval policy, approvals
// reviewer and sandbox; empty values leave the user's Codex config in charge.
func permissions(mode string) (approval, reviewer, sandbox string) {
	switch mode {
	case "default":
		return "untrusted", "", "workspace-write"
	case "acceptEdits":
		return "on-request", "", "workspace-write"
	case "auto":
		return "on-request", "auto_review", "workspace-write"
	case "bypassPermissions":
		return "never", "", "danger-full-access"
	}
	return "", "", ""
}

// threadParams are the thread/start (or thread/resume) params.
func threadParams(o claude.Options) map[string]any {
	params := map[string]any{}
	set := func(k, v string) {
		if v != "" {
			params[k] = v
		}
	}
	approval, reviewer, sandbox := permissions(o.PermissionMode)
	set("threadId", o.Resume)
	set("cwd", o.Cwd)
	set("model", o.Model)
	set("developerInstructions", o.AppendSystemPrompt)
	set("approvalPolicy", approval)
	set("approvalsReviewer", reviewer)
	set("sandbox", sandbox)
	if len(o.AddDirs) > 0 {
		params["config"] = map[string]any{"sandbox_workspace_write": map[string]any{"writable_roots": o.AddDirs}}
	}
	return params
}

func (p *Process) openThread(ctx context.Context) error {
	method := "thread/start"
	if p.o.Resume != "" {
		method = "thread/resume"
	}
	var res struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := p.call(ctx, method, threadParams(p.o), &res); err != nil {
		return err
	}
	p.mu.Lock()
	p.thread = res.Thread.ID
	p.mu.Unlock()
	return nil
}

// Done is closed when the process has exited; Err then reports how.
func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Err() error            { return p.err }

// Send starts a turn with a user message; "/command" and "$skill" text goes through as typed.
func (p *Process) Send(text string) error {
	p.mu.Lock()
	params := map[string]any{
		"threadId": p.thread,
		"input":    []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}},
	}
	p.mu.Unlock()
	if p.o.Effort != "" {
		params["effort"] = p.o.Effort
	}
	var res struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := p.call(context.Background(), "turn/start", params, &res); err != nil {
		return err
	}
	p.mu.Lock()
	if p.turn == "" {
		p.turn = res.Turn.ID
	}
	p.mu.Unlock()
	return nil
}

// Interrupt stops the current turn; it still ends with a result message.
func (p *Process) Interrupt(ctx context.Context) error {
	p.mu.Lock()
	thread, turn := p.thread, p.turn
	p.mu.Unlock()
	if turn == "" {
		return errors.New("no turn is running")
	}
	return p.call(ctx, "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, nil)
}

// Close ends stdin; the server exits once it notices.
func (p *Process) Close() error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.stdin.Close()
}

// Kill terminates the process immediately.
func (p *Process) Kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

// Shutdown closes stdin and kills the process if it hasn't exited after grace.
func (p *Process) Shutdown(grace time.Duration) {
	_ = p.Close()
	select {
	case <-p.done:
	case <-time.After(grace):
		p.Kill()
		<-p.done
	}
}

func (p *Process) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("codex stdin: %w", err)
	}
	return nil
}

func (p *Process) call(ctx context.Context, method string, params, out any) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errors.New("codex app-server has exited")
	}
	p.nextID++
	id := p.nextID
	ch := make(chan rpcResult, 1)
	p.pending[id] = ch
	p.mu.Unlock()
	forget := func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}
	if err := p.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		forget()
		return err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return fmt.Errorf("codex %s: %s", method, r.err.Message)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(r.result, out)
	case <-ctx.Done():
		forget()
		return ctx.Err()
	}
}

func (p *Process) readStderr(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		if p.h.OnStderr != nil {
			p.h.OnStderr(sc.Text())
		}
	}
}

func (p *Process) readStdout(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			p.handleLine(line)
		}
		if err != nil {
			return
		}
	}
}

func (p *Process) handleLine(line []byte) {
	var msg struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.Unmarshal(line, &msg); err != nil {
		if p.h.OnStderr != nil {
			p.h.OnStderr("unparsable stdout line: " + string(line))
		}
		return
	}
	switch {
	case msg.Method != "" && msg.ID != nil:
		p.handleRequest(msg.ID, msg.Method, msg.Params)
	case msg.Method != "":
		p.handleNotification(msg.Method, msg.Params)
	default:
		var id int64
		if json.Unmarshal(msg.ID, &id) != nil {
			return
		}
		p.mu.Lock()
		ch, ok := p.pending[id]
		delete(p.pending, id)
		p.mu.Unlock()
		if ok {
			ch <- rpcResult{result: msg.Result, err: msg.Error}
		}
	}
}

// item is the subset of a Codex ThreadItem layer8 shows.
type item struct {
	Type             string  `json:"type"`
	ID               string  `json:"id"`
	Text             string  `json:"text"`
	Command          string  `json:"command"`
	Status           string  `json:"status"`
	AggregatedOutput *string `json:"aggregatedOutput"`
	ExitCode         *int    `json:"exitCode"`
	Changes          []struct {
		Path string `json:"path"`
		Kind struct {
			Type string `json:"type"`
		} `json:"kind"`
		Diff string `json:"diff"`
	} `json:"changes"`
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Result    *struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"result"`
	Error *turnError `json:"error"`
	Query string     `json:"query"`
}

type turnError struct {
	Message string `json:"message"`
}

// toolUse names an item the way Claude Code names the equivalent tool.
func (it item) toolUse() (toolCall, bool) {
	switch it.Type {
	case "commandExecution":
		return toolCall{"Bash", map[string]any{"command": it.Command}}, true
	case "fileChange":
		in := map[string]any{}
		var diffs []string
		for _, c := range it.Changes {
			diffs = append(diffs, c.Diff)
		}
		if len(it.Changes) > 0 {
			in["file_path"] = it.Changes[0].Path
			in["diff"] = strings.Join(diffs, "\n")
		}
		if len(it.Changes) == 1 && it.Changes[0].Kind.Type == "add" {
			return toolCall{"Write", in}, true
		}
		return toolCall{"Edit", in}, true
	case "mcpToolCall":
		var in map[string]any
		_ = json.Unmarshal(it.Arguments, &in)
		return toolCall{"mcp__" + it.Server + "__" + it.Tool, in}, true
	case "webSearch":
		return toolCall{"WebSearch", map[string]any{"query": it.Query}}, true
	}
	return toolCall{}, false
}

// toolResult is the item's output once it has completed.
func (it item) toolResult() (string, bool) {
	failed := it.Status == "failed" || it.Status == "declined"
	switch it.Type {
	case "commandExecution":
		out := ""
		if it.AggregatedOutput != nil {
			out = *it.AggregatedOutput
		}
		return out, failed || (it.ExitCode != nil && *it.ExitCode != 0)
	case "fileChange":
		var paths []string
		for _, c := range it.Changes {
			paths = append(paths, c.Path)
		}
		return it.Status + ": " + strings.Join(paths, ", "), failed
	case "mcpToolCall":
		if it.Error != nil {
			return it.Error.Message, true
		}
		if it.Result == nil {
			return "", failed
		}
		var parts []string
		for _, b := range it.Result.Content {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			} else {
				parts = append(parts, "["+b.Type+"]")
			}
		}
		return strings.Join(parts, "\n"), failed
	case "webSearch":
		return it.Query, failed
	}
	return "", failed
}

func (p *Process) handleNotification(method string, raw json.RawMessage) {
	var n struct {
		Turn struct {
			ID         string     `json:"id"`
			Status     string     `json:"status"`
			Error      *turnError `json:"error"`
			DurationMS *int64     `json:"durationMs"`
		} `json:"turn"`
		Item       item `json:"item"`
		TokenUsage struct {
			Last struct {
				InputTokens       int `json:"inputTokens"`
				CachedInputTokens int `json:"cachedInputTokens"`
				OutputTokens      int `json:"outputTokens"`
			} `json:"last"`
			ModelContextWindow int `json:"modelContextWindow"`
		} `json:"tokenUsage"`
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := json.Unmarshal(raw, &n); err != nil {
		return
	}
	switch method {
	case "turn/started":
		p.mu.Lock()
		p.turn, p.turnStart, p.lastText = n.Turn.ID, time.Now(), ""
		thread := p.thread
		p.mu.Unlock()
		p.emit(claude.Message{Type: "system", Subtype: "init", SessionID: thread})
	case "item/started":
		if call, ok := n.Item.toolUse(); ok {
			p.mu.Lock()
			p.tools[n.Item.ID] = call
			p.mu.Unlock()
			p.toolUse(n.Item.ID, call)
		}
	case "item/completed":
		p.completeItem(n.Item)
	case "thread/tokenUsage/updated":
		u := n.TokenUsage.Last
		p.mu.Lock()
		if n.TokenUsage.ModelContextWindow > 0 {
			p.window = n.TokenUsage.ModelContextWindow
		}
		p.mu.Unlock()
		p.assistant(nil, &claude.Usage{
			InputTokens:          u.InputTokens - u.CachedInputTokens,
			CacheReadInputTokens: u.CachedInputTokens,
			OutputTokens:         u.OutputTokens,
		})
	case "turn/completed":
		p.completeTurn(n.Turn.Status, n.Turn.Error, n.Turn.DurationMS)
	case "serverRequest/resolved":
		id := string(n.RequestID)
		p.mu.Lock()
		cancelled := !p.answered[id]
		p.answered[id] = true
		p.mu.Unlock()
		if cancelled && p.h.OnPermissionCancelled != nil {
			p.h.OnPermissionCancelled(id)
		}
	}
}

func (p *Process) toolUse(id string, call toolCall) {
	p.assistant([]claude.Block{{Type: "tool_use", ID: id, Name: call.name, Input: call.input}}, nil)
}

func (p *Process) completeItem(it item) {
	if it.Type == "agentMessage" {
		p.mu.Lock()
		p.lastText = it.Text
		p.mu.Unlock()
		p.assistant([]claude.Block{{Type: "text", Text: it.Text}}, nil)
		return
	}
	call, ok := it.toolUse()
	if !ok {
		return
	}
	p.mu.Lock()
	_, started := p.tools[it.ID]
	delete(p.tools, it.ID)
	thread := p.thread
	p.mu.Unlock()
	if !started {
		p.toolUse(it.ID, call)
	}
	text, isErr := it.toolResult()
	content, _ := json.Marshal(text)
	blocks, _ := json.Marshal([]claude.Block{{Type: "tool_result", ToolUseID: it.ID, Content: content, IsError: isErr}})
	p.emit(claude.Message{Type: "user", SessionID: thread, Message: &claude.APIMessage{Role: "user", Content: blocks}})
}

func (p *Process) completeTurn(status string, turnErr *turnError, durationMS *int64) {
	p.mu.Lock()
	thread, window, text, started := p.thread, p.window, p.lastText, p.turnStart
	p.turn = ""
	p.tools = map[string]toolCall{}
	p.mu.Unlock()
	m := claude.Message{
		Type: "result", Subtype: "success", SessionID: thread, NumTurns: 1, Result: text,
		DurationMS: time.Since(started).Milliseconds(),
		ModelUsage: map[string]struct {
			ContextWindow int `json:"contextWindow"`
		}{"codex": {ContextWindow: window}},
	}
	if durationMS != nil {
		m.DurationMS = *durationMS
	}
	if status != "completed" {
		m.Subtype = "error_during_execution"
		m.IsError = status == "failed"
	}
	if turnErr != nil {
		m.Errors = []string{turnErr.Message}
	}
	p.emit(m)
}

func (p *Process) assistant(blocks []claude.Block, usage *claude.Usage) {
	if blocks == nil {
		blocks = []claude.Block{}
	}
	content, _ := json.Marshal(blocks)
	p.mu.Lock()
	thread := p.thread
	p.mu.Unlock()
	p.emit(claude.Message{Type: "assistant", SessionID: thread, Message: &claude.APIMessage{Role: "assistant", Content: content, Usage: usage}})
}

func (p *Process) emit(m claude.Message) {
	if p.h.OnMessage != nil {
		p.h.OnMessage(m)
	}
}

func (p *Process) handleRequest(rawID json.RawMessage, method string, raw json.RawMessage) {
	var params struct {
		ItemID      string          `json:"itemId"`
		Reason      string          `json:"reason"`
		Command     string          `json:"command"`
		Cwd         string          `json:"cwd"`
		Permissions json.RawMessage `json:"permissions"`
	}
	_ = json.Unmarshal(raw, &params)
	id := string(rawID)
	var req claude.PermissionRequest
	var answer func(allow bool) any
	switch method {
	case "item/commandExecution/requestApproval":
		req = claude.PermissionRequest{ToolName: "Bash", Input: map[string]any{"command": params.Command, "cwd": params.Cwd}}
		answer = decision
	case "item/fileChange/requestApproval":
		p.mu.Lock()
		call, ok := p.tools[params.ItemID]
		p.mu.Unlock()
		if !ok {
			call = toolCall{"Edit", map[string]any{}}
		}
		req = claude.PermissionRequest{ToolName: call.name, Input: call.input}
		answer = decision
	case "item/permissions/requestApproval":
		var perms map[string]any
		_ = json.Unmarshal(params.Permissions, &perms)
		req = claude.PermissionRequest{ToolName: "Permissions", Input: perms}
		answer = func(allow bool) any {
			if allow {
				return map[string]any{"permissions": params.Permissions, "scope": "turn"}
			}
			return map[string]any{"permissions": map[string]any{}, "scope": "turn"}
		}
	case "mcpServer/elicitation/request":
		p.respond(rawID, map[string]any{"action": "decline", "content": nil, "_meta": nil}, nil)
		return
	default:
		p.respond(rawID, nil, &rpcError{Code: -32601, Message: "layer8 does not support " + method})
		return
	}
	req.RequestID, req.ToolUseID, req.Description = id, params.ItemID, params.Reason
	if p.h.OnPermission == nil {
		p.respond(rawID, answer(false), nil)
		return
	}
	var once sync.Once
	p.h.OnPermission(req, func(d claude.Decision) {
		once.Do(func() { p.respond(rawID, answer(d.Allow), nil) })
	})
}

func decision(allow bool) any {
	if allow {
		return map[string]any{"decision": "accept"}
	}
	return map[string]any{"decision": "decline"}
}

func (p *Process) respond(rawID json.RawMessage, result any, rpcErr *rpcError) {
	id := string(rawID)
	p.mu.Lock()
	if p.answered[id] {
		p.mu.Unlock()
		return
	}
	p.answered[id] = true
	p.mu.Unlock()
	msg := map[string]any{"id": rawID, "result": result}
	if rpcErr != nil {
		msg = map[string]any{"id": rawID, "error": rpcErr}
	}
	_ = p.write(msg)
}
