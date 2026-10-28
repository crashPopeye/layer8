package sessions

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/store"
)

type nopBus struct{}

func (nopBus) Changed()          {}
func (nopBus) Event(store.Event) {}

// fakeProc plays a scripted claude: each Send runs script(turn, handlers).
type fakeProc struct {
	h      claude.Handlers
	opts   claude.Options
	script func(p *fakeProc, n int, text string)
	turns  int
	done   chan struct{}
	once   sync.Once
	intr   chan struct{}
}

func (p *fakeProc) Send(text string) error {
	p.turns++
	n := p.turns
	go p.script(p, n, text)
	return nil
}
func (p *fakeProc) Interrupt(ctx context.Context) error {
	close(p.intr)
	return nil
}
func (p *fakeProc) Shutdown(time.Duration) { p.once.Do(func() { close(p.done) }) }
func (p *fakeProc) Done() <-chan struct{}  { return p.done }

func (p *fakeProc) msg(m claude.Message) { p.h.OnMessage(m) }
func (p *fakeProc) result(ok bool) {
	sub := "success"
	if !ok {
		sub = "error_during_execution"
	}
	p.msg(claude.Message{Type: "result", Subtype: sub, IsError: !ok, TotalCostUSD: 0.1 * float64(p.turns)})
}

type harness struct {
	t     *testing.T
	st    *store.Store
	m     *Manager
	mu    sync.Mutex
	procs []*fakeProc
	task  store.Task
}

func newHarness(t *testing.T, max int, script func(p *fakeProc, n int, text string)) *harness {
	st, err := store.Open(filepath.Join(t.TempDir(), "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	w, _ := st.InsertWorkspace(store.Workspace{Name: "w", Path: "/w"})
	task, _ := st.InsertTask(store.Task{WorkspaceID: w.ID, Slug: "t", Title: "t", WorktreePath: "/w", ArtifactsPath: "/a", Status: "active"})
	h := &harness{t: t, st: st, task: task}
	start := func(o claude.Options, hd claude.Handlers) (Proc, error) {
		p := &fakeProc{h: hd, opts: o, script: script, done: make(chan struct{}), intr: make(chan struct{})}
		h.mu.Lock()
		h.procs = append(h.procs, p)
		h.mu.Unlock()
		return p, nil
	}
	h.m = New(st, nopBus{}, start, func() int { return max }, func(s store.Session) (Launch, error) {
		return Launch{Cwd: s.Cwd, AddDirs: []string{"/a"}, AppendSystemPrompt: "ctx"}, nil
	})
	return h
}

func (h *harness) until(cond func() bool) {
	h.t.Helper()
	for i := 0; i < 300; i++ {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Fatal("condition not reached")
}

func (h *harness) status(id string) string {
	s, _ := h.st.Session(id)
	return s.Status
}

func TestApprovalFlowAndResumeAfterIdle(t *testing.T) {
	h := newHarness(t, 2, func(p *fakeProc, n int, text string) {
		p.msg(claude.Message{Type: "system", Subtype: "init", SessionID: "claude-1"})
		decided := make(chan claude.Decision, 1)
		p.h.OnPermission(claude.PermissionRequest{RequestID: "r", ToolName: "Bash", Input: map[string]any{"command": "ls"}},
			func(d claude.Decision) { decided <- d })
		d := <-decided
		text2 := "denied"
		if d.Allow {
			text2 = "allowed"
		}
		p.msg(claude.Message{Type: "assistant", Message: &claude.APIMessage{Content: []byte(`[{"type":"text","text":"` + text2 + `"}]`)}})
		p.result(true)
	})
	h.m.IdleTimeout = 20 * time.Millisecond
	s, _ := h.m.Create(store.Session{TaskID: h.task.ID, Kind: "chat", Title: "c", Cwd: "/w"}, "go")
	h.until(func() bool { return h.status(s.ID) == "waiting_approval" })
	ap, _ := h.st.PendingApprovals()
	if len(ap) != 1 {
		t.Fatal("expected one approval")
	}
	if err := h.m.Send(s.ID, "again"); err != ErrBusy {
		t.Fatalf("send while busy: %v", err)
	}
	h.m.Decide(ap[0].ID, true, "")
	h.until(func() bool { return h.status(s.ID) == "idle" })
	got, _ := h.st.Session(s.ID)
	if got.ClaudeSessionID != "claude-1" || got.CostUSD != 0.1 {
		t.Fatalf("session %+v", got)
	}
	if h.procs[0].opts.Resume != "" || h.procs[0].opts.AppendSystemPrompt != "ctx" {
		t.Fatalf("first launch %+v", h.procs[0].opts)
	}

	// Idle timeout closes the process; the next message resumes the Claude session.
	h.until(func() bool {
		select {
		case <-h.procs[0].done:
			return true
		default:
			return false
		}
	})
	time.Sleep(20 * time.Millisecond)
	if err := h.m.Send(s.ID, "next"); err != nil {
		t.Fatal(err)
	}
	h.until(func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.procs) == 2 })
	if h.procs[1].opts.Resume != "claude-1" {
		t.Fatalf("resume = %q", h.procs[1].opts.Resume)
	}
	h.until(func() bool { return h.status(s.ID) == "waiting_approval" })
	ap, _ = h.st.PendingApprovals()
	h.m.Decide(ap[0].ID, false, "no")
	h.until(func() bool { return h.status(s.ID) == "idle" })
	evs, _ := h.st.Events(s.ID, 0)
	last := ""
	for _, e := range evs {
		if e.Type == "text" {
			last = e.Data["text"].(string)
		}
	}
	if last != "denied" {
		t.Fatalf("last text %q", last)
	}
}

func TestQueueRespectsLimit(t *testing.T) {
	gates := make(chan chan struct{}, 4)
	h := newHarness(t, 1, func(p *fakeProc, n int, text string) {
		g := make(chan struct{})
		gates <- g
		<-g
		p.result(true)
	})
	a, _ := h.m.Create(store.Session{TaskID: h.task.ID, Kind: "chat", Title: "a", Cwd: "/w"}, "a")
	b, _ := h.m.Create(store.Session{TaskID: h.task.ID, Kind: "chat", Title: "b", Cwd: "/w"}, "b")
	g1 := <-gates
	if h.status(a.ID) != "running" || h.status(b.ID) != "queued" {
		t.Fatalf("a=%s b=%s", h.status(a.ID), h.status(b.ID))
	}
	if r, q := h.m.Counts(); r != 1 || q != 1 {
		t.Fatalf("counts %d %d", r, q)
	}
	close(g1)
	g2 := <-gates
	h.until(func() bool { return h.status(b.ID) == "running" })
	close(g2)
	h.until(func() bool { return h.status(b.ID) == "idle" })
}

func TestInterruptAndUnexpectedExit(t *testing.T) {
	h := newHarness(t, 2, func(p *fakeProc, n int, text string) {
		if text == "crash" {
			p.Shutdown(0)
			return
		}
		<-p.intr
		p.result(false)
	})
	turnEnds := make(chan bool, 4)
	h.m.OnTurnEnd = func(s store.Session, ok bool) { turnEnds <- ok }
	s, _ := h.m.Create(store.Session{TaskID: h.task.ID, Kind: "chat", Title: "c", Cwd: "/w"}, "work")
	h.until(func() bool { return h.status(s.ID) == "running" })
	if err := h.m.Interrupt(s.ID); err != nil {
		t.Fatal(err)
	}
	h.until(func() bool { return h.status(s.ID) == "interrupted" })
	if ok := <-turnEnds; ok {
		t.Fatal("interrupted turn reported ok")
	}

	if err := h.m.Send(s.ID, "crash"); err != nil {
		t.Fatal(err)
	}
	h.until(func() bool { return h.status(s.ID) == "failed" })
	got, _ := h.st.Session(s.ID)
	if got.LastError == "" {
		t.Fatal("expected an error message")
	}
}

func TestAutonomousTurnCountsAsRunning(t *testing.T) {
	var proc *fakeProc
	h := newHarness(t, 2, func(p *fakeProc, n int, text string) {
		proc = p
		p.result(true)
	})
	s, _ := h.m.Create(store.Session{TaskID: h.task.ID, Kind: "chat", Title: "c", Cwd: "/w"}, "go")
	h.until(func() bool { return h.status(s.ID) == "idle" })
	proc.msg(claude.Message{Type: "system", Subtype: "init", SessionID: "x"})
	h.until(func() bool { return h.status(s.ID) == "running" })
	if r, _ := h.m.Counts(); r != 1 {
		t.Fatalf("running = %d", r)
	}
	proc.result(true)
	h.until(func() bool { return h.status(s.ID) == "idle" })
}

func TestReleaseClosesProcessAndNextMessageResumes(t *testing.T) {
	h := newHarness(t, 2, func(p *fakeProc, n int, text string) {
		p.msg(claude.Message{Type: "system", Subtype: "init", SessionID: "c-1"})
		p.result(true)
	})
	s, _ := h.m.Create(store.Session{TaskID: h.task.ID, Kind: "chat", Title: "c", Cwd: "/w"}, "go")
	h.until(func() bool { return h.status(s.ID) == "idle" })
	if err := h.m.Release(s.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.procs[0].done:
	default:
		t.Fatal("process still running after Release")
	}
	if err := h.m.Send(s.ID, "again"); err != nil {
		t.Fatal(err)
	}
	h.until(func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.procs) == 2 })
	if h.procs[1].opts.Resume != "c-1" {
		t.Fatalf("resume = %q", h.procs[1].opts.Resume)
	}
	h.until(func() bool { return h.status(s.ID) == "idle" })
	if got, _ := h.st.Session(s.ID); got.Status != "idle" {
		t.Fatalf("status %s", got.Status)
	}
}
