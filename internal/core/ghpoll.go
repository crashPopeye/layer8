package core

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/crashPopeye/layer8/internal/github"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/prompts"
	"github.com/crashPopeye/layer8/internal/store"
)

type promptVars = prompts.Vars

// renderPrompt fills the common variables for a slice prompt.
func renderPrompt(name string, t store.Task, x store.Slice, base string, edit func(*promptVars)) (string, error) {
	v := prompts.Vars{Worktree: x.WorktreePath, Branch: x.Branch, Base: base, Artifacts: t.ArtifactsPath,
		SliceID: x.Key, SliceTitle: x.Title, ParentID: x.ParentKey, PRNumber: x.PRNumber, PRURL: x.PRURL}
	if edit != nil {
		edit(&v)
	}
	return prompts.Render(name, v)
}

type repoRef struct{ owner, name string }

// PollGitHub refreshes PR, CI and review state for every slice of every active task.
func (a *App) PollGitHub(ctx context.Context) {
	if !a.pollMu.TryLock() {
		return
	}
	defer a.pollMu.Unlock()
	gh, err := a.github()
	if err != nil {
		return // no token yet; the UI shows how to connect
	}
	tasks, _ := a.Store.Tasks()
	repos := map[string]repoRef{}
	changed := false
	for _, t := range tasks {
		if t.Status != "active" || t.Branch == "" {
			continue
		}
		ws, err := a.Store.Workspace(t.WorkspaceID)
		if err != nil {
			continue
		}
		r, ok := repos[ws.ID]
		if !ok {
			owner, name, isGH := gitx.OriginRepo(ctx, ws.Path)
			if !isGH {
				continue
			}
			r = repoRef{owner, name}
			repos[ws.ID] = r
		}
		slices, _ := a.Store.TaskSlices(t.ID)
		for _, x := range slices {
			if x.PRState == "MERGED" || x.PRState == "CLOSED" {
				continue
			}
			if x.PRNumber == 0 {
				if !gitx.HasUpstream(ctx, x.WorktreePath) {
					continue
				}
				// The branch was pushed outside layer8; adopt its PR if there is one.
				pr, err := gh.FindPR(ctx, r.owner, r.name, x.Branch)
				if err != nil || pr == nil {
					continue
				}
				x.PRNumber, x.PRURL = pr.Number, pr.URL
			}
			st, err := gh.PRStatus(ctx, r.owner, r.name, x.PRNumber)
			if err != nil {
				log.Printf("PR status %s#%d: %v", r.name, x.PRNumber, err)
				continue
			}
			wasMerged := x.PRState != "MERGED" && st.State == "MERGED"
			err = a.Store.SetSlicePR(x.ID, store.SlicePR{Number: x.PRNumber, URL: st.URL, State: st.State, Draft: st.Draft, Base: st.Base,
				CIState: st.CIState, ReviewState: st.ReviewDecision, OpenThreads: len(st.OpenThreads), Mergeable: st.Mergeable})
			if err == nil {
				changed = true
			}
			if wasMerged && a.Config.Get().SyncIntervalMinutes > 0 {
				// Children move onto the merged parent's base right away (unless sync is off).
				go a.SyncTask(a.ctx, t.ID)
			}
		}
	}
	if changed {
		a.bus.Changed()
	}
}

// PRDetails returns the live PR status (checks and open threads) for the UI.
func (a *App) PRDetails(ctx context.Context, sliceID string) (*github.PRStatus, error) {
	x, t, r, gh, err := a.prContext(ctx, sliceID)
	if err != nil {
		return nil, err
	}
	_ = t
	return gh.PRStatus(ctx, r.owner, r.name, x.PRNumber)
}

func (a *App) prContext(ctx context.Context, sliceID string) (store.Slice, store.Task, repoRef, *github.Client, error) {
	x, err := a.Store.Slice(sliceID)
	if err != nil {
		return x, store.Task{}, repoRef{}, nil, err
	}
	t, err := a.Store.Task(x.TaskID)
	if err != nil {
		return x, t, repoRef{}, nil, err
	}
	if x.PRNumber == 0 {
		return x, t, repoRef{}, nil, errors.New("this slice has no pull request yet")
	}
	ws, err := a.Store.Workspace(t.WorkspaceID)
	if err != nil {
		return x, t, repoRef{}, nil, err
	}
	owner, name, ok := gitx.OriginRepo(ctx, ws.Path)
	if !ok {
		return x, t, repoRef{}, nil, errors.New("origin is not on GitHub")
	}
	gh, err := a.github()
	return x, t, repoRef{owner, name}, gh, err
}

// requireInSync refuses to start an agent on a branch that lacks commits GitHub has,
// since its push would then have to discard them.
func (a *App) requireInSync(ctx context.Context, x store.Slice) error {
	_, _ = gitx.Run(ctx, x.WorktreePath, "fetch", "--quiet", "origin", x.Branch)
	if gitx.RemoteDiverged(ctx, x.WorktreePath, x.Branch, x.LastPushedSHA) {
		return gitx.ErrDiverged
	}
	return nil
}

// FixCI starts a session with the failing checks' logs.
func (a *App) FixCI(ctx context.Context, sliceID string) (store.Session, error) {
	x, t, r, gh, err := a.prContext(ctx, sliceID)
	if err != nil {
		return store.Session{}, err
	}
	if err := a.requireInSync(ctx, x); err != nil {
		return store.Session{}, err
	}
	st, err := gh.PRStatus(ctx, r.owner, r.name, x.PRNumber)
	if err != nil {
		return store.Session{}, err
	}
	var b strings.Builder
	n := 0
	for _, c := range st.Checks {
		if !c.Failed() {
			continue
		}
		n++
		fmt.Fprintf(&b, "### %s (%s)\n%s\n", c.Name, strings.ToLower(c.Conclusion), c.URL)
		if c.JobID != 0 {
			if tail, err := gh.JobLogTail(ctx, r.owner, r.name, c.JobID, 150); err == nil {
				fmt.Fprintf(&b, "\nLast lines of the log:\n\n```\n%s\n```\n\n", tail)
			} else {
				fmt.Fprintf(&b, "\n(could not fetch the log: %v)\n\n", err)
			}
		} else {
			b.WriteString("\n(not a GitHub Actions job; open the link for details)\n\n")
		}
	}
	if n == 0 {
		return store.Session{}, errors.New("no failing checks on this PR right now")
	}
	prompt, err := renderPrompt("fix_ci", t, x, a.parentBranch(t, x), func(v *promptVars) { v.Extra = b.String() })
	if err != nil {
		return store.Session{}, err
	}
	return a.createSession(store.Session{TaskID: t.ID, SliceID: x.ID, Kind: "fix_ci", Title: x.Key + " fix CI", Cwd: x.WorktreePath}, prompt)
}

// AddressReview starts a session with the PR's unresolved review threads.
func (a *App) AddressReview(ctx context.Context, sliceID string) (store.Session, error) {
	x, t, r, gh, err := a.prContext(ctx, sliceID)
	if err != nil {
		return store.Session{}, err
	}
	if err := a.requireInSync(ctx, x); err != nil {
		return store.Session{}, err
	}
	st, err := gh.PRStatus(ctx, r.owner, r.name, x.PRNumber)
	if err != nil {
		return store.Session{}, err
	}
	if len(st.OpenThreads) == 0 {
		return store.Session{}, errors.New("no unresolved review threads on this PR")
	}
	var b strings.Builder
	for i, th := range st.OpenThreads {
		loc := th.Path
		if th.Line > 0 {
			loc = fmt.Sprintf("%s:%d", th.Path, th.Line)
		}
		if loc == "" {
			loc = "(general)"
		}
		if th.Outdated {
			loc += " (outdated)"
		}
		fmt.Fprintf(&b, "%d. `%s`\n", i+1, loc)
		for _, c := range th.Comments {
			fmt.Fprintf(&b, "   - **%s**: %s\n", c.Author, indent(strings.TrimSpace(c.Body), "     "))
		}
	}
	prompt, err := renderPrompt("review", t, x, a.parentBranch(t, x), func(v *promptVars) { v.Extra = b.String() })
	if err != nil {
		return store.Session{}, err
	}
	return a.createSession(store.Session{TaskID: t.ID, SliceID: x.ID, Kind: "review", Title: x.Key + " review", Cwd: x.WorktreePath}, prompt)
}

func indent(s, pad string) string {
	return strings.ReplaceAll(s, "\n", "\n"+pad)
}
