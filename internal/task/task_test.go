package task

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/store"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// repo returns a clone of a bare upstream whose main branch has one commit.
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

func service(t *testing.T) *Service {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewService(st, gitx.Git{}, home, func(kind, id string) {})
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	clone := repo(t)
	p, err := svc.AddProject(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	if p.BaseBranch != "main" {
		t.Fatalf("base branch = %q", p.BaseBranch)
	}
	instructions := "Use Golang as much as possible wherever it is possible and relevant."
	if _, err := svc.UpdateProject(p.ID, ProjectUpdate{Instructions: &instructions}); err != nil {
		t.Fatal(err)
	}

	task, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Smoke task!", Prompt: "Do the thing."})
	if err != nil {
		t.Fatal(err)
	}
	if task.Slug != "smoke-task" || task.Branch != "l8/smoke-task" || task.BaseRef != "origin/main" {
		t.Fatalf("task = %+v", task)
	}
	if got := git(t, task.Worktree, "branch", "--show-current"); got != "l8/smoke-task" {
		t.Fatalf("worktree branch = %q", got)
	}
	if head, main := git(t, task.Worktree, "rev-parse", "HEAD"), git(t, clone, "rev-parse", "origin/main"); head != main {
		t.Fatalf("worktree HEAD %s, origin/main %s", head, main)
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(task.Worktree, LinkPath))
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := filepath.EvalSymlinks(svc.ArtifactsDir(task.ID)); resolved != want {
		t.Fatalf("link resolves to %s, want %s", resolved, want)
	}

	exclude, err := os.ReadFile(filepath.Join(clone, ".git", "info", "exclude"))
	if err != nil || !strings.Contains(string(exclude), "\n.layer8/\n") {
		t.Fatalf("exclude = %q, %v", exclude, err)
	}
	if status := git(t, task.Worktree, "status", "--porcelain"); status != "" {
		t.Fatalf("worktree is dirty: %q", status)
	}

	md, err := os.ReadFile(filepath.Join(svc.ArtifactsDir(task.ID), "task.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(md), "Do the thing.") || !strings.Contains(string(md), "## Instructions\n\n"+instructions) {
		t.Fatalf("task.md = %q", md)
	}

	again, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Smoke task"})
	if err != nil || again.Slug != "smoke-task-2" {
		t.Fatalf("second task = %+v, %v", again, err)
	}
	exclude, _ = os.ReadFile(filepath.Join(clone, ".git", "info", "exclude"))
	if n := strings.Count(string(exclude), ".layer8/"); n != 1 {
		t.Fatalf(".layer8/ appears %d times in exclude", n)
	}
}

func TestAdoptKeepsBranch(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	clone := repo(t)
	p, err := svc.AddProject(ctx, clone)
	if err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(t.TempDir(), "in-flight")
	git(t, clone, "worktree", "add", "-q", "-b", "alex/in-flight", existing, "origin/main")

	task, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "In flight", AdoptPath: existing})
	if err != nil {
		t.Fatal(err)
	}
	if task.Branch != "alex/in-flight" {
		t.Fatalf("branch = %q", task.Branch)
	}
	if want, _ := filepath.EvalSymlinks(existing); task.Worktree != want && task.Worktree != existing {
		t.Fatalf("worktree = %q, want %q", task.Worktree, existing)
	}
	if _, err := os.Stat(filepath.Join(existing, LinkPath, "task.md")); err != nil {
		t.Fatal(err)
	}
	if got := git(t, existing, "branch", "--show-current"); got != "alex/in-flight" {
		t.Fatalf("branch after adopt = %q", got)
	}
}

func TestCreateRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []CreateInput{
		{ProjectID: p.ID},
		{ProjectID: p.ID, Title: "x", Workflow: "waterfall"},
	} {
		if _, err := svc.Create(ctx, in); err == nil {
			t.Fatalf("Create(%+v) succeeded", in)
		}
	}
	if _, err := svc.AddProject(ctx, t.TempDir()); err == nil {
		t.Fatal("AddProject on a plain folder succeeded")
	}
}
