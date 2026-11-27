// Package diff reads a task worktree's changes against its base, and commits and pushes them.
package diff

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/crashPopeye/layer8/internal/gitx"
)

// Mode selects which changes a diff covers.
type Mode string

const (
	All         Mode = "all"
	Uncommitted Mode = "uncommitted"
	Committed   Mode = "committed"
)

// Diff is a parsed diff of a worktree.
type Diff struct {
	Mode  Mode   `json:"mode"`
	Base  string `json:"base"`
	Files []File `json:"files"`
	Stats
}

// Stats counts the files and lines a diff touches.
type Stats struct {
	Files   int `json:"files"`
	Added   int `json:"added"`
	Deleted int `json:"deleted"`
}

// File is one file of a diff.
type File struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Status  string `json:"status"`
	Binary  bool   `json:"binary,omitempty"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Hunks   []Hunk `json:"hunks"`
}

// Hunk is one @@ section of a file.
type Hunk struct {
	Header   string `json:"header"`
	OldStart int    `json:"oldStart"`
	NewStart int    `json:"newStart"`
	Lines    []Line `json:"lines"`
}

// Line is one line of a hunk; Old and New are its line numbers, zero on the side it is absent from.
type Line struct {
	Kind string `json:"kind"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
}

var lineKinds = map[byte]string{' ': "ctx", '+': "add", '-': "del"}

// Parse reads git's unified diff output.
func Parse(patch string) []File {
	files := []File{}
	var f *File
	var h *Hunk
	var old, nu, oldLeft, newLeft int
	for line := range strings.Lines(patch) {
		line = strings.TrimSuffix(line, "\n")
		if h != nil && (oldLeft > 0 || newLeft > 0) {
			kind, ok := lineKinds[(line + " ")[0]]
			if !ok {
				continue
			}
			l := Line{Kind: kind, Text: line[min(1, len(line)):]}
			if kind != "add" {
				old++
				oldLeft--
				l.Old = old
			}
			if kind != "del" {
				nu++
				newLeft--
				l.New = nu
			}
			switch kind {
			case "add":
				f.Added++
			case "del":
				f.Deleted++
			}
			h.Lines = append(h.Lines, l)
			continue
		}
		switch {
		case strings.HasPrefix(line, "diff --git "):
			files = append(files, File{Status: "modified", Hunks: []Hunk{}})
			f, h = &files[len(files)-1], nil
			f.OldPath, f.Path = headerPaths(strings.TrimPrefix(line, "diff --git "))
		case f == nil:
		case strings.HasPrefix(line, "@@ "):
			var oldN, newN int
			fields := strings.Fields(line)
			old, oldN = hunkRange(fields[1])
			nu, newN = hunkRange(fields[2])
			f.Hunks = append(f.Hunks, Hunk{Header: line, OldStart: old, NewStart: nu, Lines: []Line{}})
			h = &f.Hunks[len(f.Hunks)-1]
			oldLeft, newLeft = oldN, newN
			old, nu = old-1, nu-1
		case strings.HasPrefix(line, "new file mode"):
			f.Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			f.Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			f.Status, f.OldPath = "renamed", unquote(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			f.Path = unquote(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "Binary files "):
			f.Binary = true
		}
	}
	for i := range files {
		if files[i].Status != "renamed" {
			files[i].OldPath = ""
		}
	}
	return files
}

// hunkRange parses "-12,3" or "+12" into start and count.
func hunkRange(s string) (start, count int) {
	a, b, found := strings.Cut(s[1:], ",")
	start, _ = strconv.Atoi(a)
	count = 1
	if found {
		count, _ = strconv.Atoi(b)
	}
	return start, count
}

// headerPaths splits "a/<old> b/<new>" of a diff --git line.
func headerPaths(s string) (old, nu string) {
	if n := (len(s) - 5) / 2; n > 0 && strings.HasPrefix(s, "a/") && s[2:2+n] == s[n+5:] && s[2+n:n+5] == " b/" {
		return s[2 : 2+n], s[2 : 2+n]
	}
	if strings.HasPrefix(s, `"`) {
		if i := strings.Index(s, `" "`); i > 0 {
			return strings.TrimPrefix(unquote(s[:i+1]), "a/"), strings.TrimPrefix(unquote(s[i+2:]), "b/")
		}
	}
	a, b, _ := strings.Cut(s, " b/")
	return strings.TrimPrefix(a, "a/"), b
}

func unquote(s string) string {
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

var diffFlags = []string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff", "--no-textconv",
	"--find-renames", "--src-prefix=a/", "--dst-prefix=b/"}

// Compute diffs the worktree at dir against its base branch in the given mode.
func Compute(ctx context.Context, dir, base string, mode Mode) (Diff, error) {
	mb, err := MergeBase(ctx, dir, base)
	if err != nil {
		return Diff{}, err
	}
	out, err := run(ctx, dir, mb, mode)
	if err != nil {
		return Diff{}, err
	}
	d := Diff{Mode: mode, Base: mb, Files: Parse(out)}
	for _, f := range d.Files {
		d.Stats.Files++
		d.Added += f.Added
		d.Deleted += f.Deleted
	}
	return d, nil
}

// Summarize counts the worktree's changes against its base, committed or not.
func Summarize(ctx context.Context, dir, base string) (Stats, error) {
	mb, err := MergeBase(ctx, dir, base)
	if err != nil {
		return Stats{}, err
	}
	out, err := run(ctx, dir, mb, All, "--numstat")
	var s Stats
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		added, _ := strconv.Atoi(f[0])
		deleted, _ := strconv.Atoi(f[1])
		s.Files++
		s.Added += added
		s.Deleted += deleted
	}
	return s, err
}

// run prints the diff of mode against mb, untracked files included.
func run(ctx context.Context, dir, mb string, mode Mode, extra ...string) (string, error) {
	args := append(slices.Clone(diffFlags), extra...)
	if mode == Committed {
		return gitx.RunEnv(ctx, dir, nil, append(args, mb, "HEAD", "--")...)
	}
	from := map[Mode]string{All: mb, Uncommitted: "HEAD"}[mode]
	if from == "" {
		return "", fmt.Errorf("mode must be all, uncommitted or committed, not %q", mode)
	}
	env, cleanup, err := snapshot(ctx, dir)
	if err != nil {
		return "", err
	}
	defer cleanup()
	return gitx.RunEnv(ctx, dir, env, append(args, "--cached", from, "--")...)
}

// snapshot returns the env selecting a throwaway index that holds the whole working tree, and its cleanup.
func snapshot(ctx context.Context, dir string) (env []string, cleanup func(), err error) {
	index, err := gitx.Run(ctx, dir, "rev-parse", "--path-format=absolute", "--git-path", "index")
	if err != nil {
		return nil, nil, err
	}
	data, err := os.ReadFile(index)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, nil, err
	}
	tmp := filepath.Join(os.TempDir(), "layer8-index-"+rand.Text())
	cleanup = func() { os.Remove(tmp) }
	if len(data) > 0 {
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return nil, nil, err
		}
	}
	env = []string{"GIT_INDEX_FILE=" + tmp}
	if _, err := gitx.RunEnv(ctx, dir, env, "add", "--all"); err != nil {
		cleanup()
		return nil, nil, err
	}
	return env, cleanup, nil
}

// baseRefs returns base and origin/base, keeping those that exist.
func baseRefs(ctx context.Context, dir, base string) ([]string, error) {
	var refs []string
	for _, ref := range []string{base, "origin/" + base} {
		if gitx.RefExists(ctx, dir, ref) {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("base branch %s not found", base)
	}
	return refs, nil
}

// MergeBase returns the commit HEAD forked from, taking the newer of base and origin/base.
func MergeBase(ctx context.Context, dir, base string) (string, error) {
	refs, err := baseRefs(ctx, dir, base)
	if err != nil {
		return "", err
	}
	return gitx.Run(ctx, dir, append([]string{"merge-base", "HEAD"}, refs...)...)
}

// AheadBehind counts the commits HEAD has that base lacks, and the reverse.
func AheadBehind(ctx context.Context, dir, base string) (ahead, behind int, err error) {
	refs, err := baseRefs(ctx, dir, base)
	if err != nil {
		return 0, 0, err
	}
	count := func(args ...string) int {
		out, e := gitx.Run(ctx, dir, append([]string{"rev-list", "--count"}, args...)...)
		n, _ := strconv.Atoi(out)
		err = errors.Join(err, e)
		return n
	}
	ahead = count(append([]string{"HEAD", "--not"}, refs...)...)
	behind = count(append(refs, "--not", "HEAD")...)
	return ahead, behind, err
}
