// Package claude drives the Claude Code CLI over its stream-json control protocol.
package claude

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
)

// Harness starts `claude` processes. Bin defaults to "claude" looked up on Env's
// PATH, Env defaults to harness.ShellEnv(), and PluginDirs load in every process.
type Harness struct {
	Bin        string
	Env        []string
	PluginDirs []string
}

var _ harness.Harness = Harness{}

// Start spawns claude for one session and completes the initialize handshake.
// ctx bounds the handshake only; the process lives until Close or exit.
func (h Harness) Start(ctx context.Context, o harness.StartOptions, sink harness.Sink) (harness.Proc, error) {
	p, err := h.spawn(o.Cwd, h.args(o), o.Env, sink)
	if err != nil {
		return nil, err
	}
	init := map[string]any{"hooks": nil}
	if o.AppendSystemPrompt != "" {
		init["appendSystemPrompt"] = o.AppendSystemPrompt
	}
	if _, err := p.request(ctx, "initialize", init); err != nil {
		p.Close(0)
		return nil, err
	}
	go func() {
		<-p.done
		sink.OnExit(p.err)
	}()
	return p, nil
}

func (h Harness) args(o harness.StartOptions) []string {
	a := []string{"--output-format", "stream-json", "--input-format", "stream-json", "--verbose",
		"--include-partial-messages", "--replay-user-messages", "--permission-prompt-tool=stdio", "--setting-sources=user,project,local"}
	for _, d := range slices.Concat(h.PluginDirs, o.PluginDirs) {
		a = append(a, "--plugin-dir", d)
	}
	for _, d := range o.AddDirs {
		a = append(a, "--add-dir", d)
	}
	if o.MCPURL != "" {
		type server struct {
			Type    string            `json:"type"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers,omitempty"`
		}
		cfg, _ := json.Marshal(map[string]any{"mcpServers": map[string]server{"layer8": {"http", o.MCPURL, o.MCPHeaders}}})
		a = append(a, "--mcp-config", string(cfg))
	}
	if len(o.AllowedTools) > 0 {
		a = append(a, "--allowedTools", strings.Join(o.AllowedTools, ","))
	}
	for _, f := range [][2]string{{"--resume", o.ResumeID}, {"--model", o.Model}, {"--effort", o.Effort}, {"--permission-mode", o.PermissionMode}} {
		if f[1] != "" {
			a = append(a, f[0], f[1])
		}
	}
	if o.Fork && o.ResumeID != "" {
		a = append(a, "--fork-session")
	}
	return a
}

func (h Harness) spawn(cwd string, args, extraEnv []string, sink harness.Sink) (*proc, error) {
	env := h.Env
	if env == nil {
		env = harness.ShellEnv()
	}
	env = slices.Concat(env, extraEnv)
	bin, err := harness.LookPath(cmp.Or(h.Bin, "claude"), env)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = cwd
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p := &proc{cmd: cmd, sink: sink, pending: map[string]chan reply{}, sent: map[string]harness.UserMessage{}, done: make(chan struct{})}
	cmd.Stderr = &p.stderr
	if p.stdin, err = cmd.StdinPipe(); err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go p.read(stdout)
	return p, nil
}

type reply struct {
	body json.RawMessage
	err  error
}

type proc struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	sink    harness.Sink
	stderr  harness.Tail
	wmu     sync.Mutex
	mu      sync.Mutex
	pending map[string]chan reply
	sent    map[string]harness.UserMessage
	seq     atomic.Int64
	closing atomic.Bool
	context int
	done    chan struct{}
	err     error
}

// Send writes one user turn to claude's stdin; claude queues it while a turn runs.
func (p *proc) Send(msg harness.UserMessage) error {
	if msg.ID != "" {
		p.mu.Lock()
		p.sent[msg.ID] = msg
		p.mu.Unlock()
	}
	return p.write(userLine(msg, ""))
}

// SendNow takes a queued message back from claude and sends it again with priority
// "now", which cuts into the running turn. A message claude already took is left alone.
func (p *proc) SendNow(ctx context.Context, id string) error {
	p.mu.Lock()
	msg, ok := p.sent[id]
	p.mu.Unlock()
	if !ok {
		return nil
	}
	body, err := p.request(ctx, "cancel_async_message", map[string]any{"message_uuid": id})
	if err != nil {
		return err
	}
	var r struct{ Cancelled bool }
	json.Unmarshal(body, &r)
	if !r.Cancelled {
		return nil
	}
	return p.write(userLine(msg, "now"))
}

func userLine(msg harness.UserMessage, priority string) map[string]any {
	text := msg.Text
	if len(msg.Files) > 0 {
		text = strings.TrimSpace(text + "\n\nAttached files:\n- " + strings.Join(msg.Files, "\n- "))
	}
	var content []any
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	for _, img := range msg.Images {
		content = append(content, map[string]any{"type": "image", "source": map[string]any{
			"type": "base64", "media_type": img.MediaType, "data": base64.StdEncoding.EncodeToString(img.Data)}})
	}
	line := map[string]any{"type": "user", "parent_tool_use_id": nil, "session_id": "",
		"message": map[string]any{"role": "user", "content": content}}
	if msg.ID != "" {
		line["uuid"] = msg.ID
	}
	if priority != "" {
		line["priority"] = priority
	}
	return line
}

// Interrupt stops the current turn and waits for claude to acknowledge it.
func (p *proc) Interrupt(ctx context.Context) error {
	_, err := p.request(ctx, "interrupt", nil)
	return err
}

// Close ends stdin, waits up to grace for claude to exit, then kills its process group.
func (p *proc) Close(grace time.Duration) {
	p.closing.Store(true)
	p.stdin.Close()
	select {
	case <-p.done:
	case <-time.After(grace):
		syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
	}
}

// Done is closed once claude has exited and its output is drained.
func (p *proc) Done() <-chan struct{} { return p.done }

func (p *proc) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	_, err = p.stdin.Write(append(b, '\n'))
	return err
}

func (p *proc) request(ctx context.Context, subtype string, fields map[string]any) (json.RawMessage, error) {
	id := fmt.Sprintf("layer8-%d", p.seq.Add(1))
	ch := make(chan reply, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}()
	req := map[string]any{"subtype": subtype}
	maps.Copy(req, fields)
	if err := p.write(map[string]any{"type": "control_request", "request_id": id, "request": req}); err != nil {
		return nil, err
	}
	select {
	case r := <-ch:
		return r.body, r.err
	case <-p.done:
		select {
		case r := <-ch:
			return r.body, r.err
		default:
			return nil, cmp.Or(p.err, errors.New("claude exited"))
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *proc) respond(id string, body any, errMsg string) error {
	r := map[string]any{"subtype": "success", "request_id": id, "response": body}
	if errMsg != "" {
		r = map[string]any{"subtype": "error", "request_id": id, "error": errMsg}
	}
	return p.write(map[string]any{"type": "control_response", "response": r})
}

func (p *proc) read(r io.Reader) {
	br := bufio.NewReaderSize(r, 1<<16)
	for {
		line, err := br.ReadBytes('\n')
		if line = bytes.TrimSpace(line); len(line) > 0 && line[0] == '{' {
			p.handle(line)
		}
		if err != nil {
			break
		}
	}
	if err := p.cmd.Wait(); err != nil && !p.closing.Load() {
		p.err = fmt.Errorf("claude exited: %w: %s", err, strings.TrimSpace(p.stderr.String()))
	}
	close(p.done)
}

func (p *proc) emit(t harness.EventType, partial bool, data map[string]any, parent *string) {
	if parent != nil {
		data["parentToolUseId"] = *parent
	}
	p.sink.OnEvent(harness.Event{Type: t, Partial: partial, Data: data})
}

type usage struct {
	Input         int `json:"input_tokens"`
	CacheCreation int `json:"cache_creation_input_tokens"`
	CacheRead     int `json:"cache_read_input_tokens"`
	Output        int `json:"output_tokens"`
}

func (u usage) tokens() harness.Tokens {
	return harness.Tokens{Input: u.Input, Output: u.Output, CacheRead: u.CacheRead, CacheWrite: u.CacheCreation}
}

type window struct {
	Utilization *float64 `json:"utilization"`
	ResetsAt    int64    `json:"resetsAt"`
	Status      string   `json:"status"`
}

type rateLimitInfo struct {
	window
	Type    string            `json:"rateLimitType"`
	Windows map[string]window `json:"unifiedWindows"`
}

var windowNames = map[string]string{"five_hour": "5h", "seven_day": "weekly", "seven_day_opus": "weekly opus", "seven_day_sonnet": "weekly sonnet"}

// limits returns the account windows the event reports, all of them when it has unified windows.
func (r rateLimitInfo) limits() []harness.Limit {
	windows := r.Windows
	if len(windows) == 0 {
		windows = map[string]window{r.Type: r.window}
	}
	var out []harness.Limit
	for key, w := range windows {
		name, known := windowNames[key]
		if !known || w.Utilization == nil && w.Status != "rejected" {
			continue
		}
		used := 100.0
		if w.Utilization != nil {
			used = *w.Utilization * 100
		}
		out = append(out, harness.Limit{Window: name, UsedPercent: used, ResetsAt: w.ResetsAt})
	}
	slices.SortFunc(out, func(a, b harness.Limit) int { return cmp.Compare(a.Window, b.Window) })
	return out
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

type message struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype"`
	RequestID string          `json:"request_id"`
	Parent    *string         `json:"parent_tool_use_id"`
	Request   json.RawMessage `json:"request"`
	Response  struct {
		Subtype   string          `json:"subtype"`
		RequestID string          `json:"request_id"`
		Response  json.RawMessage `json:"response"`
		Error     string          `json:"error"`
	} `json:"response"`
	Event struct {
		Type  string `json:"type"`
		Delta struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Thinking string `json:"thinking"`
		} `json:"delta"`
	} `json:"event"`
	Message struct {
		Content json.RawMessage `json:"content"`
		Usage   *usage          `json:"usage"`
	} `json:"message"`
	IsError    bool            `json:"is_error"`
	Result     string          `json:"result"`
	SessionID  string          `json:"session_id"`
	CostUSD    float64         `json:"total_cost_usd"`
	DurationMS int             `json:"duration_ms"`
	NumTurns   int             `json:"num_turns"`
	Usage      json.RawMessage `json:"usage"`
	ModelUsage map[string]struct {
		ContextWindow int `json:"contextWindow"`
	} `json:"modelUsage"`
	RateLimit rateLimitInfo `json:"rate_limit_info"`
	UUID      string        `json:"uuid"`
	IsReplay  bool          `json:"isReplay"`
}

var liveSystem = map[string]bool{"status": true, "thinking_tokens": true, "hook_started": true, "hook_progress": true, "hook_response": true}

func (p *proc) handle(line []byte) {
	var m message
	if json.Unmarshal(line, &m) != nil {
		return
	}
	switch m.Type {
	case "control_response":
		p.mu.Lock()
		ch := p.pending[m.Response.RequestID]
		p.mu.Unlock()
		if ch != nil {
			r := reply{body: m.Response.Response}
			if m.Response.Subtype == "error" {
				r.err = errors.New(m.Response.Error)
			}
			ch <- r
		}
	case "control_request":
		p.controlRequest(m.RequestID, m.Request)
	case "stream_event":
		if m.Event.Type != "content_block_delta" {
			return
		}
		switch d := m.Event.Delta; d.Type {
		case "text_delta":
			p.emit(harness.EventText, true, map[string]any{"text": d.Text}, m.Parent)
		case "thinking_delta":
			p.emit(harness.EventThinking, true, map[string]any{"text": d.Thinking}, m.Parent)
		}
	case "assistant":
		if u := m.Message.Usage; u != nil && m.Parent == nil {
			p.context = u.Input + u.CacheCreation + u.CacheRead + u.Output
		}
		for _, b := range blocks(m.Message.Content) {
			switch {
			case b.Type == "text" && b.Text != "":
				p.emit(harness.EventText, false, map[string]any{"text": b.Text}, m.Parent)
			case b.Type == "thinking" && b.Thinking != "":
				p.emit(harness.EventThinking, false, map[string]any{"text": b.Thinking}, m.Parent)
			case b.Type == "tool_use":
				p.emit(harness.EventToolUse, false, map[string]any{"id": b.ID, "name": b.Name, "input": b.Input}, m.Parent)
			}
		}
	case "user":
		if m.IsReplay {
			p.delivered(m.UUID)
			return
		}
		for _, b := range blocks(m.Message.Content) {
			if b.Type == "tool_result" {
				p.emit(harness.EventToolResult, false, map[string]any{"toolUseId": b.ToolUseID, "content": b.Content, "isError": b.IsError}, m.Parent)
			}
		}
	case "result":
		var u usage
		json.Unmarshal(m.Usage, &u)
		contextWindow := 0
		for _, mu := range m.ModelUsage {
			contextWindow = max(contextWindow, mu.ContextWindow)
		}
		p.emit(harness.EventResult, false, map[string]any{"subtype": m.Subtype, "isError": m.IsError, "result": m.Result,
			"sessionId": m.SessionID, "costUsd": m.CostUSD, "durationMs": m.DurationMS, "numTurns": m.NumTurns,
			"usage": m.Usage, "tokens": u.tokens(), "contextTokens": p.context, "contextWindow": contextWindow}, nil)
	case "rate_limit_event":
		if limits := m.RateLimit.limits(); len(limits) > 0 {
			p.emit(harness.EventLimits, false, map[string]any{"limits": limits}, nil)
		}
	case "system":
		var data map[string]any
		json.Unmarshal(line, &data)
		p.emit(harness.EventSystem, liveSystem[m.Subtype], data, nil)
	}
}

func (p *proc) delivered(id string) {
	p.mu.Lock()
	_, ok := p.sent[id]
	delete(p.sent, id)
	p.mu.Unlock()
	if ok {
		p.emit(harness.EventDelivered, false, map[string]any{"id": id}, nil)
	}
}

func blocks(content json.RawMessage) []block {
	var bs []block
	json.Unmarshal(content, &bs)
	return bs
}

func (p *proc) controlRequest(id string, raw json.RawMessage) {
	var r struct {
		Subtype     string          `json:"subtype"`
		ToolName    string          `json:"tool_name"`
		Input       json.RawMessage `json:"input"`
		ToolUseID   string          `json:"tool_use_id"`
		Description string          `json:"description"`
		Suggestions json.RawMessage `json:"permission_suggestions"`
	}
	json.Unmarshal(raw, &r)
	if r.Subtype != "can_use_tool" {
		p.respond(id, nil, "layer8 does not handle control request "+r.Subtype)
		return
	}
	req := harness.PermissionRequest{ID: id, Tool: r.ToolName, Input: r.Input, ToolUseID: r.ToolUseID,
		Description: r.Description, Suggestions: r.Suggestions}
	var once sync.Once
	p.sink.OnPermission(req, func(d harness.Decision) {
		once.Do(func() { p.respond(id, permissionResult(req, d), "") })
	})
}

func permissionResult(req harness.PermissionRequest, d harness.Decision) map[string]any {
	if !d.Allow {
		return map[string]any{"behavior": "deny", "message": cmp.Or(d.Message, "The user denied this tool call.")}
	}
	input := req.Input
	if d.UpdatedInput != nil {
		input = d.UpdatedInput
	}
	r := map[string]any{"behavior": "allow", "updatedInput": input}
	if d.Always {
		r["updatedPermissions"] = alwaysRules(req)
	}
	return r
}

func alwaysRules(req harness.PermissionRequest) []any {
	var suggestions []map[string]any
	json.Unmarshal(req.Suggestions, &suggestions)
	var rules []any
	for _, s := range suggestions {
		if s["type"] == "addRules" {
			rules = append(rules, s)
		}
	}
	if len(rules) == 0 {
		rules = []any{map[string]any{"type": "addRules", "behavior": "allow", "destination": "session",
			"rules": []any{map[string]any{"toolName": req.Tool}}}}
	}
	return rules
}
