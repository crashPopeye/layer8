package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
)

// procs records every claude process a Manager spawns; tests play the CLI through the handlers.
type procs struct {
	mu   sync.Mutex
	list []*proc
}

type proc struct {
	h    claude.Handlers
	sent []string
}

func (p *proc) Send(text string) error         { p.sent = append(p.sent, text); return nil }
func (p *proc) Interrupt() error               { return nil }
func (p *proc) SetModel(string) error          { return nil }
func (p *proc) SetPermissionMode(string) error { return nil }
func (p *proc) Close() error                   { p.h.OnExit(nil); return nil }
func (ps *procs) count() int                   { ps.mu.Lock(); defer ps.mu.Unlock(); return len(ps.list) }
func (ps *procs) get(i int) *proc              { ps.mu.Lock(); defer ps.mu.Unlock(); return ps.list[i] }
func (ps *procs) spawn(o claude.Options, h claude.Handlers) (session.Proc, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p := &proc{h: h}
	ps.list = append(ps.list, p)
	return p, nil
}

// serviceWithSessions returns a Service whose sessions run on recorded fake processes.
func serviceWithSessions(t *testing.T) (*Service, *store.Store, *procs) {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ps := &procs{}
	sm := session.NewManager(st, func(any) {}, ps.spawn, session.Config{ArtifactsRoot: filepath.Join(home, "artifacts")})
	return NewService(st, gitx.Git{}, home, func(kind, id string) {}, sm), st, ps
}

func TestStartRunsTheFirstPhase(t *testing.T) {
	ctx := context.Background()
	svc, _, ps := serviceWithSessions(t)
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Make lint", Workflow: "prd_tdd", Prompt: "add a make lint target", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	if c.Phase != "research-questions" || c.Session == nil || c.Session.Label != "research-questions" || c.Session.Phase != "research-questions" {
		t.Fatalf("created = %+v, session %+v", c.Task, c.Session)
	}
	if sent := ps.get(0).sent; !slices.Equal(sent, []string{"/l8:create-research-questions .layer8/task/"}) {
		t.Fatalf("first message = %q", sent)
	}
	free, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Free", Prompt: "just chat", Start: true})
	if err != nil || free.Phase != "" || free.Session.Label != "session" || !slices.Equal(ps.get(1).sent, []string{"just chat"}) {
		t.Fatalf("freeform = %+v, %v", free, err)
	}
}

func TestPhaseCheckAdvancesOnce(t *testing.T) {
	ctx := context.Background()
	svc, st, ps := serviceWithSessions(t)
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Make lint", Workflow: "prd_tdd"})
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := svc.Detail(ctx, c.ID); d.Next != "research-questions" || len(d.Phases) != 7 {
		t.Fatalf("fresh task progress = %+v", d.Progress)
	}
	prd := "prd"
	if _, err := svc.Update(ctx, c.ID, TaskUpdate{Phase: &prd}); err != nil {
		t.Fatal(err)
	}
	s, err := svc.sessions.Start(ctx, c.ID, session.StartInput{Label: "prd", Prompt: "/l8:create-prd .layer8/task/"})
	if err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(svc.ArtifactsDir(c.ID), "03-prd-make-lint.md")
	if err := os.WriteFile(artifact, []byte("---\ntype: design-prd\n---\n# PRD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	task, _ := st.Task(c.ID)
	task.AutoAdvance = true
	if err := st.UpdateTask(task); err != nil {
		t.Fatal(err)
	}
	svc.PhaseCheck(s)
	if n := ps.count(); n != 1 {
		t.Fatalf("a mid-turn prd session advanced the task: %d sessions", n)
	}

	ps.get(0).h.OnMessage(claude.Message{Type: "result", Subtype: "success"})
	for range 2 {
		svc.PhaseCheck(s)
	}
	if n := ps.count(); n != 2 {
		t.Fatalf("%d sessions after two checks, want the prd one and one tdd", n)
	}
	if sent := ps.get(1).sent; !slices.Equal(sent, []string{"/l8:create-tdd .layer8/task/"}) {
		t.Fatalf("tdd session got %q", sent)
	}
	sessions, _ := st.Sessions(c.ID)
	if sessions[1].Label != "tdd" || sessions[1].Phase != "tdd" {
		t.Fatalf("tdd session = %+v", sessions[1])
	}
	d, err := svc.Detail(ctx, c.ID)
	if err != nil || d.Phase != "tdd" || d.Next != "" || !d.Phases[2].Done || d.Phases[3].Done {
		t.Fatalf("progress after advance = %+v %+v, %v", d.Task, d.Progress, err)
	}
}

func TestPhaseCheckOffersNextWithoutAutoAdvance(t *testing.T) {
	ctx := context.Background()
	svc, _, ps := serviceWithSessions(t)
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "One shot", Workflow: "oneshot", Prompt: "touch a file", Start: true})
	if err != nil {
		t.Fatal(err)
	}
	ps.get(0).h.OnMessage(claude.Message{Type: "result", Subtype: "success"})
	svc.PhaseCheck(*c.Session)
	if d, _ := svc.Detail(ctx, c.ID); d.Next != "" || ps.count() != 1 {
		t.Fatalf("implement without a diff offers %q", d.Next)
	}

	if err := os.WriteFile(filepath.Join(c.Worktree, "lint.mk"), []byte("lint:\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc.PhaseCheck(*c.Session)
	if d, _ := svc.Detail(ctx, c.ID); d.Next != "pr" || !d.Phases[0].Done || ps.count() != 1 {
		t.Fatalf("after the diff: next %q, phases %+v, %d sessions", d.Next, d.Phases, ps.count())
	}

	on := true
	if _, err := svc.Update(ctx, c.ID, TaskUpdate{AutoAdvance: &on}); err != nil {
		t.Fatal(err)
	}
	if ps.count() != 2 || !slices.Equal(ps.get(1).sent, []string{"/l8:describe-pr .layer8/task/"}) {
		t.Fatalf("turning auto-advance on did not start the pr phase: %d sessions", ps.count())
	}
	if _, err := svc.Next(ctx, c.ID); err == nil {
		t.Fatal("Next after the last phase succeeded")
	}
}
