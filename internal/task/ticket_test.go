package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/crashPopeye/layer8/internal/linear"
)

// fakeLinear is a Linear GraphQL server holding a few FLOW issues; it records the
// issueUpdate mutations it receives.
type fakeLinear struct {
	mu      sync.Mutex
	updates []map[string]any
}

func issueJSON(id, title string, children ...string) map[string]any {
	branch := "alex/" + strings.ToLower(id) + "-" + slugify(title)
	kids := []map[string]any{}
	for _, c := range children {
		kids = append(kids, issueJSON(c, "Child "+c))
	}
	return map[string]any{
		"id": "uuid-" + id, "identifier": id, "title": title, "url": "https://linear.app/scoreplay/issue/" + id,
		"branchName": branch, "state": map[string]any{"name": "Todo"}, "team": map[string]any{"key": "FLOW"},
		"description": title + " description.", "children": map[string]any{"nodes": kids},
		"labels": map[string]any{"nodes": []any{}}, "attachments": map[string]any{"nodes": []any{}},
		"comments": map[string]any{"nodes": []any{}},
	}
}

var fakeIssues = map[string]map[string]any{
	"FLOW-1297": issueJSON("FLOW-1297", "Media ingest retry", "FLOW-1298", "FLOW-1299"),
	"FLOW-1298": issueJSON("FLOW-1298", "Child FLOW-1298"),
	"FLOW-1299": issueJSON("FLOW-1299", "Child FLOW-1299"),
}

func (f *fakeLinear) client(t *testing.T) *linear.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		var data any
		switch {
		case strings.Contains(req.Query, "issueUpdate"):
			f.mu.Lock()
			f.updates = append(f.updates, req.Variables)
			f.mu.Unlock()
			data = map[string]any{"issueUpdate": map[string]any{"success": true}}
		case strings.Contains(req.Query, "states"):
			data = map[string]any{"issue": map[string]any{"id": "uuid-" + req.Variables["id"].(string), "team": map[string]any{
				"states": map[string]any{"nodes": []any{map[string]any{"id": "s-review", "name": "In Review"}}}}}}
		default:
			issue, ok := fakeIssues[req.Variables["id"].(string)]
			if !ok {
				json.NewEncoder(w).Encode(map[string]any{"errors": []any{map[string]any{"message": "Entity not found"}}})
				return
			}
			data = map[string]any{"issue": issue}
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return linear.NewAt(srv.URL, "lin_api_x")
}

func TestTicketTaskAndSubtasksFromChildren(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	svc.UseLinear((&fakeLinear{}).client(t))
	clone := repo(t)
	p, err := svc.AddProject(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	git(t, clone, "checkout", "-q", "-b", "alex/flow-1299-child-flow-1299", "origin/main")
	git(t, clone, "commit", "-q", "--allow-empty", "-m", "started by hand")
	started := git(t, clone, "rev-parse", "HEAD")
	git(t, clone, "checkout", "-q", "main")

	parent, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Ticket: "FLOW-1297", Workflow: "oneshot", Children: true})
	if err != nil {
		t.Fatal(err)
	}
	if parent.Title != "Media ingest retry" || parent.Slug != "flow-1297-media-ingest-retry" || parent.Ticket != "FLOW-1297" ||
		parent.TicketURL != "https://linear.app/scoreplay/issue/FLOW-1297" || parent.Branch != "alex/flow-1297-media-ingest-retry" {
		t.Fatalf("task = %+v", parent.Task)
	}
	if got := git(t, parent.Worktree, "branch", "--show-current"); got != parent.Branch {
		t.Fatalf("worktree branch = %q", got)
	}
	ticket, err := os.ReadFile(filepath.Join(svc.ArtifactsDir(parent.ID), TicketFile))
	if err != nil || !strings.HasPrefix(string(ticket), "# FLOW-1297: Media ingest retry\n") || !strings.Contains(string(ticket), "FLOW-1298: Child FLOW-1298") {
		t.Fatalf("ticket.md = %q, %v", ticket, err)
	}
	md, _ := os.ReadFile(filepath.Join(svc.ArtifactsDir(parent.ID), "task.md"))
	if !strings.Contains(string(md), "[FLOW-1297](https://linear.app/scoreplay/issue/FLOW-1297)") {
		t.Fatalf("task.md = %q", md)
	}

	if len(parent.Subtasks) != 2 {
		t.Fatalf("sub-tasks = %+v", parent.Subtasks)
	}
	for i, id := range []string{"FLOW-1298", "FLOW-1299"} {
		sub := parent.Subtasks[i]
		want := "alex/" + strings.ToLower(id) + "-child-" + strings.ToLower(id)
		if sub.ParentID != parent.ID || sub.Ticket != id || sub.Branch != want || sub.Workflow != "oneshot" {
			t.Fatalf("sub-task %d = %+v", i, sub.Task)
		}
		if got := git(t, sub.Worktree, "branch", "--show-current"); got != want {
			t.Fatalf("sub-task %s worktree on %q", id, got)
		}
	}
	if head := git(t, parent.Subtasks[1].Worktree, "rev-parse", "HEAD"); head != started {
		t.Fatalf("the existing branch of FLOW-1299 was not reused: HEAD %s, want %s", head, started)
	}

	again, err := svc.SubtasksFromChildren(ctx, parent.Slug, SubtasksInput{})
	if err != nil || len(again) != 0 {
		t.Fatalf("second SubtasksFromChildren = %+v, %v", again, err)
	}
}

func TestWriteBackOnOpenPR(t *testing.T) {
	gitIdentity(t)
	ctx := context.Background()
	svc := service(t)
	lin := &fakeLinear{}
	svc.UseLinear(lin.client(t))
	svc.gh = (&fakeGH{out: map[string]string{"pr create": "https://github.com/o/r/pull/7\n"}}).cli()
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	on := true
	if _, err := svc.UpdateProject(p.ID, ProjectUpdate{LinearWriteBack: &on}); err != nil {
		t.Fatal(err)
	}
	tk, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Ticket: "FLOW-1298"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tk.Worktree, "queue.go"), []byte("package queue\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(ctx, tk.ID, "Retry queue"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenPR(ctx, tk.ID, PRInput{Title: "Retry queue"}); err != nil {
		t.Fatal(err)
	}
	if len(lin.updates) != 1 || lin.updates[0]["id"] != "uuid-FLOW-1298" || lin.updates[0]["state"] != "s-review" {
		t.Fatalf("issueUpdate calls = %v", lin.updates)
	}
}

func TestWithoutLinear(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	if svc.LinearConnected() {
		t.Fatal("connected without a key")
	}
	plain, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "No ticket"})
	if err != nil {
		t.Fatal(err)
	}
	svc.writeBack(ctx, plain.Task)
	if _, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Ticket: "FLOW-1297"}); !errors.Is(err, ErrNoLinear) || !errors.Is(err, ErrInvalid) {
		t.Fatalf("ticket task without Linear: %v", err)
	}
	if _, err := svc.Issues(ctx, ""); !errors.Is(err, ErrNoLinear) {
		t.Fatalf("Issues without Linear: %v", err)
	}
	if _, err := svc.SubtasksFromChildren(ctx, plain.ID, SubtasksInput{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("SubtasksFromChildren of a task without ticket: %v", err)
	}
}

func TestSetLinearKey(t *testing.T) {
	svc := service(t)
	if err := svc.SetLinearKey("lin_api_x"); err != nil || !svc.LinearConnected() {
		t.Fatalf("connect: %v", err)
	}
	info, err := os.Stat(filepath.Join(svc.home, "config.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config.json: %v, %v", info, err)
	}
	if err := svc.SetLinearKey(""); err != nil || svc.LinearConnected() {
		t.Fatalf("disconnect: %v", err)
	}
}
