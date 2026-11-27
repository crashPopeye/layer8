package gitx_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/gitx"
)

// Repo creates a repository on branch trunk with one commit and returns its resolved path.
func Repo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	for _, args := range [][]string{
		{"init", "-q", "-b", "trunk"},
		{"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if _, err := gitx.Run(t.Context(), dir, args...); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestWorktrees(t *testing.T) {
	ctx := t.Context()
	repo := Repo(t)
	if got := gitx.DefaultBranch(ctx, repo); got != "trunk" {
		t.Fatalf("default branch = %q", got)
	}
	if !gitx.RefExists(ctx, repo, "trunk") || gitx.RefExists(ctx, repo, "origin/trunk") {
		t.Fatal("RefExists is wrong")
	}

	wt := filepath.Join(t.TempDir(), "worktrees", "proj", "feat")
	if err := gitx.AddWorktree(ctx, repo, wt, "feat", "trunk"); err != nil {
		t.Fatal(err)
	}
	if err := gitx.AddWorktree(ctx, repo, wt+"2", "feat", "trunk"); err == nil {
		t.Fatal("creating an existing branch must fail")
	}
	os.WriteFile(filepath.Join(wt, "a.txt"), []byte("a"), 0o644)
	s, err := gitx.StatusOf(ctx, wt)
	if err != nil {
		t.Fatal(err)
	}
	if s.Branch != "feat" || len(s.Head) != 40 || s.Changed != 1 {
		t.Fatalf("status = %+v", s)
	}

	if err := gitx.RemoveWorktree(ctx, repo, wt); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatal("worktree still on disk")
	}
	if err := gitx.AddWorktree(ctx, repo, wt, "feat", ""); err != nil {
		t.Fatalf("re-adding an existing branch: %v", err)
	}
	os.RemoveAll(wt)
	if err := gitx.RemoveWorktree(ctx, repo, wt); err != nil {
		t.Fatalf("removing a vanished worktree: %v", err)
	}
	if out, _ := gitx.Run(ctx, repo, "worktree", "list"); strings.Contains(out, wt) {
		t.Fatalf("worktree not pruned: %s", out)
	}
}
