package core

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/store"
)

// SyncAll fetches every repo with active tasks and restacks every task and slice
// onto the latest default branch. Work in progress is never touched: worktrees with
// a running session or uncommitted changes are skipped, branches with commits that
// only exist on GitHub are flagged instead of rewritten, and conflicting rebases are
// aborted and reported.
func (a *App) SyncAll(ctx context.Context) {
	if !a.syncMu.TryLock() {
		return // a sync is already running
	}
	defer a.syncMu.Unlock()
	a.lastSync.Store(time.Now().UnixNano())
	tasks, _ := a.Store.Tasks()
	fetched := map[string]bool{}
	for _, t := range tasks {
		if t.Status != "active" || t.Branch == "" {
			continue
		}
		ws, err := a.Store.Workspace(t.WorkspaceID)
		if err != nil || !ws.IsGit {
			continue
		}
		if !fetched[ws.ID] {
			if err := gitx.Fetch(ctx, ws.Path); err != nil {
				log.Printf("fetch %s: %v", ws.Path, err)
			}
			fetched[ws.ID] = true
		}
		a.syncTask(ctx, t)
	}
	a.bus.Changed()
}

func (a *App) SyncTask(ctx context.Context, taskID string) error {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return err
	}
	ws, err := a.Store.Workspace(t.WorkspaceID)
	if err != nil {
		return err
	}
	if !ws.IsGit {
		return errors.New("not a git workspace")
	}
	if err := gitx.Fetch(ctx, ws.Path); err != nil {
		return err
	}
	a.syncTask(ctx, t)
	a.bus.Changed()
	return nil
}

func (a *App) syncTask(ctx context.Context, t store.Task) {
	slices, _ := a.Store.TaskSlices(t.ID)
	ownSlice := false
	for _, x := range slices {
		if x.WorktreePath == t.WorktreePath {
			ownSlice = true
		}
	}
	if !ownSlice {
		r := a.rebaseWorktree(ctx, t.WorktreePath, t.Branch, t.BaseRef, t.BaseSHA, "", false)
		_ = a.Store.SetTaskSync(t.ID, r.state, r.msg, r.base)
	}
	// Slices are stored in dependency order, so parents move before their children.
	push := a.Config.Get().PushAfterSync
	for _, x := range slices {
		if fresh, err := a.Store.Slice(x.ID); err == nil {
			a.syncSlice(ctx, t, fresh, push)
		}
	}
}

// syncSlice rebases one slice onto its parent and retargets its PR when the parent merged.
func (a *App) syncSlice(ctx context.Context, t store.Task, x store.Slice, push bool) {
	if x.PRState == "MERGED" || x.PRState == "CLOSED" {
		return
	}
	r := a.rebaseWorktree(ctx, x.WorktreePath, x.Branch, a.parentRef(t, x), x.BaseSHA, x.LastPushedSHA, push)
	_ = a.Store.SetSliceSync(x.ID, r.state, r.msg)
	if r.base != "" {
		_ = a.Store.SetSliceBase(x.ID, r.base)
	}
	if r.pushed != "" {
		_ = a.Store.SetSliceLastPushed(x.ID, r.pushed)
	}
	if x.PRNumber > 0 && x.PRState == "OPEN" {
		want := a.parentBranch(t, x)
		if x.PRBase != "" && x.PRBase != want {
			if err := a.retargetPR(ctx, t, x, want); err != nil {
				_ = a.Store.SetSliceSync(x.ID, "error", "retarget PR to "+want+": "+err.Error())
			}
		}
	}
}

func (a *App) retargetPR(ctx context.Context, t store.Task, x store.Slice, base string) error {
	ws, err := a.Store.Workspace(t.WorkspaceID)
	if err != nil {
		return err
	}
	owner, repo, ok := gitx.OriginRepo(ctx, ws.Path)
	if !ok {
		return errors.New("origin is not on GitHub")
	}
	gh, err := a.github()
	if err != nil {
		return err
	}
	if err := gh.SetPRBase(ctx, owner, repo, x.PRNumber, base); err != nil {
		return err
	}
	return a.Store.SetSlicePRBase(x.ID, base)
}

type rebaseResult struct {
	state, msg string
	base       string // new base SHA ("" = unchanged)
	pushed     string // SHA pushed to origin ("" = not pushed)
}

// rebaseWorktree moves the commits after oldBase onto parentRef, and pushes when asked
// and the branch is already on GitHub.
func (a *App) rebaseWorktree(ctx context.Context, dir, branch, parentRef, oldBase, lastPushed string, push bool) rebaseResult {
	unlock := a.lock(dir)
	defer unlock()
	if a.worktreeBusy(dir) {
		return rebaseResult{state: "busy", msg: "a session is working here; will sync when it finishes"}
	}
	if gitx.CurrentBranch(ctx, dir) == "" {
		return rebaseResult{state: "error", msg: "the worktree is not on a branch (detached HEAD or rebase in progress)"}
	}
	if branch != "" && gitx.RemoteDiverged(ctx, dir, branch, lastPushed) {
		return rebaseResult{state: "diverged", msg: gitx.ErrDiverged.Error()}
	}
	dirty, err := gitx.Dirty(ctx, dir)
	if err != nil {
		return rebaseResult{state: "error", msg: err.Error()}
	}
	tip, err := gitx.RevParse(ctx, dir, parentRef)
	if err != nil {
		return rebaseResult{state: "error", msg: "can't resolve " + parentRef}
	}
	if gitx.IsAncestor(ctx, dir, tip, "HEAD") {
		// Up to date; the branch's own work starts at the parent's tip, even if it
		// was rebased by hand.
		return rebaseResult{state: "ok", base: tip}
	}
	if dirty {
		return rebaseResult{state: "dirty", msg: "behind " + shortRef(parentRef) + "; commit or stash changes to sync"}
	}
	if oldBase == "" || !gitx.IsAncestor(ctx, dir, oldBase, "HEAD") {
		if oldBase, err = gitx.MergeBase(ctx, dir, tip, "HEAD"); err != nil {
			return rebaseResult{state: "error", msg: err.Error()}
		}
	}
	before, _ := gitx.RevParse(ctx, dir, "HEAD")
	rctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	if err := gitx.RebaseOnto(rctx, dir, tip, oldBase); err != nil {
		if errors.Is(err, gitx.ErrConflict) {
			return rebaseResult{state: "conflict", msg: "conflicts with " + shortRef(parentRef) + "; use Resolve conflicts"}
		}
		return rebaseResult{state: "error", msg: firstLine(err.Error(), 200)}
	}
	res := rebaseResult{state: "ok", msg: "rebased onto " + shortRef(parentRef), base: tip}
	if push && branch != "" && gitx.HasUpstream(ctx, dir) {
		sha, err := gitx.SafePush(ctx, dir, branch, lastPushed, before)
		switch {
		case errors.Is(err, gitx.ErrDiverged):
			res.state, res.msg = "diverged", err.Error()
		case err != nil:
			res.state, res.msg = "error", "rebased, but push failed: "+firstLine(err.Error(), 160)
		default:
			res.pushed = sha
		}
	}
	return res
}

func shortRef(ref string) string { return strings.TrimPrefix(ref, "origin/") }

// ResolveConflicts hands a conflicting rebase to an agent.
func (a *App) ResolveConflicts(ctx context.Context, sliceID string) (store.Session, error) {
	x, err := a.Store.Slice(sliceID)
	if err != nil {
		return store.Session{}, err
	}
	t, err := a.Store.Task(x.TaskID)
	if err != nil {
		return store.Session{}, err
	}
	ref := a.parentRef(t, x)
	old := x.BaseSHA
	if old == "" {
		old, _ = gitx.MergeBase(ctx, x.WorktreePath, ref, "HEAD")
	}
	prompt, err := renderPrompt("conflict", t, x, a.parentBranch(t, x), func(v *promptVars) { v.Base, v.OldBase = ref, old })
	if err != nil {
		return store.Session{}, err
	}
	return a.createSession(store.Session{TaskID: t.ID, SliceID: x.ID, Kind: "conflict", Title: x.Key + " conflicts", Cwd: x.WorktreePath}, prompt)
}
