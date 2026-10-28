package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/crashPopeye/layer8/internal/diff"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/store"
)

// md renders untrusted markdown: goldmark drops raw HTML and dangerous link
// schemes unless WithUnsafe is set, which we never do.
var md = goldmark.New(goldmark.WithExtensions(extension.GFM))

func Markdown(src string) string {
	var b bytes.Buffer
	if err := md.Convert([]byte(src), &b); err != nil {
		return ""
	}
	return b.String()
}

// reviewTarget is the worktree and comparison base for a task or slice.
func (a *App) reviewTarget(taskID, sliceID string) (dir, base string, err error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return "", "", err
	}
	if t.Branch == "" {
		return "", "", errors.New("not a git folder")
	}
	ref := t.BaseRef
	dir = t.WorktreePath
	if sliceID != "" {
		x, err := a.Store.Slice(sliceID)
		if err != nil {
			return "", "", err
		}
		dir, ref = x.WorktreePath, a.parentRef(t, x)
	}
	return dir, ref, nil
}

type DiffResult struct {
	Base      string        `json:"base"`
	Files     []diff.File   `json:"files"`
	Untracked []string      `json:"untracked"`
	Commits   []gitx.Commit `json:"commits"`
	Commit    *gitx.Commit  `json:"commit,omitempty"`
	Dirty     bool          `json:"dirty"` // uncommitted changes in the worktree
}

// Diff returns everything on the branch since it left its parent (including
// uncommitted changes), or one commit when sha is set.
func (a *App) Diff(ctx context.Context, taskID, sliceID, sha string) (*DiffResult, error) {
	dir, ref, err := a.reviewTarget(taskID, sliceID)
	if err != nil {
		return nil, err
	}
	mb, err := gitx.MergeBase(ctx, dir, ref, "HEAD")
	if err != nil {
		return nil, fmt.Errorf("compare with %s: %w", ref, err)
	}
	res := &DiffResult{Base: ref}
	res.Commits, err = gitx.Log(ctx, dir, mb, "HEAD") // never nil: the UI maps over it
	if err != nil {
		return nil, err
	}
	var out string
	if sha != "" {
		found := false
		for i := range res.Commits {
			if res.Commits[i].SHA == sha {
				res.Commit, found = &res.Commits[i], true
			}
		}
		if !found {
			return nil, errors.New("that commit is not on this branch")
		}
		out, err = gitx.RunRaw(ctx, dir, "show", "--format=", "--no-color", "--no-ext-diff", "-M", sha)
	} else {
		out, err = gitx.RunRaw(ctx, dir, "diff", "--no-color", "--no-ext-diff", "-M", mb)
		if err == nil {
			res.Untracked, _ = gitx.UntrackedFiles(ctx, dir)
			res.Dirty, _ = gitx.Dirty(ctx, dir)
		}
	}
	if err != nil {
		return nil, err
	}
	res.Files = diff.Parse(out)
	if res.Files == nil {
		res.Files = []diff.File{}
	}
	return res, nil
}

// Commit stages everything in the worktree and commits it.
func (a *App) Commit(ctx context.Context, taskID, sliceID, message string) (string, error) {
	dir, _, err := a.reviewTarget(taskID, sliceID)
	if err != nil {
		return "", err
	}
	if a.worktreeBusy(dir) {
		return "", errors.New("a session is working here; wait for it to finish")
	}
	unlock := a.lock(dir)
	defer unlock()
	sha, err := gitx.CommitAll(ctx, dir, message)
	a.bus.Changed()
	return sha, err
}

// ---- comments

func (a *App) AddComment(c store.Comment) (store.Comment, error) {
	if strings.TrimSpace(c.Body) == "" {
		return c, errors.New("empty comment")
	}
	if c.Target != "artifact" && c.Target != "diff" {
		return c, errors.New("comments go on an artifact or a diff")
	}
	if _, err := a.Store.Task(c.TaskID); err != nil {
		return c, err
	}
	c, err := a.Store.InsertComment(c)
	a.bus.Changed()
	return c, err
}

func (a *App) DeleteComment(id string) error {
	err := a.Store.DeleteComment(id)
	a.bus.Changed()
	return err
}

// SendComments delivers the open doc comments of a task, plus the diff comments of
// the given slice (or of the task branch when sliceID is ""), to a session:
// the given one, else the latest idle session in that place, else a new one.
func (a *App) SendComments(ctx context.Context, taskID, sliceID, sessionID string) (store.Session, error) {
	all, err := a.Store.Comments(taskID)
	if err != nil {
		return store.Session{}, err
	}
	var open []store.Comment
	for _, c := range all {
		// Doc comments go with any session of the task; diff comments only to their own PR.
		if c.Status == "open" && (c.Target == "artifact" || c.SliceID == sliceID) {
			open = append(open, c)
		}
	}
	if len(open) == 0 {
		return store.Session{}, errors.New("no open comments to send")
	}
	t, err := a.Store.Task(taskID)
	if err != nil {
		return store.Session{}, err
	}
	var b strings.Builder
	for i, c := range open {
		where := c.Path
		if c.Line > 0 {
			where = fmt.Sprintf("%s:%d", c.Path, c.Line)
		}
		if c.Target == "artifact" {
			where = t.ArtifactsPath + "/" + where
		}
		fmt.Fprintf(&b, "%d. `%s`\n", i+1, where)
		if q := strings.TrimSpace(c.Quote); q != "" {
			fmt.Fprintf(&b, "   > %s\n", indent(q, "   > "))
		}
		fmt.Fprintf(&b, "   %s\n", indent(strings.TrimSpace(c.Body), "   "))
	}
	text, err := renderPrompt("comments", t, store.Slice{}, t.BaseRef, func(v *promptVars) { v.Extra = b.String() })
	if err != nil {
		return store.Session{}, err
	}

	var target store.Session
	if sessionID != "" {
		if target, err = a.Store.Session(sessionID); err != nil {
			return target, err
		}
	} else {
		for _, s := range a.taskSessions(taskID) {
			if s.SliceID == sliceID && !a.Sessions.Busy(s.ID) && s.ClaudeSessionID != "" {
				target = s // latest wins: sessions are ordered by creation
			}
		}
	}
	ids := make([]string, len(open))
	for i, c := range open {
		ids[i] = c.ID
	}
	if target.ID != "" {
		if err := a.SendMessage(target.ID, text); err != nil {
			return target, err
		}
	} else {
		cwd := t.WorktreePath
		if sliceID != "" {
			x, err := a.Store.Slice(sliceID)
			if err != nil {
				return target, err
			}
			cwd = x.WorktreePath
		}
		target, err = a.createSession(store.Session{TaskID: taskID, SliceID: sliceID, Kind: "comments", Title: "review comments", Cwd: cwd}, text)
		if err != nil {
			return target, err
		}
	}
	err = a.Store.MarkCommentsSent(ids)
	a.bus.Changed()
	return target, err
}
