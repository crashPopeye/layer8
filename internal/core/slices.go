package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/linear"
	"github.com/crashPopeye/layer8/internal/plan"
	"github.com/crashPopeye/layer8/internal/prompts"
	"github.com/crashPopeye/layer8/internal/store"
)

// parentSlice returns the slice x is stacked on, skipping merged ancestors, or nil for the task base.
func (a *App) parentSlice(x store.Slice) *store.Slice {
	key := x.ParentKey
	for key != "" {
		slices, _ := a.Store.TaskSlices(x.TaskID)
		var p *store.Slice
		for i := range slices {
			if slices[i].Key == key {
				p = &slices[i]
			}
		}
		if p == nil {
			return nil
		}
		if p.PRState != "MERGED" {
			return p
		}
		key = p.ParentKey
	}
	return nil
}

// parentRef is the git ref x is rebased onto: a parent slice's branch or the task base (origin/main).
func (a *App) parentRef(t store.Task, x store.Slice) string {
	if p := a.parentSlice(x); p != nil {
		return p.Branch
	}
	return t.BaseRef
}

// parentBranch is the branch name a PR targets on GitHub.
func (a *App) parentBranch(t store.Task, x store.Slice) string {
	if p := a.parentSlice(x); p != nil {
		return p.Branch
	}
	return strings.TrimPrefix(t.BaseRef, "origin/")
}

// CreateSlices turns the plan's `prs:` block into stacked branches and worktrees.
// It is idempotent: PRs that already have a slice are left alone.
func (a *App) CreateSlices(ctx context.Context, taskID string) ([]store.Slice, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return nil, err
	}
	ws, err := a.Store.Workspace(t.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !ws.IsGit || t.Branch == "" {
		return nil, errors.New("PR slices need a git workspace")
	}
	md, err := os.ReadFile(filepath.Join(t.ArtifactsPath, "plan.md"))
	if err != nil {
		return nil, errors.New("no plan.md yet: run the Plan workflow first")
	}
	prs, err := plan.Parse(string(md))
	if err != nil {
		return nil, err
	}
	existing, _ := a.Store.TaskSlices(taskID)
	have := map[string]store.Slice{}
	for _, x := range existing {
		have[x.Key] = x
	}
	var todo []plan.PR
	for _, p := range prs {
		if _, ok := have[p.ID]; !ok {
			todo = append(todo, p)
		}
	}
	if len(todo) == 0 {
		return existing, nil
	}

	cfg := a.Config.Get()
	// Linear first, so a Linear failure creates nothing on disk.
	subIssues := map[string]*linear.IssueSummary{}
	if cfg.CreateLinearSubIssues && t.Linear != nil && cfg.LinearAPIKey != "" {
		lc := linear.New(cfg.LinearAPIKey)
		parent, err := lc.Issue(ctx, t.Linear.ID)
		if err != nil {
			return nil, fmt.Errorf("Linear sub-issues: %w", err)
		}
		for _, p := range todo {
			desc := fmt.Sprintf("Part of %s. Pull request `%s` of the plan, created by layer8.", t.Linear.Identifier, p.ID)
			sub, err := lc.CreateSubIssue(ctx, parent, p.Title, desc)
			if err != nil {
				return nil, fmt.Errorf("create Linear sub-issue for %s: %w", p.ID, err)
			}
			subIssues[p.ID] = sub
		}
	}

	_ = gitx.Fetch(ctx, ws.Path)
	pos := len(existing)
	for _, p := range todo {
		x := store.Slice{TaskID: t.ID, Key: p.ID, Title: p.Title, Position: pos, DependsOn: p.DependsOn, ParentKey: p.Parent()}
		pos++
		if sub := subIssues[p.ID]; sub != nil {
			x.Linear = &store.LinearRef{ID: sub.ID, Identifier: sub.Identifier, URL: sub.URL, BranchName: sub.BranchName}
			x.Branch = gitx.SanitizeBranch(sub.BranchName)
		} else {
			x.Branch = gitx.SanitizeBranch(t.Branch + "-" + p.ID)
		}
		base := t.BaseRef
		if x.ParentKey != "" {
			parent, ok := have[x.ParentKey]
			if !ok {
				return nil, fmt.Errorf("slice %s: parent %s was not created", p.ID, x.ParentKey)
			}
			base = parent.Branch
		}
		x.WorktreePath = filepath.Join(cfg.WorktreeRoot, ws.Name, t.Slug+"--"+p.ID)
		if err := gitx.AddWorktree(ctx, ws.Path, x.WorktreePath, x.Branch, base); err != nil {
			return nil, fmt.Errorf("worktree for %s: %w", p.ID, err)
		}
		x.BaseSHA, _ = gitx.MergeBase(ctx, x.WorktreePath, base, "HEAD")
		x.TmuxSession = a.ensureTmux(ctx, t.Slug+"--"+p.ID, x.WorktreePath, map[string]string{
			"LAYER8_TASK": t.Slug, "LAYER8_SLICE": p.ID, "LAYER8_ARTIFACTS": t.ArtifactsPath,
		})
		x.SyncState = "ok"
		saved, err := a.Store.InsertSlice(x)
		if err != nil {
			return nil, err
		}
		have[p.ID] = saved
	}
	a.bus.Changed()
	return a.Store.TaskSlices(taskID)
}

// CreateSingleSlice tracks the task's own branch as one PR, for work that doesn't need splitting.
func (a *App) CreateSingleSlice(ctx context.Context, taskID string) (store.Slice, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return store.Slice{}, err
	}
	if t.Branch == "" {
		return store.Slice{}, errors.New("PRs need a git workspace")
	}
	existing, _ := a.Store.TaskSlices(taskID)
	for _, x := range existing {
		if x.Branch == t.Branch {
			return x, nil
		}
	}
	x := store.Slice{TaskID: t.ID, Key: "pr", Title: t.Title, Position: len(existing), Branch: t.Branch,
		WorktreePath: t.WorktreePath, BaseSHA: t.BaseSHA, TmuxSession: t.TmuxSession, Linear: t.Linear, SyncState: "ok"}
	x, err = a.Store.InsertSlice(x)
	a.bus.Changed()
	return x, err
}

type StartSession struct {
	TaskID  string `json:"taskId"`
	SliceID string `json:"sliceId"`
	Kind    string `json:"kind"` // research, plan, implement, slice, chat
	Prompt  string `json:"prompt"`
}

func (a *App) StartSession(ctx context.Context, in StartSession) (store.Session, error) {
	t, err := a.Store.Task(in.TaskID)
	if err != nil {
		return store.Session{}, err
	}
	v := prompts.Vars{Worktree: t.WorktreePath, Branch: t.Branch, Base: t.BaseRef, Artifacts: t.ArtifactsPath, Extra: strings.TrimSpace(in.Prompt)}
	cwd := t.WorktreePath
	title := in.Kind
	var x store.Slice
	if in.SliceID != "" {
		if x, err = a.Store.Slice(in.SliceID); err != nil {
			return store.Session{}, err
		}
		cwd, v.Worktree, v.Branch = x.WorktreePath, x.WorktreePath, x.Branch
		v.SliceID, v.SliceTitle, v.ParentID = x.Key, x.Title, x.ParentKey
		v.Base = a.parentBranch(t, x)
		title = x.Key + " " + in.Kind
		if in.Kind == "implement" {
			in.Kind = "slice"
		}
	}
	var prompt string
	switch in.Kind {
	case "chat":
		prompt = strings.TrimSpace(in.Prompt)
		if prompt == "" {
			return store.Session{}, errors.New("type a message to start a chat")
		}
		title = firstLine(prompt, 48)
	case "research", "plan", "implement":
		if prompt, err = prompts.Render(in.Kind, v); err != nil {
			return store.Session{}, err
		}
	case "slice":
		if in.SliceID == "" {
			return store.Session{}, errors.New("pick a PR slice")
		}
		// Start from the parent's latest commits so the handoff matches the code.
		a.syncSlice(ctx, t, x, false)
		if prompt, err = prompts.Render("slice", v); err != nil {
			return store.Session{}, err
		}
	default:
		return store.Session{}, fmt.Errorf("unknown workflow %q", in.Kind)
	}
	return a.createSession(store.Session{TaskID: t.ID, SliceID: in.SliceID, Kind: in.Kind, Title: title, Cwd: cwd}, prompt)
}

func (a *App) createSession(s store.Session, prompt string) (store.Session, error) {
	unlock := a.lock(s.Cwd)
	defer unlock()
	return a.Sessions.Create(s, prompt)
}

// SendMessage continues a session; holding the worktree lock keeps it from racing a rebase.
func (a *App) SendMessage(sessionID, text string) error {
	s, err := a.Store.Session(sessionID)
	if err != nil {
		return err
	}
	unlock := a.lock(s.Cwd)
	defer unlock()
	return a.Sessions.Send(sessionID, text)
}

func firstLine(s string, n int) string {
	s = strings.TrimSpace(strings.SplitN(s, "\n", 2)[0])
	if len([]rune(s)) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}

// worktreeBusy reports whether any session is working in path.
func (a *App) worktreeBusy(path string) bool {
	all, _ := a.Store.Sessions()
	for _, s := range all {
		if s.Cwd == path && a.Sessions.Busy(s.ID) {
			return true
		}
	}
	return false
}

// onTurnEnd chains work: pushes after CI/review/conflict fixes, and starts slices
// whose dependencies are done.
func (a *App) onTurnEnd(s store.Session, ok bool) {
	if !ok || s.SliceID == "" {
		return
	}
	ctx := a.ctx
	x, err := a.Store.Slice(s.SliceID)
	if err != nil {
		return
	}
	t, err := a.Store.Task(s.TaskID)
	if err != nil {
		return
	}
	switch s.Kind {
	case "fix_ci", "review", "conflict":
		if s.Kind == "conflict" {
			if tip, err := gitx.RevParse(ctx, x.WorktreePath, a.parentRef(t, x)); err == nil && gitx.IsAncestor(ctx, x.WorktreePath, tip, "HEAD") {
				x.BaseSHA, x.SyncState, x.SyncMessage = tip, "ok", ""
				_ = a.Store.UpdateSlice(x)
			}
		}
		if x.PRNumber > 0 {
			if err := a.push(ctx, x); err != nil {
				a.setSliceSync(x.ID, "error", "push after "+s.Kind+": "+err.Error())
			}
		}
	case "slice":
		if a.Config.Get().AutoStartSlices {
			a.startReadySlices(ctx, t)
		}
	}
	a.bus.Changed()
}

// startReadySlices starts each slice that has no session yet and whose dependencies
// all have commits and are idle.
func (a *App) startReadySlices(ctx context.Context, t store.Task) {
	slices, _ := a.Store.TaskSlices(t.ID)
	byKey := map[string]store.Slice{}
	for _, x := range slices {
		byKey[x.Key] = x
	}
	sessionsBySlice := map[string]int{}
	for _, s := range a.taskSessions(t.ID) {
		if s.SliceID != "" {
			sessionsBySlice[s.SliceID]++
		}
	}
	for _, x := range slices {
		if sessionsBySlice[x.ID] > 0 || len(x.DependsOn) == 0 {
			continue
		}
		ready := true
		for _, dep := range x.DependsOn {
			d, ok := byKey[dep]
			if !ok || a.worktreeBusy(d.WorktreePath) || !a.hasCommits(ctx, t, d) {
				ready = false
				break
			}
		}
		if ready {
			if _, err := a.StartSession(ctx, StartSession{TaskID: t.ID, SliceID: x.ID, Kind: "slice"}); err != nil {
				a.setSliceSync(x.ID, "error", "auto-start: "+err.Error())
			}
		}
	}
}

func (a *App) hasCommits(ctx context.Context, t store.Task, x store.Slice) bool {
	if x.PRState == "MERGED" {
		return true
	}
	commits, err := gitx.Log(ctx, x.WorktreePath, a.parentRef(t, x), "HEAD")
	return err == nil && len(commits) > 0
}

func (a *App) setSliceSync(id, state, msg string) {
	x, err := a.Store.Slice(id)
	if err != nil {
		return
	}
	x.SyncState, x.SyncMessage = state, msg
	_ = a.Store.UpdateSlice(x)
	a.bus.Changed()
}

// push sends the slice branch to origin; branches we rebase are force-pushed with lease.
func (a *App) push(ctx context.Context, x store.Slice) error {
	unlock := a.lock(x.WorktreePath)
	defer unlock()
	return gitx.Push(ctx, x.WorktreePath, x.Branch, gitx.HasUpstream(ctx, x.WorktreePath))
}

// PushSlice pushes without touching the PR.
func (a *App) PushSlice(ctx context.Context, sliceID string) error {
	x, err := a.Store.Slice(sliceID)
	if err != nil {
		return err
	}
	if a.worktreeBusy(x.WorktreePath) {
		return errors.New("a session is working in this slice; wait for it to finish")
	}
	return a.push(ctx, x)
}

// OpenPR pushes the slice and opens (or finds) its pull request against its parent.
func (a *App) OpenPR(ctx context.Context, sliceID string) (store.Slice, error) {
	x, err := a.Store.Slice(sliceID)
	if err != nil {
		return x, err
	}
	t, err := a.Store.Task(x.TaskID)
	if err != nil {
		return x, err
	}
	ws, err := a.Store.Workspace(t.WorkspaceID)
	if err != nil {
		return x, err
	}
	owner, repo, ok := gitx.OriginRepo(ctx, ws.Path)
	if !ok {
		return x, errors.New("origin is not a GitHub repository")
	}
	if a.worktreeBusy(x.WorktreePath) {
		return x, errors.New("a session is working in this slice; wait for it to finish")
	}
	if p := a.parentSlice(x); p != nil && p.PRNumber == 0 && !gitx.HasUpstream(ctx, p.WorktreePath) {
		return x, fmt.Errorf("open the PR for %s first: this PR is stacked on it", p.Key)
	}
	if !a.hasCommits(ctx, t, x) {
		return x, errors.New("this branch has no commits yet")
	}
	gh, err := a.github()
	if err != nil {
		return x, err
	}
	if err := a.push(ctx, x); err != nil {
		return x, err
	}
	pr, err := gh.FindPR(ctx, owner, repo, x.Branch)
	if err != nil {
		return x, err
	}
	if pr == nil || pr.State == "closed" {
		title := x.Title
		if x.Linear != nil {
			title = x.Linear.Identifier + ": " + title
		} else if t.Linear != nil {
			title = t.Linear.Identifier + ": " + title
		}
		pr, err = gh.CreatePR(ctx, owner, repo, x.Branch, a.parentBranch(t, x), title, a.prBody(t, x), a.Config.Get().DraftPRs)
		if err != nil {
			return x, err
		}
	}
	x.PRNumber, x.PRURL, x.PRDraft, x.PRState = pr.Number, pr.URL, pr.Draft, "OPEN"
	x.PRBase = pr.Base.Ref
	if err := a.Store.UpdateSlice(x); err != nil {
		return x, err
	}
	a.bus.Changed()
	go a.PollGitHub(a.ctx)
	return x, nil
}

func (a *App) prBody(t store.Task, x store.Slice) string {
	var b strings.Builder
	if x.Linear != nil {
		fmt.Fprintf(&b, "Linear: [%s](%s)\n", x.Linear.Identifier, x.Linear.URL)
	}
	if t.Linear != nil {
		fmt.Fprintf(&b, "Part of [%s](%s): %s\n", t.Linear.Identifier, t.Linear.URL, t.Title)
	}
	if handoff, err := os.ReadFile(filepath.Join(t.ArtifactsPath, "handoff-"+x.Key+".md")); err == nil {
		fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(string(handoff)))
	}
	slices, _ := a.Store.TaskSlices(t.ID)
	if len(slices) > 1 {
		b.WriteString("\n### Stack\n\n")
		for _, s := range slices {
			mark := ""
			if s.ID == x.ID {
				mark = " ← this PR"
			}
			ref := s.Branch
			if s.PRNumber > 0 {
				ref = fmt.Sprintf("#%d", s.PRNumber)
			}
			fmt.Fprintf(&b, "- %s: %s (%s)%s\n", s.Key, s.Title, ref, mark)
		}
	}
	return b.String()
}
