// Package claude drives the Claude Code CLI over its stream-json control protocol,
// the same wire protocol the official TypeScript Agent SDK speaks (verified
// against SDK 0.3.312 / CLI 2.1.312):
//
//	claude --output-format stream-json --verbose --input-format stream-json
//	       --permission-prompt-tool=stdio [--resume=ID] --setting-sources=user,project,local
//	       --permission-mode=default --add-dir=DIR...
//
// stdin carries one JSON object per line: an initialize control request (which
// also carries the appended system prompt), then user messages and interrupts.
// stdout carries system/assistant/user/result messages and control requests
// (can_use_tool) that must each get a control_response.
package claude

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type Options struct {
	Binary             string // defaults to "claude" on PATH
	Cwd                string
	Resume             string
	AddDirs            []string
	AppendSystemPrompt string
	Model              string
	Env                []string // extra KEY=VALUE pairs
}

// PermissionRequest is a can_use_tool control request.
type PermissionRequest struct {
	RequestID   string         `json:"-"`
	ToolName    string         `json:"tool_name"`
	Input       map[string]any `json:"input"`
	Description string         `json:"description"`
	BlockedPath string         `json:"blocked_path"`
	ToolUseID   string         `json:"tool_use_id"`
	Reason      string         `json:"decision_reason"`
}

type Decision struct {
	Allow   bool
	Message string // shown to the model when denied
}

// Message is one stdout line; Raw keeps everything for fields we don't model.
type Message struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	SessionID       string          `json:"session_id"`
	ParentToolUseID *string         `json:"parent_tool_use_id"`
	Message         *APIMessage     `json:"message"`
	State           string          `json:"state"` // system/session_state_changed
	IsError         bool            `json:"is_error"`
	Result          string          `json:"result"`
	DurationMS      int64           `json:"duration_ms"`
	NumTurns        int             `json:"num_turns"`
	TotalCostUSD    float64         `json:"total_cost_usd"`
	Errors          []string        `json:"errors"`
	Raw             json.RawMessage `json:"-"`
}

type APIMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string or []Block
}

type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     map[string]any  `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// Blocks decodes the content array; a plain string becomes one text block.
func (m *APIMessage) Blocks() []Block {
	if m == nil || len(m.Content) == 0 {
		return nil
	}
	var blocks []Block
	if json.Unmarshal(m.Content, &blocks) == nil {
		return blocks
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []Block{{Type: "text", Text: s}}
	}
	return nil
}

// ResultText flattens a tool_result's content (string or blocks) to text.
func (b Block) ResultText() string {
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var parts []Block
	if json.Unmarshal(b.Content, &parts) != nil {
		return ""
	}
	var out []string
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		} else {
			out = append(out, "["+p.Type+"]")
		}
	}
	return strings.Join(out, "\n")
}

// Handlers receive everything the process emits. OnPermission must eventually
// call respond exactly once; it may do so from another goroutine (the user decides).
type Handlers struct {
	OnMessage    func(Message)
	OnPermission func(req PermissionRequest, respond func(Decision))
	// OnPermissionCancelled fires when the CLI withdraws a pending request.
	OnPermissionCancelled func(requestID string)
	OnStderr              func(line string)
}

type Process struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	h     Handlers

	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[string]chan controlResult // our requests awaiting the CLI's response
	answered map[string]bool               // CLI requests we already answered
	closed   bool

	done chan struct{}
	err  error
}

type controlResult struct {
	ok   bool
	body json.RawMessage
	err  string
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// inheritedSessionVars identify a parent Claude Code session. When layer8 is
// started from inside one (e.g. `open` run by an agent), passing them on would
// make every agent join or report as that parent session.
var inheritedSessionVars = []string{
	"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_PID",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_REMOTE_SESSION_ID", "CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_AFTER_LAST_COMPACT",
	"CLAUDE_ADDITIONAL_DIRECTORIES", "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD", "CLAUDE_CODE_TEE_SDK_STDOUT",
	"CLAUDE_CODE_INCLUDE_PARTIAL_MESSAGES",
	// Same as the SDK: never leak Node debugging flags into the CLI.
	"NODE_OPTIONS", "DEBUG",
}

// CleanEnv drops variables that would tie a spawned CLI to a parent session.
func CleanEnv(in []string) []string {
	out := make([]string, 0, len(in))
outer:
	for _, kv := range in {
		for _, k := range inheritedSessionVars {
			if strings.HasPrefix(kv, k+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// Args is exported for tests: the exact argv after the binary.
func Args(o Options) []string {
	args := []string{"--output-format", "stream-json", "--verbose", "--input-format", "stream-json", "--permission-prompt-tool=stdio"}
	if o.Resume != "" {
		args = append(args, "--resume="+o.Resume)
	}
	if o.Model != "" {
		args = append(args, "--model="+o.Model)
	}
	// The user's own settings, CLAUDE.md, skills and permission rules apply.
	args = append(args, "--setting-sources=user,project,local", "--permission-mode=default")
	for _, d := range o.AddDirs {
		args = append(args, "--add-dir="+d)
	}
	return args
}

// Start spawns the CLI and sends initialize. The process stays alive across turns
// until Close (stdin EOF) or Kill.
func Start(o Options, h Handlers) (*Process, error) {
	bin := o.Binary
	if bin == "" {
		bin = "claude"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, fmt.Errorf("claude CLI not found on PATH (install Claude Code and log in): %w", err)
	}
	cmd := exec.Command(bin, Args(o)...)
	cmd.Dir = o.Cwd
	env := CleanEnv(os.Environ())
	// layer8 speaks the TypeScript SDK's control protocol, so it identifies as that host.
	env = append(env, "CLAUDE_CODE_ENTRYPOINT=sdk-ts", "CLAUDE_CODE_SDK_READS_SESSION_STATE=1")
	cmd.Env = append(env, o.Env...)

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
	p := &Process{cmd: cmd, stdin: stdin, h: h, pending: map[string]chan controlResult{}, answered: map[string]bool{}, done: make(chan struct{})}

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
			ch <- controlResult{err: "process exited"}
			delete(p.pending, id)
		}
		p.mu.Unlock()
		close(p.done)
	}()

	init := map[string]any{"subtype": "initialize"}
	if o.AppendSystemPrompt != "" {
		init["appendSystemPrompt"] = o.AppendSystemPrompt
	}
	// Like the SDK, don't wait for the initialize response before sending the first message.
	if _, err := p.request(init, false); err != nil {
		p.Kill()
		return nil, err
	}
	return p, nil
}

// Done is closed when the process has exited; Err then reports how.
func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Err() error            { return p.err }

func (p *Process) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	if _, err := p.stdin.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("claude stdin: %w", err)
	}
	return nil
}

// Send starts a turn with a user message.
func (p *Process) Send(text string) error {
	return p.write(map[string]any{
		"type":               "user",
		"message":            map[string]any{"role": "user", "content": []map[string]string{{"type": "text", "text": text}}},
		"parent_tool_use_id": nil,
		"session_id":         "",
	})
}

// Interrupt stops the current turn; the process stays alive for the next one.
func (p *Process) Interrupt(ctx context.Context) error {
	ch, err := p.request(map[string]any{"subtype": "interrupt"}, true)
	if err != nil {
		return err
	}
	select {
	case r := <-ch:
		if !r.ok {
			return fmt.Errorf("interrupt: %s", r.err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Process) request(req map[string]any, wait bool) (chan controlResult, error) {
	id := newID()
	var ch chan controlResult
	if wait {
		ch = make(chan controlResult, 1)
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, errors.New("claude process has exited")
		}
		p.pending[id] = ch
		p.mu.Unlock()
	}
	if err := p.write(map[string]any{"request_id": id, "type": "control_request", "request": req}); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, err
	}
	return ch, nil
}

// Close ends stdin; the CLI exits once in-flight work (including background tasks) finishes.
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

func (p *Process) respond(id string, body map[string]any, errMsg string) {
	p.mu.Lock()
	if p.answered[id] {
		p.mu.Unlock()
		return
	}
	p.answered[id] = true
	p.mu.Unlock()
	resp := map[string]any{"subtype": "success", "request_id": id, "response": body}
	if errMsg != "" {
		resp = map[string]any{"subtype": "error", "request_id": id, "error": errMsg}
	}
	_ = p.write(map[string]any{"type": "control_response", "response": resp})
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
	var head struct {
		Type      string          `json:"type"`
		RequestID string          `json:"request_id"`
		Request   json.RawMessage `json:"request"`
		Response  struct {
			Subtype   string          `json:"subtype"`
			RequestID string          `json:"request_id"`
			Response  json.RawMessage `json:"response"`
			Error     string          `json:"error"`
		} `json:"response"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		if p.h.OnStderr != nil {
			p.h.OnStderr("unparsable stdout line: " + string(line))
		}
		return
	}
	switch head.Type {
	case "control_response":
		p.mu.Lock()
		ch, ok := p.pending[head.Response.RequestID]
		delete(p.pending, head.Response.RequestID)
		p.mu.Unlock()
		if ok {
			ch <- controlResult{ok: head.Response.Subtype == "success", body: head.Response.Response, err: head.Response.Error}
		}
	case "control_request":
		p.handleControlRequest(head.RequestID, head.Request)
	case "control_cancel_request":
		p.mu.Lock()
		p.answered[head.RequestID] = true
		p.mu.Unlock()
		if p.h.OnPermissionCancelled != nil {
			p.h.OnPermissionCancelled(head.RequestID)
		}
	default:
		var m Message
		if err := json.Unmarshal(line, &m); err != nil {
			return
		}
		m.Raw = append(json.RawMessage(nil), line...)
		if p.h.OnMessage != nil {
			p.h.OnMessage(m)
		}
	}
}

func (p *Process) handleControlRequest(id string, raw json.RawMessage) {
	var sub struct {
		Subtype string `json:"subtype"`
	}
	_ = json.Unmarshal(raw, &sub)
	switch sub.Subtype {
	case "can_use_tool":
		var req PermissionRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			p.respond(id, nil, "invalid can_use_tool request")
			return
		}
		req.RequestID = id
		if p.h.OnPermission == nil {
			p.respond(id, map[string]any{"behavior": "deny", "message": "No approval handler", "toolUseID": req.ToolUseID}, "")
			return
		}
		var once sync.Once
		p.h.OnPermission(req, func(d Decision) {
			once.Do(func() {
				body := map[string]any{"toolUseID": req.ToolUseID}
				if d.Allow {
					body["behavior"] = "allow"
					body["updatedInput"] = req.Input
				} else {
					msg := d.Message
					if msg == "" {
						msg = "The user denied this action."
					}
					body["behavior"] = "deny"
					body["message"] = msg
					body["interrupt"] = false
				}
				p.respond(id, body, "")
			})
		})
	case "elicitation":
		p.respond(id, map[string]any{"action": "decline"}, "")
	case "request_user_dialog":
		// The SDK deliberately never answers these; neither do we.
	default:
		p.respond(id, nil, "Unsupported control request subtype: "+sub.Subtype)
	}
}
