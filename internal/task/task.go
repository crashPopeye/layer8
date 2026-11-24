// Package task owns projects and tasks: a task is a worktree, a branch and an
// artifacts folder linked into the worktree at .layer8/task.
package task

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/workflow"
	"github.com/google/uuid"
)

// ErrInvalid marks errors caused by bad input rather than by the system.
var ErrInvalid = errors.New("invalid input")

// Workflows are the phase chains a task can follow.
var Workflows = []string{"rpi", "prd_tdd", "outline_only", "oneshot", "freeform"}

// Statuses are the states a task can be in.
var Statuses = []string{"active", "done", "archived"}

// LinkPath is where a task's artifacts folder appears inside its worktree.
const LinkPath = ".layer8/task"

// ParentLink is the name, inside a sub-task's artifacts folder, of the link to its parent's folder.
const ParentLink = "parent"

// Service creates and updates projects and tasks, starts their first session,
// and announces every change.
type Service struct {
	store    *store.Store
	git      gitx.Git
	home     string
	notify   func(kind, id string)
	sessions *session.Manager

	phaseMu sync.Mutex
}

// NewService returns a Service keeping worktrees and artifacts under home; sessions
// may be nil, which only disables starting a session with a new task.
func NewService(st *store.Store, git gitx.Git, home string, notify func(kind, id string), sessions *session.Manager) *Service {
	return &Service{store: st, git: git, home: home, notify: notify, sessions: sessions}
}

// ArtifactsDir is the canonical artifacts folder of a task.
func (s *Service) ArtifactsDir(taskID string) string {
	return filepath.Join(s.home, "artifacts", taskID)
}

// AddProject registers the git repository containing path.
func (s *Service) AddProject(ctx context.Context, path string) (store.Project, error) {
	abs, err := filepath.Abs(expandHome(path))
	if err != nil {
		return store.Project{}, err
	}
	root, err := s.git.TopLevel(ctx, abs)
	if err != nil {
		return store.Project{}, fmt.Errorf("%w: %s is not a git repository", ErrInvalid, path)
	}
	base, err := s.git.DefaultBranch(ctx, root)
	if err != nil {
		return store.Project{}, err
	}
	p := store.Project{
		ID:         uuid.Must(uuid.NewV7()).String(),
		Name:       filepath.Base(root),
		Path:       root,
		BaseBranch: base,
		CreatedAt:  time.Now().UnixMilli(),
	}
	if err := s.store.InsertProject(p); err != nil {
		return store.Project{}, err
	}
	s.notify("project", p.ID)
	return p, nil
}

// ProjectUpdate holds the project fields to change; nil fields are kept.
type ProjectUpdate struct {
	BaseBranch   *string `json:"baseBranch"`
	Instructions *string `json:"instructions"`
}

// UpdateProject applies u to the project id.
func (s *Service) UpdateProject(id string, u ProjectUpdate) (store.Project, error) {
	p, err := s.store.Project(id)
	if err != nil {
		return p, err
	}
	if u.BaseBranch != nil {
		p.BaseBranch = *u.BaseBranch
	}
	if u.Instructions != nil {
		p.Instructions = *u.Instructions
	}
	if err := s.store.UpdateProject(p); err != nil {
		return p, err
	}
	s.notify("project", p.ID)
	return p, nil
}

// CreateInput describes a new task. ParentID makes it a sub-task of that task, in
// the parent's project and branched from the parent's branch. AdoptPath turns an
// existing folder into the task's worktree instead of creating one. Start starts
// the task's first session: its workflow's first phase, or Prompt for freeform.
// AutoAdvance starts each next phase as soon as the previous one is done.
type CreateInput struct {
	ProjectID   string `json:"projectId"`
	ParentID    string `json:"parentId"`
	Title       string `json:"title"`
	Workflow    string `json:"workflow"`
	Prompt      string `json:"prompt"`
	AdoptPath   string `json:"adoptPath"`
	Autonomous  bool   `json:"autonomous"`
	AutoAdvance bool   `json:"autoAdvance"`
	Start       bool   `json:"start"`
}

// Created is a new task and, when it was asked to start, its first session.
type Created struct {
	store.Task
	Session *store.Session `json:"session,omitempty"`
}

// Create makes the task's worktree and branch, its artifacts folder with task.md,
// and the .layer8/task link between them, then starts its first session if asked.
// A sub-task inherits its parent's project and autonomy and links the parent's
// artifacts folder as parent/.
func (s *Service) Create(ctx context.Context, in CreateInput) (Created, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return Created{}, fmt.Errorf("%w: title is required", ErrInvalid)
	}
	if in.Workflow == "" {
		in.Workflow = "freeform"
	}
	if !slices.Contains(Workflows, in.Workflow) {
		return Created{}, fmt.Errorf("%w: unknown workflow %q", ErrInvalid, in.Workflow)
	}
	if in.Start && s.sessions == nil {
		return Created{}, fmt.Errorf("%w: sessions are not available", ErrInvalid)
	}
	var parent *store.Task
	if in.ParentID != "" {
		pt, err := s.store.Task(in.ParentID)
		if err != nil {
			return Created{}, fmt.Errorf("parent task %s: %w", in.ParentID, err)
		}
		if in.ProjectID != "" && in.ProjectID != pt.ProjectID {
			return Created{}, fmt.Errorf("%w: a sub-task belongs to its parent's project", ErrInvalid)
		}
		parent, in.ProjectID = &pt, pt.ProjectID
		in.Autonomous = in.Autonomous || pt.Autonomous
	}
	p, err := s.store.Project(in.ProjectID)
	if err != nil {
		return Created{}, err
	}
	slug, err := s.freeSlug(slugify(in.Title))
	if err != nil {
		return Created{}, err
	}
	t := store.Task{
		ID:          uuid.Must(uuid.NewV7()).String(),
		ProjectID:   p.ID,
		Slug:        slug,
		Title:       in.Title,
		Workflow:    in.Workflow,
		Status:      "active",
		Autonomous:  in.Autonomous,
		AutoAdvance: in.AutoAdvance,
		CreatedAt:   time.Now().UnixMilli(),
	}
	if parent != nil {
		t.ParentID, t.BaseRef = parent.ID, parent.Branch
	} else {
		t.BaseRef = s.baseRef(ctx, p)
	}
	if in.AdoptPath != "" {
		err = s.adopt(ctx, &t, in.AdoptPath)
	} else {
		err = s.newWorktree(ctx, &t, p)
	}
	if err != nil {
		return Created{}, err
	}
	if err := s.linkArtifacts(ctx, t, taskMarkdown(t.Title, in.Prompt, p.Instructions)); err != nil {
		return Created{}, err
	}
	if err := s.store.InsertTask(t); err != nil {
		return Created{}, err
	}
	s.notify("task", t.ID)
	c := Created{Task: t}
	if !in.Start {
		return c, nil
	}
	ss, err := s.startFirst(ctx, &c.Task, in.Prompt)
	if err != nil {
		return c, fmt.Errorf("task %s was created but its session did not start: %w", t.Slug, err)
	}
	c.Session = &ss
	return c, nil
}

// startFirst starts a new task's first session: its workflow's first phase, or a plain session for freeform.
func (s *Service) startFirst(ctx context.Context, t *store.Task, prompt string) (store.Session, error) {
	first, ok := workflow.First(t.Workflow)
	if !ok {
		return s.sessions.Start(ctx, t.ID, session.StartInput{Label: "session", Prompt: firstMessage(prompt)})
	}
	s.phaseMu.Lock()
	defer s.phaseMu.Unlock()
	return s.startPhase(ctx, t, first, prompt)
}

// firstMessage is what a started task's first session receives.
func firstMessage(prompt string) string {
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		return prompt
	}
	return "Read " + LinkPath + "/task.md and work on this task."
}

// baseRef fetches the project's base branch and prefers origin's copy of it,
// falling back to the local branch when there is no origin or no network.
func (s *Service) baseRef(ctx context.Context, p store.Project) string {
	_ = s.git.Fetch(ctx, p.Path, p.BaseBranch)
	if origin := "origin/" + p.BaseBranch; s.git.RefExists(ctx, p.Path, origin) {
		return origin
	}
	return p.BaseBranch
}

func (s *Service) newWorktree(ctx context.Context, t *store.Task, p store.Project) error {
	t.Branch = "l8/" + t.Slug
	t.Worktree = filepath.Join(s.home, "workspaces", t.Slug)
	if err := os.MkdirAll(filepath.Dir(t.Worktree), 0o755); err != nil {
		return err
	}
	return s.git.WorktreeAdd(ctx, p.Path, t.Worktree, t.Branch, t.BaseRef)
}

func (s *Service) adopt(ctx context.Context, t *store.Task, path string) error {
	root, err := s.git.TopLevel(ctx, expandHome(path))
	if err != nil {
		return fmt.Errorf("%w: %s is not a git worktree", ErrInvalid, path)
	}
	t.Worktree = root
	t.Branch, err = s.git.CurrentBranch(ctx, root)
	return err
}

// linkArtifacts writes task.md into the artifacts folder, links that folder at
// <worktree>/.layer8/task, links a sub-task's parent folder as parent/, and keeps
// .layer8/ out of git.
func (s *Service) linkArtifacts(ctx context.Context, t store.Task, taskMD string) error {
	dir := s.ArtifactsDir(t.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "task.md"), []byte(taskMD), 0o644); err != nil {
		return err
	}
	if t.ParentID != "" {
		if err := symlink(filepath.Join("..", t.ParentID), filepath.Join(dir, ParentLink)); err != nil {
			return err
		}
	}
	link := filepath.Join(t.Worktree, LinkPath)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return err
	}
	if err := symlink(dir, link); err != nil {
		return err
	}
	common, err := s.git.CommonDir(ctx, t.Worktree)
	if err != nil {
		return err
	}
	return ensureLine(filepath.Join(common, "info", "exclude"), ".layer8/")
}

// Detail is a task with its artifacts folder, the content of its task.md and its workflow progress.
type Detail struct {
	store.Task
	Progress
	ArtifactsDir string `json:"artifactsDir"`
	TaskMD       string `json:"taskMd"`
}

// Detail looks a task up by id or slug and reads its task.md and workflow progress.
func (s *Service) Detail(ctx context.Context, idOrSlug string) (Detail, error) {
	t, err := s.store.Task(idOrSlug)
	if err != nil {
		return Detail{}, err
	}
	dir := s.ArtifactsDir(t.ID)
	md, err := os.ReadFile(filepath.Join(dir, "task.md"))
	if err != nil && !os.IsNotExist(err) {
		return Detail{}, err
	}
	return Detail{Task: t, Progress: s.progress(ctx, t), ArtifactsDir: dir, TaskMD: string(md)}, nil
}

// TaskUpdate holds the task fields to change; nil fields are kept.
type TaskUpdate struct {
	Status      *string `json:"status"`
	Phase       *string `json:"phase"`
	AutoAdvance *bool   `json:"autoAdvance"`
	Autonomous  *bool   `json:"autonomous"`
}

// Update applies u to a task. Turning auto-advance on starts the next phase
// right away if the current one is already done.
func (s *Service) Update(ctx context.Context, idOrSlug string, u TaskUpdate) (store.Task, error) {
	s.phaseMu.Lock()
	defer s.phaseMu.Unlock()
	t, err := s.store.Task(idOrSlug)
	if err != nil {
		return t, err
	}
	if u.Status != nil {
		if !slices.Contains(Statuses, *u.Status) {
			return t, fmt.Errorf("%w: unknown status %q", ErrInvalid, *u.Status)
		}
		t.Status = *u.Status
	}
	if u.Phase != nil {
		if _, ok := workflow.Find(t.Workflow, *u.Phase); !ok && *u.Phase != "" {
			return t, fmt.Errorf("%w: workflow %s has no phase %q", ErrInvalid, t.Workflow, *u.Phase)
		}
		t.Phase = *u.Phase
	}
	if u.AutoAdvance != nil {
		t.AutoAdvance = *u.AutoAdvance
	}
	if u.Autonomous != nil {
		t.Autonomous = *u.Autonomous
	}
	if err := s.store.UpdateTask(t); err != nil {
		return t, err
	}
	s.notify("task", t.ID)
	if u.AutoAdvance != nil && t.AutoAdvance {
		s.advance(ctx, t)
		return s.store.Task(t.ID)
	}
	return t, nil
}

func (s *Service) freeSlug(base string) (string, error) {
	for n := 1; ; n++ {
		slug := base
		if n > 1 {
			slug += "-" + strconv.Itoa(n)
		}
		taken, err := s.store.SlugTaken(slug)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(filepath.Join(s.home, "workspaces", slug)); !taken && os.IsNotExist(err) {
			return slug, nil
		}
	}
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(title string) string {
	slug := strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(slug) > 50 {
		slug = strings.TrimRight(slug[:50], "-")
	}
	if slug == "" {
		return "task"
	}
	return slug
}

func taskMarkdown(title, prompt, instructions string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", title)
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		fmt.Fprintf(&b, "\n%s\n", prompt)
	}
	if instructions = strings.TrimSpace(instructions); instructions != "" {
		fmt.Fprintf(&b, "\n## Instructions\n\n%s\n", instructions)
	}
	return b.String()
}

// symlink makes link point at target, replacing an existing link.
func symlink(target, link string) error {
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		os.Remove(link)
	}
	return os.Symlink(target, link)
}

func ensureLine(path, line string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if slices.Contains(strings.Split(string(data), "\n"), line) {
		return nil
	}
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		line = "\n" + line
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func expandHome(path string) string {
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, rest)
		}
	}
	return path
}
