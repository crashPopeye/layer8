package gitx

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// maxUntracked is the largest untracked file whose lines are shown.
const maxUntracked = 1 << 20

// Changes is a worktree against its merge-base with a base ref: committed, staged,
// unstaged and untracked work alike.
type Changes struct {
	Base      string `json:"base"`
	MergeBase string `json:"mergeBase"`
	Files     []File `json:"files"`
}

// Changes diffs worktree against its merge-base with baseRef, detecting renames,
// and lists untracked files as added.
func (g Git) Changes(ctx context.Context, worktree, baseRef string) (Changes, error) {
	mb, err := g.Run(ctx, worktree, "merge-base", baseRef, "HEAD")
	if err != nil {
		return Changes{}, err
	}
	out, err := g.output(ctx, worktree, "diff", "--no-color", "--no-ext-diff", "-M", mb)
	if err != nil {
		return Changes{}, err
	}
	c := Changes{Base: baseRef, MergeBase: mb, Files: Parse(out)}
	untracked, err := g.output(ctx, worktree, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return Changes{}, err
	}
	for _, p := range strings.Split(untracked, "\x00") {
		if p != "" {
			c.Files = append(c.Files, untrackedFile(worktree, p))
		}
	}
	return c, nil
}

// untrackedFile renders a file git does not know yet as entirely added.
func untrackedFile(worktree, rel string) File {
	f := File{OldPath: rel, NewPath: rel, Status: "added", Untracked: true, Hunks: []Hunk{}}
	data, err := readUntracked(filepath.Join(worktree, rel))
	if err != nil || len(data) > maxUntracked || bytes.IndexByte(data, 0) >= 0 {
		f.Binary = true
		return f
	}
	if len(data) == 0 {
		return f
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	h := Hunk{Header: "@@ -0,0 +1," + strconv.Itoa(len(lines)) + " @@", Lines: make([]Line, len(lines))}
	for i, l := range lines {
		h.Lines[i] = Line{Kind: Add, New: i + 1, Text: l}
	}
	f.Hunks, f.Adds = []Hunk{h}, len(lines)
	return f
}

// readUntracked reads a file, or a symlink's target the way git stores it.
func readUntracked(path string) ([]byte, error) {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(path)
		return []byte(target), err
	}
	return os.ReadFile(path)
}

// Dirty reports whether the worktree has uncommitted or untracked changes.
func (g Git) Dirty(ctx context.Context, worktree string) (bool, error) {
	out, err := g.Run(ctx, worktree, "status", "--porcelain")
	return out != "", err
}

// CommitAll stages everything in the worktree, commits it with message and returns the new HEAD.
func (g Git) CommitAll(ctx context.Context, worktree, message string) (string, error) {
	if _, err := g.Run(ctx, worktree, "add", "-A"); err != nil {
		return "", err
	}
	if _, err := g.Run(ctx, worktree, "commit", "--quiet", "-m", message); err != nil {
		return "", err
	}
	return g.Head(ctx, worktree)
}

// Head returns the commit checked out in dir.
func (g Git) Head(ctx context.Context, dir string) (string, error) {
	return g.Run(ctx, dir, "rev-parse", "HEAD")
}

// IsAncestor reports whether commit a is reachable from commit b.
func (g Git) IsAncestor(ctx context.Context, dir, a, b string) bool {
	_, err := g.Run(ctx, dir, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// Push pushes branch to origin and sets it as upstream. A non-empty lease
// force-pushes, but only while origin's branch is still at that commit.
func (g Git) Push(ctx context.Context, dir, branch, lease string) error {
	args := []string{"push", "--quiet", "-u", "origin", branch}
	if lease != "" {
		args = append(args, "--force-with-lease="+branch+":"+lease)
	}
	_, err := g.Run(ctx, dir, args...)
	return err
}
