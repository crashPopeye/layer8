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
// a running session or uncommitted changes are skipped, and conflicting rebases are
// aborted and reported.
func (a *App) SyncAll(ctx context.Context) {
	if !a.syncMu.TryLock() {
		return // a sync is already running
	}
	defer a.syncMu.Unlock()
	a.lastSync = time.Now()
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
		state, msg, base := a.rebaseWorktree(ctx, t.WorktreePath, t.BaseRef, t.BaseSHA, false)
		_ = a.Store.SetTaskSync(t.ID, state, msg, base)
	}
	// Slices are stored in dependency order, so parents move before their children.
	push := a.Config.Get().PushAfterSync
	for _, x := range slices {
		x, _ = a.Store.Slice(x.ID) // pick up changes made by earlier iterations
		a.syncSlice(ctx, t, x, push)
	}
}

// syncSlice rebases one slice onto its parent and retargets its PR when the parent merged.
func (a *App) syncSlice(ctx context.Context, t store.Task, x store.Slice, push bool) {
	if x.PRState == "MERGED" || x.PRState == "CLOSED" {
		return
	}
	ref := a.parentRef(t, x)
	state, msg, base := a.rebaseWorktree(ctx, x.WorktreePath, ref, x.BaseSHA, push)
	x, _ = a.Store.Slice(x.ID)
	x.SyncState, x.SyncMessage = state, msg
	if base != "" {
		x.BaseSHA = base
	}
	_ = a.Store.UpdateSlice(x)

	if x.PRNumber > 0 && x.PRState == "OPEN" {
		want := a.parentBranch(t, x)
		if x.PRBase != "" && x.PRBase != want {
			if err := a.retargetPR(ctx, t, x, want); err != nil {
				a.setSliceSync(x.ID, "error", "retarget PR to "+want+": "+err.Error())
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
	x, _ = a.Store.Slice(x.ID)
	x.PRBase = base
	return a.Store.UpdateSlice(x)
}

// rebaseWorktree moves the commits after oldBase onto parentRef. It returns the sync
// state, a message, and the new base SHA ("" if unchanged).
func (a *App) rebaseWorktree(ctx context.Context, dir, parentRef, oldBase string, push bool) (state, msg, newBase string) {
	unlock := a.lock(dir)
	defer unlock()
	if a.worktreeBusy(dir) {
		return "busy", "a session is working here; will sync when it finishes", ""
	}
	if gitx.CurrentBranch(ctx, dir) == "" {
		return "error", "the worktree is not on a branch (detached HEAD or rebase in progress)", ""
	}
	dirty, err := gitx.Dirty(ctx, dir)
	if err != nil {
		return "error", err.Error(), ""
	}
	tip, err := gitx.RevParse(ctx, dir, parentRef)
	if err != nil {
		return "error", "can't resolve " + parentRef, ""
	}
	if gitx.IsAncestor(ctx, dir, tip, "HEAD") {
		if oldBase == "" {
			mb, _ := gitx.MergeBase(ctx, dir, tip, "HEAD")
			return "ok", "", mb
		}
		return "ok", "", ""
	}
	if dirty {
		return "dirty", "behind " + parentRef + "; commit or stash changes to sync", ""
	}
	if oldBase == "" || !gitx.IsAncestor(ctx, dir, oldBase, "HEAD") {
		if oldBase, err = gitx.MergeBase(ctx, dir, tip, "HEAD"); err != nil {
			return "error", err.Error(), ""
		}
	}
	if err := gitx.RebaseOnto(ctx, dir, tip, oldBase); err != nil {
		if errors.Is(err, gitx.ErrConflict) {
			return "conflict", "conflicts with " + parentRef + "; use Resolve conflicts", ""
		}
		return "error", firstLine(err.Error(), 200), ""
	}
	if push && gitx.HasUpstream(ctx, dir) {
		if err := gitx.Push(ctx, dir, gitx.CurrentBranch(ctx, dir), true); err != nil {
			return "error", "rebased, but push failed: " + firstLine(err.Error(), 160), tip
		}
	}
	return "ok", "rebased onto " + shortRef(parentRef), tip
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
