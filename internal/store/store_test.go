package store

import (
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRoundTrip(t *testing.T) {
	s := open(t)
	w, err := s.InsertWorkspace(Workspace{Name: "w", Path: "/w", IsGit: true, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertWorkspace(Workspace{Name: "w", Path: "/w"}); err == nil {
		t.Fatal("duplicate path should fail")
	}
	task, err := s.InsertTask(Task{WorkspaceID: w.ID, Slug: "eng-1", Title: "T", Linear: &LinearRef{Identifier: "ENG-1"},
		WorktreePath: "/wt", ArtifactsPath: "/a", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Task(task.ID)
	if got.Linear == nil || got.Linear.Identifier != "ENG-1" || !s.SlugTaken("eng-1") {
		t.Fatalf("task %+v", got)
	}
	x, err := s.InsertSlice(Slice{TaskID: task.ID, Key: "pr1", Title: "S", DependsOn: []string{"a"}, Branch: "b", WorktreePath: "/s"})
	if err != nil {
		t.Fatal(err)
	}
	x.PRNumber, x.CIState = 12, "FAILURE"
	if err := s.UpdateSlice(x); err != nil {
		t.Fatal(err)
	}
	x2, _ := s.Slice(x.ID)
	if x2.PRNumber != 12 || x2.CIState != "FAILURE" || len(x2.DependsOn) != 1 {
		t.Fatalf("slice %+v", x2)
	}
	sess, _ := s.InsertSession(Session{TaskID: task.ID, Kind: "chat", Title: "c", Cwd: "/wt"})
	s.SetSessionCost(sess.ID, 0.5)
	s.SetSessionCost(sess.ID, 0.2)
	s.SetSessionStatus(sess.ID, "running", "")
	got2, _ := s.Session(sess.ID)
	if got2.CostUSD != 0.5 || got2.Status != "running" {
		t.Fatalf("session %+v", got2)
	}
	e, _ := s.AddEvent(sess.ID, "text", map[string]any{"text": "hi"})
	evs, _ := s.Events(sess.ID, 0)
	if len(evs) != 1 || evs[0].ID != e.ID || evs[0].Data["text"] != "hi" {
		t.Fatalf("events %+v", evs)
	}
	a, _ := s.InsertApproval(sess.ID, "Bash", map[string]any{"command": "ls"})
	if p, _ := s.PendingApprovals(); len(p) != 1 || p[0].Input["command"] != "ls" {
		t.Fatalf("approvals %+v", p)
	}
	s.RecoverSessions()
	got3, _ := s.Session(sess.ID)
	if got3.Status != "interrupted" {
		t.Fatal(got3.Status)
	}
	if p, _ := s.PendingApprovals(); len(p) != 0 {
		t.Fatal("approval should be cancelled", a)
	}
	c, _ := s.InsertComment(Comment{TaskID: task.ID, Target: "artifact", Path: "plan.md", Line: 3, Body: "why?"})
	s.MarkCommentsSent([]string{c.ID})
	cs, _ := s.Comments(task.ID)
	if len(cs) != 1 || cs[0].Status != "sent" {
		t.Fatalf("comments %+v", cs)
	}
	// Deleting the workspace cascades.
	s.DeleteWorkspace(w.ID)
	if ts, _ := s.Tasks(); len(ts) != 0 {
		t.Fatal("cascade failed")
	}
}
