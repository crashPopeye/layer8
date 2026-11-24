package session

import (
	"context"
	"slices"
	"testing"

	"github.com/crashPopeye/layer8/internal/store"
)

func TestRecover(t *testing.T) {
	f := newFixture(t)
	insert := func(s store.Session) {
		t.Helper()
		s.TaskID, s.CreatedAt = "t1", int64(len(s.ID))
		if err := f.st.InsertSession(s); err != nil {
			t.Fatal(err)
		}
	}
	insert(store.Session{ID: "mid-turn", ClaudeSessionID: "claude-1", Status: "waiting_approval", InTurn: true})
	insert(store.Session{ID: "idle", ClaudeSessionID: "claude-2", Status: "ready_for_input"})
	insert(store.Session{ID: "done", ClaudeSessionID: "claude-3", Status: "stopped"})
	if err := f.st.InsertApproval(store.Approval{ID: "a1", SessionID: "mid-turn", RequestID: "r1", ToolName: "Bash", Status: "pending"}); err != nil {
		t.Fatal(err)
	}

	if err := f.m.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.procs) != 1 || f.procs[0].opts.Resume != "claude-1" || !slices.Equal(f.procs[0].Sent(), []string{ResumePrompt}) {
		t.Fatalf("recover spawned %d processes, first %+v", len(f.procs), f.procs)
	}
	if a, _ := f.st.Approval("a1"); a.Status != "stale" {
		t.Fatalf("approval status = %q", a.Status)
	}
	if s := f.session(t, "mid-turn"); s.Status != "starting" || !s.InTurn {
		t.Fatalf("mid-turn after recover: %+v", s)
	}
	if s := f.session(t, "idle"); s.Status != "ready_for_input" || s.InTurn {
		t.Fatalf("idle after recover: %+v", s)
	}
	if s := f.session(t, "done"); s.Status != "stopped" {
		t.Fatalf("stopped session touched: %+v", s)
	}

	if _, err := f.m.Send(context.Background(), "idle", "next"); err != nil {
		t.Fatal(err)
	}
	if len(f.procs) != 2 || f.procs[1].opts.Resume != "claude-2" {
		t.Fatalf("idle session respawned with %+v", f.procs[len(f.procs)-1].opts)
	}
}
