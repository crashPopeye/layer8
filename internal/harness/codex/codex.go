// Package codex drives the Codex CLI through `codex app-server`, JSON-RPC 2.0 over
// stdio without the "jsonrpc" member.
package codex

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
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
)

// Harness starts `codex app-server` processes. Bin defaults to "codex" looked up on
// Env's PATH, Env defaults to harness.ShellEnv(), and the skills of PluginDirs load
// in every process.
type Harness struct {
	Bin        string
	Env        []string
	PluginDirs []string
}

var _ harness.Harness = Harness{}

// Start spawns codex for one session, then starts or resumes its thread.
// ctx bounds the handshake only; the process lives until Close or exit.
func (h Harness) Start(ctx context.Context, o harness.StartOptions, sink harness.Sink) (harness.Proc, error) {
	p, err := h.spawn(o.Cwd, o.Env, sink)
	if err != nil {
		return nil, err
	}
	o.PluginDirs = slices.Concat(h.PluginDirs, o.PluginDirs)
	if err := p.open(ctx, o); err != nil {
		p.Close(0)
		return nil, err
	}
	go func() {
		<-p.done
		sink.OnExit(p.err)
	}()
	return p, nil
}

// open initializes the connection, loads the skills of o.Cwd and starts the thread.
func (p *proc) open(ctx context.Context, o harness.StartOptions) error {
	if err := p.initialize(ctx); err != nil {
		return err
	}
	skills, err := p.listSkills(ctx, o.Cwd, o.PluginDirs)
	if err != nil {
		return err
	}
	method, params := threadParams(o)
	var r struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
		Model string `json:"model"`
	}
	if err := p.call(ctx, method, params, &r); err != nil {
		return err
	}
	p.thread, p.effort, p.skills = r.Thread.ID, o.Effort, map[string]string{}
	var names []string
	for _, s := range skills {
		p.skills[s.Name] = s.Path
		names = append(names, s.Name)
	}
	p.emit(harness.EventSystem, false, map[string]any{"subtype": "init", "session_id": p.thread, "model": r.Model, "cwd": o.Cwd, "skills": names})
	return nil
}

func (p *proc) initialize(ctx context.Context) error {
	if err := p.call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "layer8", "title": "layer8", "version": "1"}}, nil); err != nil {
		return err
	}
	return p.write(map[string]any{"method": "initialized"})
}

// policies maps a session permission mode to codex's approval policy and sandbox.
var policies = map[string][2]string{
	"":                  {"untrusted", "workspace-write"},
	"default":           {"untrusted", "workspace-write"},
	"plan":              {"untrusted", "read-only"},
	"acceptEdits":       {"on-request", "workspace-write"},
	"auto":              {"on-request", "workspace-write"},
	"bypassPermissions": {"never", "danger-full-access"},
}

func threadParams(o harness.StartOptions) (string, map[string]any) {
	policy, ok := policies[o.PermissionMode]
	if !ok {
		policy = policies[""]
	}
	config := map[string]any{}
	if len(o.AddDirs) > 0 {
		config["sandbox_workspace_write"] = map[string]any{"writable_roots": o.AddDirs}
	}
	if o.MCPURL != "" {
		config["mcp_servers"] = map[string]any{"layer8": map[string]any{"url": o.MCPURL, "http_headers": o.MCPHeaders}}
	}
	params := map[string]any{"cwd": o.Cwd, "approvalPolicy": policy[0], "sandbox": policy[1], "config": config}
	if o.Model != "" {
		params["model"] = o.Model
	}
	if o.AppendSystemPrompt != "" {
		params["developerInstructions"] = o.AppendSystemPrompt
	}
	if o.ResumeID != "" {
		params["threadId"] = o.ResumeID
		return "thread/resume", params
	}
	return "thread/start", params
}

func (h Harness) spawn(cwd string, extraEnv []string, sink harness.Sink) (*proc, error) {
	env := h.Env
	if env == nil {
		env = harness.ShellEnv()
	}
	env = slices.Concat(env, extraEnv)
	bin, err := harness.LookPath(cmp.Or(h.Bin, "codex"), env)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(bin, "app-server")
	cmd.Dir = cwd
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	p := &proc{cmd: cmd, sink: sink, pending: map[int64]chan reply{}, tools: map[string]toolCall{}, done: make(chan struct{})}
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
	emu     sync.Mutex
	mu      sync.Mutex
	pending map[int64]chan reply
	seq     atomic.Int64
	closing atomic.Bool
	done    chan struct{}
	err     error

	thread string
	effort string
	skills map[string]string

	tmu   sync.Mutex
	busy  bool
	turn  string
	queue []harness.UserMessage

	tools map[string]toolCall
	text  string
	usage tokenUsage
}

// Send starts a turn with msg, or queues it until the current turn completes.
func (p *proc) Send(msg harness.UserMessage) error {
	p.tmu.Lock()
	defer p.tmu.Unlock()
	if p.busy {
		p.queue = append(p.queue, msg)
		return nil
	}
	if err := p.startTurn(msg); err != nil {
		return err
	}
	p.busy = true
	return nil
}

// startTurn sends turn/start; a rejected turn ends with an error result.
func (p *proc) startTurn(msg harness.UserMessage) error {
	params := map[string]any{"threadId": p.thread, "input": p.input(msg)}
	if p.effort != "" {
		params["effort"] = p.effort
	}
	ch, err := p.request("turn/start", params)
	if err != nil {
		return err
	}
	go func() {
		if _, err := p.await(context.Background(), ch); err != nil && !p.closing.Load() {
			p.emit(harness.EventError, false, map[string]any{"message": err.Error()})
			p.endTurn(map[string]any{"subtype": "error", "isError": true, "result": err.Error(), "sessionId": p.thread})
		}
	}()
	return nil
}

// endTurn reports a turn's result and starts the next queued message.
func (p *proc) endTurn(result map[string]any) {
	p.emit(harness.EventResult, false, result)
	p.tmu.Lock()
	defer p.tmu.Unlock()
	p.turn = ""
	for len(p.queue) > 0 {
		msg := p.queue[0]
		p.queue = p.queue[1:]
		if p.startTurn(msg) == nil {
			return
		}
	}
	p.busy = false
}

// input converts msg to codex input items. A leading /name or $name naming a known
// skill becomes a skill item.
func (p *proc) input(msg harness.UserMessage) []any {
	text := msg.Text
	if len(msg.Files) > 0 {
		text = strings.TrimSpace(text + "\n\nAttached files:\n- " + strings.Join(msg.Files, "\n- "))
	}
	var skill []any
	if len(text) > 1 && (text[0] == '/' || text[0] == '$') {
		name, _, _ := strings.Cut(text[1:], " ")
		name, _, _ = strings.Cut(name, "\n")
		if path, ok := p.skills[name]; ok {
			text = "$" + text[1:]
			skill = []any{map[string]any{"type": "skill", "name": name, "path": path}}
		}
	}
	var in []any
	if text != "" {
		in = append(in, map[string]any{"type": "text", "text": text})
	}
	for _, img := range msg.Images {
		in = append(in, map[string]any{"type": "image", "url": "data:" + img.MediaType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)})
	}
	return append(in, skill...)
}

// Interrupt stops the current turn and waits for codex to acknowledge it.
func (p *proc) Interrupt(ctx context.Context) error {
	select {
	case <-p.done:
		return cmp.Or(p.err, errors.New("codex exited"))
	default:
	}
	p.tmu.Lock()
	turn := p.turn
	p.tmu.Unlock()
	if turn == "" {
		return nil
	}
	return p.call(ctx, "turn/interrupt", map[string]any{"threadId": p.thread, "turnId": turn}, nil)
}

// Close ends stdin, waits up to grace for codex to exit, then kills its process group.
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

// Done is closed once codex has exited and its output is drained.
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

func (p *proc) request(method string, params any) (<-chan reply, error) {
	id := p.seq.Add(1)
	ch := make(chan reply, 1)
	p.mu.Lock()
	p.pending[id] = ch
	p.mu.Unlock()
	if err := p.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
		return nil, err
	}
	return ch, nil
}

func (p *proc) await(ctx context.Context, ch <-chan reply) (json.RawMessage, error) {
	select {
	case r := <-ch:
		return r.body, r.err
	case <-p.done:
		select {
		case r := <-ch:
			return r.body, r.err
		default:
			return nil, cmp.Or(p.err, errors.New("codex exited"))
		}
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *proc) call(ctx context.Context, method string, params, out any) error {
	ch, err := p.request(method, params)
	if err != nil {
		return err
	}
	body, err := p.await(ctx, ch)
	if err != nil || out == nil {
		return err
	}
	return json.Unmarshal(body, out)
}

// pages calls a paginated list method until it has no next cursor.
func pages[T any](ctx context.Context, p *proc, method string, params map[string]any) ([]T, error) {
	var all []T
	for {
		var r struct {
			Data       []T    `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if err := p.call(ctx, method, params, &r); err != nil {
			return all, err
		}
		all = append(all, r.Data...)
		if r.NextCursor == "" {
			return all, nil
		}
		params["cursor"] = r.NextCursor
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
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
		p.err = fmt.Errorf("codex exited: %w: %s", err, strings.TrimSpace(p.stderr.String()))
	}
	close(p.done)
}

func (p *proc) handle(line []byte) {
	var m message
	if json.Unmarshal(line, &m) != nil {
		return
	}
	switch {
	case m.Method != "" && m.ID != nil:
		p.serverRequest(m.ID, m.Method, m.Params)
	case m.Method != "":
		p.notify(m.Method, m.Params)
	default:
		var id int64
		json.Unmarshal(m.ID, &id)
		p.mu.Lock()
		ch := p.pending[id]
		delete(p.pending, id)
		p.mu.Unlock()
		if ch == nil {
			return
		}
		r := reply{body: m.Result}
		if m.Error != nil {
			r.err = errors.New(m.Error.Message)
		}
		ch <- r
	}
}

func (p *proc) reply(id json.RawMessage, result any, rerr *rpcError) error {
	if rerr != nil {
		return p.write(map[string]any{"id": id, "error": rerr})
	}
	return p.write(map[string]any{"id": id, "result": result})
}

func (p *proc) emit(t harness.EventType, partial bool, data map[string]any) {
	p.emu.Lock()
	defer p.emu.Unlock()
	p.sink.OnEvent(harness.Event{Type: t, Partial: partial, Data: data})
}
