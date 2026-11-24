// Package claude runs one Claude Code CLI process over its stream-json control
// protocol, the wire protocol the Agent SDKs speak.
package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// Options configure one claude process.
type Options struct {
	Binary                                                string
	Cwd, Resume, Model, Effort, PermissionMode, PluginDir string
	Fork                                                  bool
	AddDirs, Env                                          []string
	SystemPromptAppend                                    string
	Hooks                                                 []HookMatcher
	Stderr                                                io.Writer
}

// Handlers receive what the process emits. OnPermission and OnHook run on their
// own goroutine and may block until the user decides; OnMessage runs in stream
// order; OnExit runs once, after the last message.
type Handlers struct {
	OnMessage    func(Message)
	OnPermission func(PermissionRequest) PermissionDecision
	OnHook       func(HookCall) HookResult
	OnExit       func(error)
}

// Process is a running claude CLI.
type Process struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
	h     Handlers

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int
	pending map[string]chan controlResult
	done    chan struct{}
}

type controlResult struct {
	ok  bool
	err string
}

// Spawn starts claude with o and sends initialize with the appended system
// prompt and the hook registrations.
func Spawn(o Options, h Handlers) (*Process, error) {
	bin := o.Binary
	if bin == "" {
		bin = "claude"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("claude CLI not found (install Claude Code and log in): %w", err)
	}
	cmd := exec.Command(path, Args(o)...)
	cmd.Dir = o.Cwd
	cmd.Env = append(append(CleanEnv(os.Environ()), "CLAUDE_CODE_ENTRYPOINT=sdk-ts"), o.Env...)
	cmd.Stderr = o.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &Process{cmd: cmd, stdin: stdin, h: h, pending: map[string]chan controlResult{}, done: make(chan struct{})}
	go p.run(stdout)

	init := map[string]any{"subtype": "initialize", "hooks": hooksPayload(o.Hooks)}
	if o.SystemPromptAppend != "" {
		init["appendSystemPrompt"] = o.SystemPromptAppend
	}
	if _, err := p.request(init); err != nil {
		p.kill()
		return nil, err
	}
	return p, nil
}

func (p *Process) run(stdout io.Reader) {
	r := bufio.NewReaderSize(stdout, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 1 {
			p.handleLine(line)
		}
		if err != nil {
			break
		}
	}
	err := p.cmd.Wait()
	p.mu.Lock()
	for id, ch := range p.pending {
		ch <- controlResult{err: "claude exited"}
		delete(p.pending, id)
	}
	p.pending = nil
	p.mu.Unlock()
	close(p.done)
	if p.h.OnExit != nil {
		p.h.OnExit(err)
	}
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

// Interrupt stops the current turn; the process stays up for the next one.
func (p *Process) Interrupt() error {
	return p.call(map[string]any{"subtype": "interrupt"})
}

// SetModel switches the model for the next turns; "" resets it to the default.
func (p *Process) SetModel(model string) error {
	var m any
	if model != "" {
		m = model
	}
	return p.call(map[string]any{"subtype": "set_model", "model": m})
}

// SetPermissionMode switches how tool calls are approved.
func (p *Process) SetPermissionMode(mode string) error {
	return p.call(map[string]any{"subtype": "set_permission_mode", "mode": mode})
}

// Close ends stdin, waits up to 5 s for the CLI to exit, then kills it.
func (p *Process) Close() error {
	p.writeMu.Lock()
	err := p.stdin.Close()
	p.writeMu.Unlock()
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.kill()
		<-p.done
	}
	return err
}

func (p *Process) kill() {
	if p.cmd.Process != nil {
		p.cmd.Process.Kill()
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
		return fmt.Errorf("claude stdin: %w", err)
	}
	return nil
}

// call sends a control request and waits up to 10 s for its response.
func (p *Process) call(req map[string]any) error {
	ch, err := p.request(req)
	if err != nil {
		return err
	}
	select {
	case r := <-ch:
		if !r.ok {
			return fmt.Errorf("%s: %s", req["subtype"], r.err)
		}
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("%s: no response from claude", req["subtype"])
	}
}

func (p *Process) request(req map[string]any) (chan controlResult, error) {
	ch := make(chan controlResult, 1)
	p.mu.Lock()
	if p.pending == nil {
		p.mu.Unlock()
		return nil, errors.New("claude has exited")
	}
	p.nextID++
	id := "req_" + strconv.Itoa(p.nextID)
	p.pending[id] = ch
	p.mu.Unlock()
	if err := p.write(map[string]any{"type": "control_request", "request_id": id, "request": req}); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, err
	}
	return ch, nil
}

func (p *Process) respond(id string, body any, errMsg string) {
	resp := map[string]any{"subtype": "success", "request_id": id, "response": body}
	if errMsg != "" {
		resp = map[string]any{"subtype": "error", "request_id": id, "error": errMsg}
	}
	p.write(map[string]any{"type": "control_response", "response": resp})
}

func (p *Process) handleLine(line []byte) {
	var head struct {
		Type      string          `json:"type"`
		RequestID string          `json:"request_id"`
		Request   json.RawMessage `json:"request"`
		Response  struct {
			Subtype   string `json:"subtype"`
			RequestID string `json:"request_id"`
			Error     string `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(line, &head) != nil {
		return
	}
	switch head.Type {
	case "control_response":
		p.mu.Lock()
		ch, ok := p.pending[head.Response.RequestID]
		delete(p.pending, head.Response.RequestID)
		p.mu.Unlock()
		if ok {
			ch <- controlResult{ok: head.Response.Subtype == "success", err: head.Response.Error}
		}
	case "control_request":
		p.handleControlRequest(head.RequestID, head.Request)
	case "control_cancel_request":
	default:
		var m Message
		if json.Unmarshal(line, &m) != nil {
			return
		}
		m.Raw = line
		if p.h.OnMessage != nil {
			p.h.OnMessage(m)
		}
	}
}

func (p *Process) handleControlRequest(id string, raw json.RawMessage) {
	var sub struct {
		Subtype    string    `json:"subtype"`
		CallbackID string    `json:"callback_id"`
		ToolUseID  string    `json:"tool_use_id"`
		Input      HookInput `json:"input"`
	}
	json.Unmarshal(raw, &sub)
	switch sub.Subtype {
	case "can_use_tool":
		var req PermissionRequest
		if err := json.Unmarshal(raw, &req); err != nil || p.h.OnPermission == nil {
			p.respond(id, map[string]any{"behavior": "deny", "message": "layer8 cannot answer this request"}, "")
			return
		}
		req.RequestID = id
		go func() { p.respond(id, p.h.OnPermission(req).response(req), "") }()
	case "hook_callback":
		call := HookCall{CallbackID: sub.CallbackID, ToolUseID: sub.ToolUseID, Input: sub.Input}
		go func() {
			var r HookResult
			if p.h.OnHook != nil {
				r = p.h.OnHook(call)
			}
			p.respond(id, r.output(call.Input.HookEventName), "")
		}()
	default:
		p.respond(id, nil, "unsupported control request: "+sub.Subtype)
	}
}
