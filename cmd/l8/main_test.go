package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/crashPopeye/layer8/internal/api"
	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

func TestMain(m *testing.M) {
	if os.Getenv("L8_TEST_RUN_MAIN") == "1" {
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// spawned records the options and messages of every claude process the daemon starts.
type spawned struct {
	mu    sync.Mutex
	opts  []claude.Options
	sends []string
}

type recordedProc struct {
	s *spawned
	h claude.Handlers
}

func (p recordedProc) Send(text string) error {
	p.s.mu.Lock()
	p.s.sends = append(p.s.sends, text)
	p.s.mu.Unlock()
	return nil
}
func (recordedProc) Interrupt() error               { return nil }
func (recordedProc) SetModel(string) error          { return nil }
func (recordedProc) SetPermissionMode(string) error { return nil }
func (p recordedProc) Close() error                 { p.h.OnExit(nil); return nil }

func (s *spawned) spawn(o claude.Options, h claude.Handlers) (session.Proc, error) {
	s.mu.Lock()
	s.opts = append(s.opts, o)
	s.mu.Unlock()
	return recordedProc{s, h}, nil
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream.git")
	git(t, root, "init", "-q", "--bare", "-b", "main", upstream)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", upstream, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "add", "-A")
	git(t, seed, "commit", "-qm", "init")
	git(t, seed, "push", "-q", "origin", "HEAD:main")
	clone := filepath.Join(root, "work")
	git(t, root, "clone", "-q", upstream, clone)
	return clone
}

// l8 runs this test binary as the l8 CLI against the daemon at addr, inside task taskID.
func l8(t *testing.T, home, addr, taskID string, args ...string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), "L8_TEST_RUN_MAIN=1", "LAYER8_HOME="+home, "LAYER8_ADDR="+addr, "L8_TASK_ID="+taskID)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("l8 %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestCreateSubtaskAndStartIt(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	const token = "s3cret"
	if err := os.WriteFile(filepath.Join(home, "token"), []byte(token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := &spawned{}
	hub := api.NewHub()
	sessions := session.NewManager(st, hub.Publish, rec.spawn, session.Config{ArtifactsRoot: filepath.Join(home, "artifacts")})
	tasks := task.NewService(st, gitx.Git{}, home, hub.Notify, sessions)
	srv := httptest.NewServer(api.New(st, hub, tasks, sessions, token, fstest.MapFS{}))
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")

	p, err := tasks.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	parent, err := tasks.Create(ctx, task.CreateInput{ProjectID: p.ID, Title: "Local agent desktop", Workflow: "prd_tdd", Autonomous: true})
	if err != nil {
		t.Fatal(err)
	}

	out := l8(t, home, addr, parent.ID, "task", "create", "--parent", "current", "--title", "Session runtime (Go)",
		"--workflow", "oneshot", "--prompt", "Build the session runtime.", "--start", "--json")
	var created task.Created
	if err := json.Unmarshal([]byte(out), &created); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if created.Slug != "session-runtime-go" || created.ParentID != parent.ID || created.BaseRef != parent.Branch || !created.Autonomous {
		t.Fatalf("created = %+v", created.Task)
	}
	if created.Session == nil || created.Session.Label != "implement" || created.Session.TaskID != created.ID {
		t.Fatalf("session = %+v", created.Session)
	}

	rec.mu.Lock()
	if len(rec.opts) != 1 || rec.opts[0].Cwd != created.Worktree || !slices.Contains(rec.opts[0].Env, "L8_TASK_ID="+created.ID) ||
		!strings.Contains(rec.opts[0].SystemPromptAppend, "Parent task: Local agent desktop · "+parent.ID) {
		t.Fatalf("spawned %+v", rec.opts)
	}
	if !slices.Equal(rec.sends, []string{"Build the session runtime."}) {
		t.Fatalf("first message = %q", rec.sends)
	}
	rec.mu.Unlock()

	listed := l8(t, home, addr, parent.ID, "task", "list", "--parent", "current")
	if !strings.Contains(listed, "session-runtime-go") || strings.Contains(listed, parent.Slug) {
		t.Fatalf("task list --parent current:\n%s", listed)
	}
	text := l8(t, home, addr, created.ID, "task", "show", "current")
	if !strings.Contains(text, "parent") || !strings.Contains(text, parent.ID) {
		t.Fatalf("task show:\n%s", text)
	}
	read := l8(t, home, addr, created.ID, "artifact", "read", "--task", "parent", "task.md")
	if !strings.HasPrefix(read, "# Local agent desktop") {
		t.Fatalf("parent task.md = %q", read)
	}
}
