package core

import (
	"context"
	"slices"

	"github.com/crashPopeye/layer8/internal/store"
)

var (
	taskKinds = []string{"research", "plan", "implement"}
	targets   = []string{"terminal", "editor"}
	themes    = []string{"system", "dark", "light"}
)

func busy(status string) bool {
	return slices.Contains([]string{"queued", "running", "waiting_approval"}, status)
}

// declareVerbs is the one list of everything that can be done to layer8.
func (a *App) declareVerbs() []*Verb {
	sliceID := func(n *Node) string {
		if n.slice != nil {
			return n.slice.ID
		}
		return ""
	}
	run := func(ctx context.Context, n *Node, in Args) (any, error) {
		kind := in.str("kind")
		if kind == "" {
			kind = "chat"
		}
		return a.StartSession(ctx, StartSession{TaskID: n.task.ID, SliceID: sliceID(n), Kind: kind, Prompt: in.str("prompt")})
	}
	open := func(ctx context.Context, n *Node, in Args) (any, error) {
		if in.str("target") == "editor" {
			return nil, a.OpenEditor(n.task.ID, sliceID(n))
		}
		return nil, a.OpenTerminal(ctx, n.task.ID, sliceID(n))
	}
	show := func(ctx context.Context, n *Node, in Args) (any, error) {
		return a.toWindow("show", n, in)
	}
	isGit := func(n *Node) bool { return n.task.Branch != "" }
	hasPR := func(n *Node) bool { return n.slice.PRNumber > 0 }
	panel := []Arg{{Name: "panel", Optional: true}}

	return []*Verb{
		{Kind: "app", Name: "new-task", Help: "Create a task in a folder, from a Linear issue or a title",
			Args: []Arg{{Name: "folder"}, {Name: "title", Optional: true}, {Name: "linear", Optional: true}},
			Run: func(ctx context.Context, n *Node, in Args) (any, error) {
				folders, err := a.Store.Folders()
				if err != nil {
					return nil, err
				}
				i := slices.IndexFunc(folders, func(f store.Folder) bool { return f.Name == in.str("folder") || f.ID == in.str("folder") })
				if i < 0 {
					return nil, bad("no folder " + in.str("folder"))
				}
				return a.CreateTask(ctx, NewTask{FolderID: folders[i].ID, Title: in.str("title"), LinearIssue: in.str("linear")})
			}},
		{Kind: "app", Name: "sync", Help: "Rebase every task onto its latest default branch",
			Run: func(ctx context.Context, n *Node, in Args) (any, error) {
				go a.SyncAll(a.ctx)
				return "sync started", nil
			}},
		{Kind: "app", Name: "show", Help: "Bring a panel to the front of the window", Args: []Arg{{Name: "panel"}}, Run: show},
		{Kind: "app", Name: "notify", Help: "Show the user a message in the window", Args: []Arg{{Name: "text"}},
			Run: func(ctx context.Context, n *Node, in Args) (any, error) { return a.toWindow("notify", n, in) }},
		{Kind: "app", Name: "theme", Help: "Switch the window's theme", Args: []Arg{{Name: "mode", Enum: themes}},
			Run: func(ctx context.Context, n *Node, in Args) (any, error) { return a.toWindow("theme", n, in) }},

		{Kind: "task", Name: "run", Help: "Start a research, plan or implement session", Args: []Arg{{Name: "kind", Enum: taskKinds}, {Name: "prompt", Optional: true}}, Run: run},
		{Kind: "task", Name: "chat", Help: "Start a free-form session with a first message", Args: []Arg{{Name: "prompt"}}, Run: run},
		{Kind: "task", Name: "split", Help: "Create one stacked branch per PR listed in plan.md",
			When: func(n *Node) bool { return isGit(n) && !hasSlices(n) },
			Run:  func(ctx context.Context, n *Node, in Args) (any, error) { return a.CreateSlices(ctx, n.task.ID) }},
		{Kind: "task", Name: "single-pr", Help: "Track the task's own branch as a single PR",
			When: func(n *Node) bool { return isGit(n) && !hasSlices(n) },
			Run:  func(ctx context.Context, n *Node, in Args) (any, error) { return a.CreateSingleSlice(ctx, n.task.ID) }},
		{Kind: "task", Name: "sync", Help: "Fetch and rebase the task's branches", When: isGit,
			Run: func(ctx context.Context, n *Node, in Args) (any, error) { return nil, a.SyncTask(ctx, n.task.ID) }},
		{Kind: "task", Name: "open", Help: "Open the worktree in a terminal or editor", Args: []Arg{{Name: "target", Enum: targets}}, Run: open},
		{Kind: "task", Name: "archive", Help: "Archive the task, keeping branches and worktrees",
			Run: func(ctx context.Context, n *Node, in Args) (any, error) {
				return nil, a.ArchiveTask(ctx, n.task.ID, false)
			}},
		{Kind: "task", Name: "show", Help: "Focus the task in the window", Args: panel, Run: show},

		{Kind: "slice", Name: "implement", Help: "Start a session that implements this PR", Args: []Arg{{Name: "prompt", Optional: true}},
			Run: func(ctx context.Context, n *Node, in Args) (any, error) {
				return run(ctx, n, Args{"kind": "implement", "prompt": in["prompt"]})
			}},
		{Kind: "slice", Name: "chat", Help: "Start a free-form session in this PR's worktree", Args: []Arg{{Name: "prompt"}}, Run: run},
		{Kind: "slice", Name: "pr", Help: "Push and open the pull request", When: func(n *Node) bool { return !hasPR(n) },
			Run: func(ctx context.Context, n *Node, in Args) (any, error) { return a.OpenPR(ctx, n.slice.ID) }},
		{Kind: "slice", Name: "push", Help: "Push the branch", When: hasPR,
			Run: func(ctx context.Context, n *Node, in Args) (any, error) { return nil, a.PushSlice(ctx, n.slice.ID) }},
		{Kind: "slice", Name: "fix-ci", Help: "Start a session that fixes the failing checks",
			When: func(n *Node) bool { return hasPR(n) && n.slice.CIState == "FAILURE" },
			Run:  func(ctx context.Context, n *Node, in Args) (any, error) { return a.FixCI(ctx, n.slice.ID) }},
		{Kind: "slice", Name: "review", Help: "Start a session that addresses review comments", When: hasPR,
			Run: func(ctx context.Context, n *Node, in Args) (any, error) { return a.AddressReview(ctx, n.slice.ID) }},
		{Kind: "slice", Name: "resolve", Help: "Start a session that resolves rebase conflicts",
			When: func(n *Node) bool { return n.slice.SyncState == "conflict" },
			Run:  func(ctx context.Context, n *Node, in Args) (any, error) { return a.ResolveConflicts(ctx, n.slice.ID) }},
		{Kind: "slice", Name: "open", Help: "Open the PR's worktree in a terminal or editor", Args: []Arg{{Name: "target", Enum: targets}}, Run: open},
		{Kind: "slice", Name: "show", Help: "Focus the PR in the window", Args: panel, Run: show},

		{Kind: "session", Name: "say", Help: "Send the session a message", Args: []Arg{{Name: "text"}},
			Run: func(ctx context.Context, n *Node, in Args) (any, error) {
				return nil, a.SendMessage(n.session.ID, in.str("text"))
			}},
		{Kind: "session", Name: "stop", Help: "Interrupt the session's turn", When: func(n *Node) bool { return busy(n.session.Status) },
			Run: func(ctx context.Context, n *Node, in Args) (any, error) {
				return nil, a.Sessions.Interrupt(n.session.ID)
			}},
		{Kind: "session", Name: "show", Help: "Open the session's transcript in the window", Run: show},
	}
}

func hasSlices(n *Node) bool {
	return slices.ContainsFunc(n.Children, func(c *Node) bool { return c.Kind == "slice" })
}
