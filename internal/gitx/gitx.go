// Package gitx runs the git CLI with the user's login-shell PATH.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const pathMarker = "__LAYER8_PATH__"

// LoginPath returns the PATH of the user's login shell, falling back to the daemon's own.
var LoginPath = sync.OnceValue(func() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		return os.Getenv("PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, shell, "-ilc", "echo "+pathMarker+"$PATH").Output()
	_, path, found := strings.Cut(string(out), pathMarker)
	if err != nil || !found {
		return os.Getenv("PATH")
	}
	path, _, _ = strings.Cut(path, "\n")
	return path
})

// Command returns a command for name found on the login-shell PATH, which it also gets in its environment.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	path := LoginPath()
	bin := name
	for _, dir := range filepath.SplitList(path) {
		if p, err := exec.LookPath(filepath.Join(dir, name)); dir != "" && err == nil {
			bin = p
			break
		}
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "PATH="+path)
	return cmd
}

// Run runs git in dir and returns its trimmed stdout, or an error carrying its stderr.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := RunEnv(ctx, dir, nil, args...)
	return strings.TrimSpace(out), err
}

// RunEnv is Run with extra environment variables, returning stdout untrimmed.
func RunEnv(ctx context.Context, dir string, env []string, args ...string) (string, error) {
	cmd := Command(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(cmd.Env, env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return string(out), nil
}

// Toplevel returns the root of the repository containing dir.
func Toplevel(ctx context.Context, dir string) (string, error) {
	return Run(ctx, dir, "rev-parse", "--show-toplevel")
}

// DefaultBranch returns origin's HEAD branch, else the checked-out branch, else main.
func DefaultBranch(ctx context.Context, repo string) string {
	if ref, err := Run(ctx, repo, "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil {
		return strings.TrimPrefix(ref, "origin/")
	}
	if ref, err := Run(ctx, repo, "symbolic-ref", "--short", "HEAD"); err == nil {
		return ref
	}
	return "main"
}

// RefExists reports whether ref names a commit in repo.
func RefExists(ctx context.Context, repo, ref string) bool {
	_, err := Run(ctx, repo, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
}

// AddWorktree checks out branch at path, creating the branch from start unless start is empty.
func AddWorktree(ctx context.Context, repo, path, branch, start string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	args := []string{"worktree", "add", path, branch}
	if start != "" {
		args = []string{"worktree", "add", "--no-track", "-b", branch, path, start}
	}
	_, err := Run(ctx, repo, args...)
	return err
}

// RemoveWorktree deletes the worktree at path, discarding its changes; the branch stays.
func RemoveWorktree(ctx context.Context, repo, path string) error {
	_, err := Run(ctx, repo, "worktree", "remove", "--force", path)
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		_, err = Run(ctx, repo, "worktree", "prune")
	}
	return err
}

// Status summarises a worktree: its branch, head, commits ahead of and behind upstream, and changed paths.
type Status struct {
	Branch  string `json:"branch"`
	Head    string `json:"head"`
	Ahead   int    `json:"ahead"`
	Behind  int    `json:"behind"`
	Changed int    `json:"changed"`
}

// StatusOf reads the Status of the worktree at dir.
func StatusOf(ctx context.Context, dir string) (Status, error) {
	out, err := Run(ctx, dir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return Status{}, err
	}
	var s Status
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		switch {
		case len(f) == 0:
		case f[0] != "#":
			s.Changed++
		case len(f) == 3 && f[1] == "branch.oid":
			s.Head = f[2]
		case len(f) == 3 && f[1] == "branch.head":
			s.Branch = f[2]
		case len(f) == 4 && f[1] == "branch.ab":
			s.Ahead, _ = strconv.Atoi(strings.TrimPrefix(f[2], "+"))
			s.Behind, _ = strconv.Atoi(strings.TrimPrefix(f[3], "-"))
		}
	}
	return s, nil
}
