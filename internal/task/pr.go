package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/gh"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
)

const (
	commitMessageFile = "commit-message.txt"
	prDescriptionFile = "pr-description.md"
)

// Changes is the task's worktree against its merge-base with the task's base.
func (s *Service) Changes(ctx context.Context, taskRef string) (gitx.Changes, error) {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return gitx.Changes{}, err
	}
	return s.git.Changes(ctx, t.Worktree, t.BaseRef)
}

// Commit stages everything in the task's worktree and commits it with message,
// refusing while one of the task's sessions is mid-turn. It returns the new HEAD.
func (s *Service) Commit(ctx context.Context, taskRef, message string) (string, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("%w: a commit needs a message", ErrInvalid)
	}
	t, err := s.store.Task(taskRef)
	if err != nil {
		return "", err
	}
	if s.busy(t) {
		return "", fmt.Errorf("%w: a session is working in this worktree; wait for its turn to end", ErrInvalid)
	}
	dirty, err := s.git.Dirty(ctx, t.Worktree)
	if err != nil {
		return "", err
	}
	if !dirty {
		return "", fmt.Errorf("%w: nothing to commit", ErrInvalid)
	}
	sha, err := s.git.CommitAll(ctx, t.Worktree, message)
	if err != nil {
		return "", err
	}
	os.Remove(filepath.Join(s.ArtifactsDir(t.ID), commitMessageFile))
	s.notify("changes", t.ID)
	s.notify("pr", t.ID)
	return sha, nil
}

// busy reports whether any session of t is mid-turn.
func (s *Service) busy(t store.Task) bool {
	sessions, err := s.store.Sessions(t.ID)
	return err != nil || slices.ContainsFunc(sessions, func(ss store.Session) bool { return ss.InTurn })
}

// Push pushes the task's branch to origin. When the history was rewritten since
// layer8's last push, it force-pushes with a lease on that push, so commits
// someone else pushed are never overwritten.
func (s *Service) Push(ctx context.Context, taskRef string) error {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return err
	}
	if t.Branch == "" {
		return fmt.Errorf("%w: the worktree is not on a branch", ErrInvalid)
	}
	head, err := s.git.Head(ctx, t.Worktree)
	if err != nil {
		return err
	}
	lease := ""
	if t.LastPushed != "" && !s.git.IsAncestor(ctx, t.Worktree, t.LastPushed, head) {
		lease = t.LastPushed
	}
	if err := s.git.Push(ctx, t.Worktree, t.Branch, lease); err != nil {
		return err
	}
	if err := s.store.SetTaskPushed(t.ID, head); err != nil {
		return err
	}
	s.notify("pr", t.ID)
	return nil
}

// PRInfo is what the PR tab shows: the task's pull request when it has one, and
// otherwise what a new one would be opened with.
type PRInfo struct {
	PR         *gh.PR `json:"pr"`
	Head       string `json:"head"`
	Target     string `json:"target"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	Unpushed   bool   `json:"unpushed"`
	Dirty      bool   `json:"dirty"`
	TaskStatus string `json:"taskStatus"`
	Error      string `json:"error,omitempty"`
}

// PR looks up the task's pull request with its threads and records what changed.
// A gh failure is reported in Error so the rest still shows.
func (s *Service) PR(ctx context.Context, taskRef string) (PRInfo, error) {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return PRInfo{}, err
	}
	target, err := s.prTarget(t)
	if err != nil {
		return PRInfo{}, err
	}
	remote, pushed := s.originHead(ctx, t)
	head, _ := s.git.Head(ctx, t.Worktree)
	info := PRInfo{Head: t.Branch, Target: target, Title: t.Title, Body: s.prBody(t), Unpushed: head != remote, TaskStatus: t.Status}
	info.Dirty, _ = s.git.Dirty(ctx, t.Worktree)
	var pr *gh.PR
	if t.PRNumber != 0 || pushed {
		pr, err = s.syncPR(ctx, t)
	}
	if err == nil && pr != nil {
		pr.Threads, err = s.gh.Threads(ctx, t.Worktree, pr.Number)
	}
	if err != nil {
		info.Error = err.Error()
	}
	info.PR = pr
	return info, nil
}

// prTarget is the branch a task's PR merges into: its parent's branch while the
// parent has an open PR, else the project's base branch.
func (s *Service) prTarget(t store.Task) (string, error) {
	if t.ParentID != "" {
		parent, err := s.store.Task(t.ParentID)
		if err != nil {
			return "", err
		}
		if parent.PRNumber != 0 && parent.PRState == "OPEN" {
			return parent.Branch, nil
		}
	}
	p, err := s.store.Project(t.ProjectID)
	return p.BaseBranch, err
}

// prBody is the default body of the task's PR: the drafted description, else task.md without its title and instructions.
func (s *Service) prBody(t store.Task) string {
	dir := s.ArtifactsDir(t.ID)
	if b, err := os.ReadFile(filepath.Join(dir, prDescriptionFile)); err == nil {
		return strings.TrimSpace(stripFrontmatter(string(b)))
	}
	b, _ := os.ReadFile(filepath.Join(dir, "task.md"))
	body, _, _ := strings.Cut(string(b), "\n## Instructions")
	if first, rest, ok := strings.Cut(body, "\n"); ok && strings.HasPrefix(first, "# ") {
		body = rest
	}
	return strings.TrimSpace(body)
}

func stripFrontmatter(md string) string {
	if rest, ok := strings.CutPrefix(md, "---\n"); ok {
		if _, body, ok := strings.Cut(rest, "\n---\n"); ok {
			return body
		}
	}
	return md
}

// originHead returns the commit of origin's copy of the task's branch, if it was pushed.
func (s *Service) originHead(ctx context.Context, t store.Task) (string, bool) {
	if t.Branch == "" {
		return "", false
	}
	sha, err := s.git.Run(ctx, t.Worktree, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+t.Branch)
	return sha, err == nil
}

// syncPR fetches the task's PR, by number or else by branch, stores its number
// and state, and announces a "pr" event when anything shown changed. It returns
// nil when the branch has no PR.
func (s *Service) syncPR(ctx context.Context, t store.Task) (*gh.PR, error) {
	ref := t.Branch
	if t.PRNumber != 0 {
		ref = strconv.Itoa(t.PRNumber)
	}
	if ref == "" {
		return nil, nil
	}
	pr, err := s.gh.PRView(ctx, t.Worktree, ref)
	if errors.Is(err, gh.ErrNoPR) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if pr.Number != t.PRNumber || pr.URL != t.PRURL || pr.State != t.PRState {
		if err := s.store.SetTaskPR(t.ID, pr.Number, pr.URL, pr.State); err != nil {
			return nil, err
		}
		s.notify("task", t.ID)
	}
	s.announcePR(t.ID, pr)
	return &pr, nil
}

// announcePR sends a "pr" event when pr differs from what was last seen for the task.
func (s *Service) announcePR(taskID string, pr gh.PR) {
	b, _ := json.Marshal(pr)
	s.prMu.Lock()
	changed := s.prSeen[taskID] != string(b)
	s.prSeen[taskID] = string(b)
	s.prMu.Unlock()
	if changed {
		s.notify("pr", taskID)
	}
}

// PRInput is a new pull request's title and body, and whether it opens as a draft.
type PRInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Draft bool   `json:"draft"`
}

// OpenPR pushes the task's branch, opens its pull request into the PR target and,
// when the project writes back to Linear, moves the task's ticket to In Review.
func (s *Service) OpenPR(ctx context.Context, taskRef string, in PRInput) (gh.PR, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return gh.PR{}, fmt.Errorf("%w: a pull request needs a title", ErrInvalid)
	}
	t, err := s.store.Task(taskRef)
	if err != nil {
		return gh.PR{}, err
	}
	if t.PRNumber != 0 && t.PRState == "OPEN" {
		return gh.PR{}, fmt.Errorf("%w: the task already has PR #%d", ErrInvalid, t.PRNumber)
	}
	target, err := s.prTarget(t)
	if err != nil {
		return gh.PR{}, err
	}
	if err := s.Push(ctx, t.ID); err != nil {
		return gh.PR{}, err
	}
	pr, err := s.gh.PRCreate(ctx, t.Worktree, gh.NewPR{Head: t.Branch, Base: target, Title: in.Title, Body: in.Body, Draft: in.Draft})
	if err != nil {
		return gh.PR{}, err
	}
	if err := s.store.SetTaskPR(t.ID, pr.Number, pr.URL, pr.State); err != nil {
		return pr, err
	}
	s.notify("task", t.ID)
	s.notify("pr", t.ID)
	s.writeBack(ctx, t)
	return pr, nil
}

// MarkReady takes the task's draft pull request out of draft.
func (s *Service) MarkReady(ctx context.Context, taskRef string) error {
	t, err := s.withPR(taskRef)
	if err != nil {
		return err
	}
	if err := s.gh.PRReady(ctx, t.Worktree, t.PRNumber); err != nil {
		return err
	}
	_, err = s.syncPR(ctx, t)
	return err
}

func (s *Service) withPR(taskRef string) (store.Task, error) {
	t, err := s.store.Task(taskRef)
	if err == nil && t.PRNumber == 0 {
		err = fmt.Errorf("%w: the task has no pull request", ErrInvalid)
	}
	return t, err
}

// FixCI sends the PR's failing checks to the session sessionID, or to a new
// "fix-ci" session when it is empty.
func (s *Service) FixCI(ctx context.Context, taskRef, sessionID string) (store.Session, error) {
	t, err := s.withPR(taskRef)
	if err != nil {
		return store.Session{}, err
	}
	pr, err := s.gh.PRView(ctx, t.Worktree, strconv.Itoa(t.PRNumber))
	if err != nil {
		return store.Session{}, err
	}
	failed := slices.DeleteFunc(pr.Checks, func(c gh.Check) bool { return !c.Failed() })
	if len(failed) == 0 {
		return store.Session{}, fmt.Errorf("%w: no failing checks on PR #%d", ErrInvalid, pr.Number)
	}
	return s.tell(ctx, t, sessionID, "fix-ci", fixCIMessage(pr, failed))
}

func fixCIMessage(pr gh.PR, failed []gh.Check) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CI is failing on pull request #%d (%s):\n\n", pr.Number, pr.URL)
	for _, c := range failed {
		fmt.Fprintf(&b, "- %s: %s %s\n", c.Name, c.State, c.URL)
	}
	fmt.Fprintf(&b, "\nFind the cause with `gh pr checks %d` and `gh run view <run-id> --log-failed`, fix it, run the matching local check, then commit and push.\n", pr.Number)
	return b.String()
}

// FixReviews sends the PR's unresolved review threads to the session sessionID,
// or to a new "fix-review" session when it is empty.
func (s *Service) FixReviews(ctx context.Context, taskRef, sessionID string) (store.Session, error) {
	t, err := s.withPR(taskRef)
	if err != nil {
		return store.Session{}, err
	}
	threads, err := s.gh.Threads(ctx, t.Worktree, t.PRNumber)
	if err != nil {
		return store.Session{}, err
	}
	if len(threads) == 0 {
		return store.Session{}, fmt.Errorf("%w: no unresolved review threads on PR #%d", ErrInvalid, t.PRNumber)
	}
	return s.tell(ctx, t, sessionID, "fix-review", reviewsMessage(t.PRNumber, threads))
}

func reviewsMessage(number int, threads []gh.Thread) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Pull request #%d has unresolved review threads:\n", number)
	for _, th := range threads {
		where := th.Path
		if th.Line > 0 {
			where = fmt.Sprintf("%s:%d", where, th.Line)
		}
		if th.Outdated {
			where += " (outdated)"
		}
		fmt.Fprintf(&b, "\n%s\n", where)
		for _, c := range th.Comments {
			fmt.Fprintf(&b, "%s: %s\n", c.Author, strings.ReplaceAll(c.Body, "\n", "\n  "))
		}
	}
	b.WriteString("\nAddress each thread, then commit and push.\n")
	return b.String()
}

// Draft asks an agent to write the commit message ("commit") or the PR description
// ("pr") into the task's artifacts, where the Commit and PR forms pick it up.
func (s *Service) Draft(ctx context.Context, taskRef, kind, sessionID string) (store.Session, error) {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return store.Session{}, err
	}
	switch kind {
	case "commit":
		return s.tell(ctx, t, sessionID, "commit-message", "Write a commit message for the uncommitted changes in this worktree (`git status`, `git diff HEAD` and the untracked files): "+
			"a summary line under 72 characters, a blank line, then why the change exists. Save only the message to "+LinkPath+"/"+commitMessageFile+". Do not stage, commit or push anything.")
	case "pr":
		return s.tell(ctx, t, sessionID, "pr-description", "/l8:describe-pr Draft only: write the description of this task's branch to "+LinkPath+"/"+prDescriptionFile+
			" and stop there. Do not commit, push, create or edit a pull request; layer8 opens it with that file as the body.")
	}
	return store.Session{}, fmt.Errorf("%w: draft a commit or a pr, not %q", ErrInvalid, kind)
}

// tell sends text to the task's session sessionID, or starts a session labelled label with it when sessionID is empty.
func (s *Service) tell(ctx context.Context, t store.Task, sessionID, label, text string) (store.Session, error) {
	if s.sessions == nil {
		return store.Session{}, fmt.Errorf("%w: sessions are not available", ErrInvalid)
	}
	if sessionID == "" {
		return s.sessions.Start(ctx, t.ID, session.StartInput{Label: label, Prompt: text})
	}
	ss, err := s.store.Session(sessionID)
	if err != nil {
		return ss, err
	}
	if ss.TaskID != t.ID {
		return ss, fmt.Errorf("%w: session %s belongs to another task", ErrInvalid, sessionID)
	}
	if _, err := s.sessions.Send(ctx, sessionID, text); err != nil {
		return ss, err
	}
	return s.store.Session(sessionID)
}

// PollPRs refreshes the pull requests of active tasks now and then every interval until ctx is done.
func (s *Service) PollPRs(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		s.pollPRs(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// pollPRs refreshes the PR of every active task whose PR may have changed.
func (s *Service) pollPRs(ctx context.Context) {
	tasks, err := s.store.ListTasks(store.TaskFilter{Status: "active"})
	if err != nil {
		log.Printf("poll PRs: %v", err)
		return
	}
	for _, t := range tasks {
		if ctx.Err() != nil {
			return
		}
		if !s.prWatched(ctx, t) {
			continue
		}
		if _, err := s.syncPR(ctx, t); err != nil {
			log.Printf("task %s: PR status: %v", t.Slug, err)
		}
	}
}

// prWatched reports whether t's PR may change: it is open, or t has none yet
// but its branch is on origin, where an agent may have opened one.
func (s *Service) prWatched(ctx context.Context, t store.Task) bool {
	if t.PRNumber != 0 {
		return t.PRState == "OPEN"
	}
	_, pushed := s.originHead(ctx, t)
	return pushed
}
