package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/github"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/launch"
	"github.com/crashPopeye/layer8/internal/linear"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tmux"
)

var slugBad = regexp.MustCompile(`[^a-z0-9]+`)

func Slugify(s string) string {
	s = slugBad.ReplaceAllString(strings.ToLower(s), "-")
	s = strings.Trim(s, "-")
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	if s == "" {
		return "task"
	}
	return s
}

func (a *App) uniqueSlug(base string) string {
	slug := Slugify(base)
	for n := 2; a.Store.SlugTaken(slug); n++ {
		slug = fmt.Sprintf("%s-%d", Slugify(base), n)
	}
	return slug
}

func (a *App) AddProject(name string) (store.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return store.Project{}, errors.New("a project needs a name")
	}
	p, err := a.Store.InsertProject(name)
	a.bus.Changed()
	return p, err
}

// RemoveProject forgets a project, its folders and their tasks. Files and branches are kept.
func (a *App) RemoveProject(id string) error {
	folders, err := a.Store.Folders()
	if err != nil {
		return err
	}
	inProject := map[string]bool{}
	for _, f := range folders {
		inProject[f.ID] = f.ProjectID == id
	}
	if err := a.releaseTasks(func(t store.Task) bool { return inProject[t.FolderID] }); err != nil {
		return err
	}
	err = a.Store.DeleteProject(id)
	a.bus.Changed()
	return err
}

// AddFolder adds any folder to a project. Git repos get worktrees per task.
func (a *App) AddFolder(ctx context.Context, projectID, path string) (store.Folder, error) {
	if _, err := a.Store.Project(projectID); err != nil {
		return store.Folder{}, errors.New("unknown project")
	}
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return store.Folder{}, err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return store.Folder{}, fmt.Errorf("not a folder: %s", abs)
	}
	// Resolve symlinks so git's toplevel (which is resolved) compares equal.
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		abs = r
	}
	root := gitx.RepoRoot(ctx, abs)
	isGit := root != "" && filepath.Clean(root) == abs
	f := store.Folder{ProjectID: projectID, Name: filepath.Base(abs), Path: abs, IsGit: isGit}
	if isGit {
		f.DefaultBranch = gitx.DefaultBranch(ctx, abs)
	}
	f, err = a.Store.InsertFolder(f)
	a.bus.Changed()
	return f, err
}

func (a *App) RemoveFolder(id string) error {
	if err := a.releaseTasks(func(t store.Task) bool { return t.FolderID == id }); err != nil {
		return err
	}
	err := a.Store.DeleteFolder(id)
	a.bus.Changed()
	return err
}

// releaseTasks closes the sessions of the matching tasks before their rows are deleted,
// refusing while any of them is working.
func (a *App) releaseTasks(match func(store.Task) bool) error {
	tasks, err := a.Store.Tasks()
	if err != nil {
		return err
	}
	var sessions []store.Session
	for _, t := range tasks {
		if match(t) {
			sessions = append(sessions, a.taskSessions(t.ID)...)
		}
	}
	for _, s := range sessions {
		if a.Sessions.Busy(s.ID) {
			return errors.New("agents are working here; stop them first")
		}
	}
	for _, s := range sessions {
		_ = a.Sessions.Release(s.ID)
	}
	return nil
}

type NewTask struct {
	FolderID    string `json:"folderId"`
	LinearIssue string `json:"linearIssue"`
	Title       string `json:"title"`
}

func (a *App) CreateTask(ctx context.Context, in NewTask) (store.Task, error) {
	folder, err := a.Store.Folder(in.FolderID)
	if err != nil {
		return store.Task{}, errors.New("unknown folder")
	}
	cfg := a.Config.Get()
	title := strings.TrimSpace(in.Title)
	var ticket string
	var lin *store.LinearRef
	var branchHint string
	if in.LinearIssue != "" {
		lc, err := a.linear(ctx)
		if err != nil {
			return store.Task{}, err
		}
		issue, err := lc.Issue(ctx, in.LinearIssue)
		if err != nil {
			return store.Task{}, err
		}
		lin = &store.LinearRef{ID: issue.ID, Identifier: issue.Identifier, URL: issue.URL, BranchName: issue.BranchName}
		if title == "" {
			title = issue.Title
		}
		ticket = linear.TicketMarkdown(issue)
		// Linear's branch name contains the identifier, which is what links PRs back to the issue.
		branchHint = issue.BranchName
	} else {
		if title == "" {
			return store.Task{}, errors.New("a task needs a Linear issue or a title")
		}
		branchHint = "layer8/" + Slugify(title)
	}

	slugBase := title
	if lin != nil {
		slugBase = lin.Identifier
	}
	slug := a.uniqueSlug(slugBase)
	t := store.Task{
		ID: store.NewID(), FolderID: folder.ID, Slug: slug, Title: title, Linear: lin, Status: "active",
		ArtifactsPath: filepath.Join(a.Paths.Tasks, slug), WorktreePath: folder.Path,
	}
	if err := os.MkdirAll(t.ArtifactsPath, 0o755); err != nil {
		return t, err
	}
	if ticket != "" {
		if err := os.WriteFile(filepath.Join(t.ArtifactsPath, "ticket.md"), []byte(ticket), 0o644); err != nil {
			return t, err
		}
	}
	if folder.IsGit {
		def := folder.DefaultBranch
		if def == "" {
			def = gitx.DefaultBranch(ctx, folder.Path)
		}
		_ = gitx.Fetch(ctx, folder.Path)
		t.BaseRef = gitx.BaseRef(ctx, folder.Path, def)
		t.Branch = gitx.SanitizeBranch(branchHint)
		t.WorktreePath = filepath.Join(cfg.WorktreeRoot, folder.Name, slug)
		if err := gitx.AddWorktree(ctx, folder.Path, t.WorktreePath, t.Branch, t.BaseRef); err != nil {
			return t, err
		}
		t.BaseSHA, _ = gitx.MergeBase(ctx, t.WorktreePath, t.BaseRef, "HEAD")
	}
	t.TmuxSession = a.ensureTmux(ctx, slug, t.WorktreePath, map[string]string{
		"LAYER8_TASK": slug, "LAYER8_ARTIFACTS": t.ArtifactsPath,
	})
	t, err = a.Store.InsertTask(t)
	a.bus.Changed()
	return t, err
}

func (a *App) ensureTmux(ctx context.Context, slug, cwd string, env map[string]string) string {
	if !a.Config.Get().UseTmux || !tmux.Available() {
		return ""
	}
	name := tmux.Name(slug)
	if err := tmux.Ensure(ctx, name, cwd, env); err != nil {
		return ""
	}
	return name
}

// ArchiveTask hides a task. Branches are always kept; worktrees are removed only on request.
func (a *App) ArchiveTask(ctx context.Context, id string, removeWorktrees bool) error {
	t, err := a.Store.Task(id)
	if err != nil {
		return err
	}
	for _, s := range a.taskSessions(id) {
		if a.Sessions.Busy(s.ID) {
			return errors.New("stop the task's running sessions first")
		}
	}
	folder, wsErr := a.Store.Folder(t.FolderID)
	slices, _ := a.Store.TaskSlices(id)
	for _, x := range slices {
		if x.TmuxSession != "" {
			_ = tmux.Kill(ctx, x.TmuxSession)
		}
		if removeWorktrees && wsErr == nil && folder.IsGit && x.WorktreePath != t.WorktreePath {
			if err := gitx.RemoveWorktree(ctx, folder.Path, x.WorktreePath); err != nil {
				return fmt.Errorf("remove worktree of %s (commit or discard its changes first): %w", x.Key, err)
			}
		}
	}
	if t.TmuxSession != "" {
		_ = tmux.Kill(ctx, t.TmuxSession)
	}
	if removeWorktrees && wsErr == nil && folder.IsGit && t.WorktreePath != folder.Path {
		if err := gitx.RemoveWorktree(ctx, folder.Path, t.WorktreePath); err != nil {
			return fmt.Errorf("remove task worktree (commit or discard its changes first): %w", err)
		}
	}
	err = a.Store.SetTaskStatus(id, "archived")
	a.bus.Changed()
	return err
}

func (a *App) taskSessions(taskID string) []store.Session {
	all, _ := a.Store.Sessions()
	var out []store.Session
	for _, s := range all {
		if s.TaskID == taskID {
			out = append(out, s)
		}
	}
	return out
}

// ---- artifacts

type Artifact struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	UpdatedAt string `json:"updatedAt"`
}

func (a *App) Artifacts(taskID string) ([]Artifact, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(t.ArtifactsPath)
	if errors.Is(err, os.ErrNotExist) {
		return []Artifact{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Artifact{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Artifact{Name: e.Name(), Size: info.Size(), UpdatedAt: info.ModTime().UTC().Format(time.RFC3339)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (a *App) ReadArtifact(taskID, name string) (string, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return "", err
	}
	// Names come from the UI: only plain file names inside the artifacts folder.
	if name == "" || name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return "", errors.New("invalid artifact name")
	}
	b, err := os.ReadFile(filepath.Join(t.ArtifactsPath, name))
	if err != nil {
		return "", err
	}
	if len(b) > 4<<20 {
		b = b[:4<<20]
	}
	return string(b), nil
}

// ---- terminal / editor

// worktreeOf returns where a task or slice lives, plus its tmux session name.
func (a *App) worktreeOf(taskID, sliceID string) (path, tmuxName, slug string, err error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return "", "", "", err
	}
	if sliceID == "" {
		return t.WorktreePath, t.TmuxSession, t.Slug, nil
	}
	x, err := a.Store.Slice(sliceID)
	if err != nil {
		return "", "", "", err
	}
	return x.WorktreePath, x.TmuxSession, t.Slug + "--" + x.Key, nil
}

func (a *App) OpenTerminal(ctx context.Context, taskID, sliceID string) error {
	path, name, slug, err := a.worktreeOf(taskID, sliceID)
	if err != nil {
		return err
	}
	cfg := a.Config.Get()
	cmd := os.Getenv("SHELL")
	if cmd == "" {
		cmd = "/bin/zsh"
	}
	if cfg.UseTmux && tmux.Available() {
		if name == "" {
			name = tmux.Name(slug)
		}
		cmd = tmux.AttachCommand(name, path)
	}
	c, err := launch.Terminal(cfg, path, cmd)
	if err != nil {
		return err
	}
	return launch.Start(c, path)
}

func (a *App) OpenEditor(taskID, sliceID string) error {
	path, _, _, err := a.worktreeOf(taskID, sliceID)
	if err != nil {
		return err
	}
	c, err := launch.Editor(a.Config.Get(), path)
	if err != nil {
		return err
	}
	return launch.Start(c, path)
}

// ResumeInTerminal continues a session by hand with `claude --resume` in the worktree's tmux session.
func (a *App) ResumeInTerminal(ctx context.Context, sessionID string) error {
	s, err := a.Store.Session(sessionID)
	if err != nil {
		return err
	}
	if s.ClaudeSessionID == "" {
		return errors.New("this session has not started yet")
	}
	// Close layer8's own process for this conversation so only the terminal writes to it;
	// the next message from layer8 resumes it again.
	if err := a.Sessions.Release(sessionID); err != nil {
		return errors.New("stop the session in layer8 first, so two agents don't write to the same conversation")
	}
	path, name, slug, err := a.worktreeOf(s.TaskID, s.SliceID)
	if err != nil {
		return err
	}
	resume := "claude --resume " + tmux.Quote(s.ClaudeSessionID)
	cfg := a.Config.Get()
	if cfg.UseTmux && tmux.Available() {
		if name == "" {
			name = tmux.Name(slug)
		}
		if err := tmux.Ensure(ctx, name, path, nil); err != nil {
			return err
		}
		if err := tmux.NewWindow(ctx, name, path, "claude-"+s.Kind, resume); err != nil {
			return err
		}
		c, err := launch.Terminal(cfg, path, tmux.AttachCommand(name, path))
		if err != nil {
			return err
		}
		return launch.Start(c, path)
	}
	c, err := launch.Terminal(cfg, path, resume)
	if err != nil {
		return err
	}
	return launch.Start(c, path)
}

// ---- tokens

// linear signs requests with the OAuth connection when there is one, else the personal API key.
func (a *App) linear(ctx context.Context) (*linear.Client, error) {
	cfg := a.Config.Get()
	auth, err := a.Linear.Authorization(ctx)
	if err != nil {
		return nil, err
	}
	if auth == "" {
		auth = cfg.LinearAPIKey
	}
	return linear.New(auth), nil
}

func (a *App) hasLinear() bool {
	return a.Linear.Connected() || a.Config.Get().LinearAPIKey != ""
}

func (a *App) githubToken() (string, error) {
	return github.Token(a.Config.Get().GitHubToken)
}

func (a *App) github() (*github.Client, error) {
	tok, err := a.githubToken()
	if err != nil {
		return nil, err
	}
	return github.New(tok), nil
}
