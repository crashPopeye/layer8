// Package gitx runs the git CLI with the user's own config and credentials.
package gitx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Git runs git commands; its zero value is ready to use.
type Git struct{}

// Run runs git -C dir args and returns trimmed stdout, or an error carrying stderr.
func (Git) Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// TopLevel returns the root of the working tree containing dir.
func (g Git) TopLevel(ctx context.Context, dir string) (string, error) {
	return g.Run(ctx, dir, "rev-parse", "--show-toplevel")
}

// CommonDir returns the absolute git dir shared by a repo and all its worktrees.
func (g Git) CommonDir(ctx context.Context, dir string) (string, error) {
	return g.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

// DefaultBranch returns the branch origin/HEAD points to, else the current branch.
func (g Git) DefaultBranch(ctx context.Context, repo string) (string, error) {
	if ref, err := g.Run(ctx, repo, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil {
		return strings.TrimPrefix(ref, "origin/"), nil
	}
	return g.CurrentBranch(ctx, repo)
}

// CurrentBranch returns the branch checked out in dir.
func (g Git) CurrentBranch(ctx context.Context, dir string) (string, error) {
	return g.Run(ctx, dir, "branch", "--show-current")
}

// Fetch updates origin's copy of branch.
func (g Git) Fetch(ctx context.Context, repo, branch string) error {
	_, err := g.Run(ctx, repo, "fetch", "--quiet", "origin", branch)
	return err
}

// RefExists reports whether ref resolves to a commit.
func (g Git) RefExists(ctx context.Context, repo, ref string) bool {
	_, err := g.Run(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// WorktreeAdd creates a worktree at path on a new branch starting at base.
func (g Git) WorktreeAdd(ctx context.Context, repo, path, branch, base string) error {
	_, err := g.Run(ctx, repo, "worktree", "add", "--no-track", "-b", branch, path, base)
	return err
}
