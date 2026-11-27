// Package tasks provides tasks and sub-tasks with their branches and worktrees, and the artifacts shared by a task tree.
package tasks

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Options configures Register.
type Options struct {
	Home string
	Apps config.Apps
	// Open runs macOS open with args; nil runs /usr/bin/open.
	Open func(ctx context.Context, args ...string) error
}

// Service owns the task and artifact kinds.
type Service struct {
	db   *sql.DB
	tree *tree.Tree
	o    Options
}

// Task is a task or sub-task with the paths it works in.
type Task struct {
	ID          int64
	Slug        string
	Title       string
	Branch      string
	Base        string
	Worktree    string
	Status      string
	Project     string
	ProjectPath string
	CreatedBy   string
	Linear, PR  json.RawMessage
	// Artifacts is the folder shared by every task in the tree.
	Artifacts string
}

// Addr returns the task's address, task:<root>/<child>….
func (t Task) Addr() tree.Addr { return AddrOf(t.Slug) }

// Root returns the slug of the task at the top of the tree.
func (t Task) Root() string { return rootOf(t.Slug) }

// AddrOf returns the address of the task with slug.
func AddrOf(slug string) tree.Addr { return tree.Addr("task:" + strings.ReplaceAll(slug, "--", "/")) }

func rootOf(slug string) string {
	root, _, _ := strings.Cut(slug, "--")
	return root
}

// parent returns the address of the task's parent task, or of its project for a root task.
func (t Task) parent() tree.Addr {
	if i := strings.LastIndex(t.Slug, "--"); i >= 0 {
		return AddrOf(t.Slug[:i])
	}
	return tree.Addr("project:" + t.Project)
}

// Register adds the task and artifact kinds to t, and tasks under projects.
func Register(t *tree.Tree, db *sql.DB, o Options) *Service {
	s := &Service{db: db, tree: t, o: o}
	if s.o.Open == nil {
		s.o.Open = func(ctx context.Context, args ...string) error {
			if out, err := exec.CommandContext(ctx, "open", args...).CombinedOutput(); err != nil {
				return fmt.Errorf("open: %v %s", err, out)
			}
			return nil
		}
	}
	t.Register(tree.Kind{
		Name:     "task",
		Get:      s.get,
		Children: s.children,
		Actions: []tree.Action{
			{Name: "create-subtask", Help: "Create a child task stacked on this one", When: statusIn("active"), Run: s.createSubtask,
				Args: json.RawMessage(`{"type":"object","required":["name"],"properties":{
					"name":{"type":"string","description":"Short name, becomes the last part of the address"},
					"base":{"type":"string","description":"Sibling sub-task name or branch to stack on, defaults to this task's branch"}}}`)},
			{Name: "done", Help: "Mark the task done; its worktree stays", When: statusIn("active"), Run: s.setStatus("done")},
			{Name: "reopen", Help: "Make the task active again, restoring its worktree", When: statusIn("done", "archived"), Run: s.reopen},
			{Name: "archive", Help: "Archive the task and its sub-tasks, removing their worktrees and keeping branches and artifacts", When: statusIn("active", "done"), Run: s.archive},
			{Name: "open", Help: "Open the worktree in the terminal, the editor or Finder", When: statusIn("active", "done"), Run: s.open,
				Args: json.RawMessage(`{"type":"object","required":["target"],"properties":{"target":{"enum":["terminal","editor","finder"]}}}`)},
		},
	})
	t.Register(tree.Kind{
		Name: "artifact",
		Get:  s.getArtifact,
		Actions: []tree.Action{
			{Name: "write", Help: "Replace the artifact's content, creating it if needed", Run: s.writeArtifact,
				Args: json.RawMessage(`{"type":"object","required":["content"],"properties":{"content":{"type":"string"}}}`)},
			{Name: "delete", Help: "Delete the artifact", When: func(n tree.Node) bool { return n.Fields["exists"] == true }, Run: s.deleteArtifact},
		},
	})
	t.Extend("project", tree.Action{
		Name: "create-task", Help: "Create a task with its own branch, worktree and artifacts folder", Run: s.createTask,
		Args: json.RawMessage(`{"type":"object","required":["title"],"properties":{
			"title":{"type":"string"},
			"slug":{"type":"string","description":"Short id for the address, branch and folders, derived from the title by default"},
			"base":{"type":"string","description":"Branch to start from, defaults to the project's default branch"},
			"branch":{"type":"string","description":"Branch name, defaults to the slug"}}}`),
	})
	t.AddChildren("project", s.projectTasks)
	return s
}

func statusIn(statuses ...string) func(tree.Node) bool {
	return func(n tree.Node) bool { return slices.Contains(statuses, n.Fields["status"].(string)) }
}

const taskQuery = `SELECT t.id, t.slug, t.title, t.branch, t.base_ref, t.worktree, t.status, t.created_by, t.linear, t.pr, p.name, p.path
	FROM tasks t JOIN projects p ON p.id = t.project_id `

func (s *Service) query(ctx context.Context, where string, args ...any) ([]Task, error) {
	rows, err := s.db.QueryContext(ctx, taskQuery+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Task
	for rows.Next() {
		var t Task
		var linear, pr sql.NullString
		if err := rows.Scan(&t.ID, &t.Slug, &t.Title, &t.Branch, &t.Base, &t.Worktree, &t.Status, &t.CreatedBy, &linear, &pr, &t.Project, &t.ProjectPath); err != nil {
			return nil, err
		}
		if linear.Valid {
			t.Linear = json.RawMessage(linear.String)
		}
		if pr.Valid {
			t.PR = json.RawMessage(pr.String)
		}
		t.Artifacts = s.artifactsDir(t.Root())
		list = append(list, t)
	}
	return list, rows.Err()
}

// Lookup returns the task at addr.
func (s *Service) Lookup(ctx context.Context, addr tree.Addr) (Task, error) {
	if addr.Kind() != "task" {
		return Task{}, fmt.Errorf("%w: %s is not a task", tree.ErrNotFound, addr)
	}
	list, err := s.query(ctx, `WHERE t.slug = ?`, strings.ReplaceAll(addr.Path(), "/", "--"))
	if err != nil {
		return Task{}, err
	}
	if len(list) == 0 {
		return Task{}, fmt.Errorf("%w: %s", tree.ErrNotFound, addr)
	}
	return list[0], nil
}

// SetLinear stores the task's Linear link; nil clears it.
func (s *Service) SetLinear(ctx context.Context, addr tree.Addr, link json.RawMessage) error {
	t, err := s.Lookup(ctx, addr)
	if err != nil {
		return err
	}
	var value any
	if link != nil {
		value = string(link)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE tasks SET linear = ? WHERE id = ?`, value, t.ID)
	return err
}

// subtree returns the task with slug and all its descendants.
func (s *Service) subtree(ctx context.Context, slug string) ([]Task, error) {
	return s.query(ctx, `WHERE t.slug = ? OR t.slug LIKE ? || '--%' ORDER BY t.slug`, slug, slug)
}

func (s *Service) get(ctx context.Context, a tree.Addr) (tree.Node, error) {
	t, err := s.Lookup(ctx, a)
	if err != nil {
		return tree.Node{}, err
	}
	files, err := s.listArtifacts(t.Root())
	if err != nil {
		return tree.Node{}, err
	}
	fields := map[string]any{
		"slug": t.Slug, "branch": t.Branch, "base": t.Base, "status": t.Status, "worktree": t.Worktree,
		"artifacts": t.Artifacts, "artifactFiles": files, "project": "project:" + t.Project, "parent": t.parent(),
		"createdBy": t.CreatedBy, "linear": t.Linear, "pr": t.PR,
	}
	if t.Status != "archived" {
		if st, err := gitx.StatusOf(ctx, t.Worktree); err == nil {
			fields["git"] = st
		}
	}
	return tree.Node{Title: t.Title, Fields: fields}, nil
}

func (s *Service) children(ctx context.Context, a tree.Addr) ([]tree.Summary, error) {
	slug := strings.ReplaceAll(a.Path(), "/", "--")
	subs, err := s.query(ctx, `WHERE t.parent_id = (SELECT id FROM tasks WHERE slug = ?) ORDER BY t.id`, slug)
	if err != nil {
		return nil, err
	}
	list := summaries(subs)
	if slug != rootOf(slug) {
		return list, nil
	}
	files, err := s.listArtifacts(slug)
	return append(list, files...), err
}

func (s *Service) projectTasks(ctx context.Context, a tree.Addr) ([]tree.Summary, error) {
	roots, err := s.query(ctx, `WHERE p.name = ? AND t.parent_id IS NULL ORDER BY t.id`, a.Path())
	return summaries(roots), err
}

func summaries(list []Task) []tree.Summary {
	out := make([]tree.Summary, len(list))
	for i, t := range list {
		out[i] = tree.Summary{Addr: t.Addr(), Kind: "task", Title: t.Title, Status: t.Status}
	}
	return out
}

// newTask is what create inserts and checks out.
type newTask struct {
	project, slug, title, branch, base, start string
	parentID                                  any
	by                                        tree.Actor
}

func (s *Service) createTask(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Title, Slug, Base, Branch string }
	json.Unmarshal(args, &a)
	slug := slugify(a.Slug)
	if a.Slug == "" {
		var err error
		if slug, err = s.freeSlug(ctx, slugify(a.Title)); err != nil {
			return tree.Result{}, err
		}
	}
	repo := n.Fields["path"].(string)
	base := cmp.Or(a.Base, n.Fields["defaultBranch"].(string))
	start := base
	if gitx.RefExists(ctx, repo, "origin/"+base) {
		start = "origin/" + base
	}
	return s.create(ctx, newTask{project: n.Title, slug: slug, title: a.Title, branch: cmp.Or(a.Branch, slug), base: base, start: start, by: by})
}

func (s *Service) createSubtask(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Name, Base string }
	json.Unmarshal(args, &a)
	parent, err := s.Lookup(ctx, n.Addr)
	if err != nil {
		return tree.Result{}, err
	}
	slug := parent.Slug + "--" + slugify(a.Name)
	base := parent.Branch
	if a.Base != "" {
		base = a.Base
		if sibling, err := s.Lookup(ctx, AddrOf(parent.Slug+"--"+slugify(a.Base))); err == nil {
			base = sibling.Branch
		}
	}
	return s.create(ctx, newTask{project: parent.Project, parentID: parent.ID, slug: slug, title: a.Name,
		branch: strings.ReplaceAll(slug, "--", "-"), base: base, start: base, by: by})
}

func (s *Service) create(ctx context.Context, nt newTask) (tree.Result, error) {
	worktree := filepath.Join(s.o.Home, "worktrees", nt.project, nt.slug)
	res, err := s.db.ExecContext(ctx, `INSERT INTO tasks (slug, parent_id, root_id, project_id, title, branch, base_ref, worktree, created_by)
		VALUES (?, ?, (SELECT root_id FROM tasks WHERE id = ?), (SELECT id FROM projects WHERE name = ?), ?, ?, ?, ?, ?)`,
		nt.slug, nt.parentID, nt.parentID, nt.project, nt.title, nt.branch, nt.base, worktree, nt.by.String())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return tree.Result{}, fmt.Errorf("task %s already exists", AddrOf(nt.slug))
		}
		return tree.Result{}, err
	}
	id, _ := res.LastInsertId()
	s.db.ExecContext(ctx, `UPDATE tasks SET root_id = id WHERE id = ? AND root_id IS NULL`, id)
	var repo string
	s.db.QueryRowContext(ctx, `SELECT path FROM projects WHERE name = ?`, nt.project).Scan(&repo)
	err = gitx.AddWorktree(ctx, repo, worktree, nt.branch, nt.start)
	if err == nil {
		err = os.MkdirAll(s.artifactsDir(rootOf(nt.slug)), 0o755)
	}
	if err != nil {
		s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
		return tree.Result{}, err
	}
	return tree.Result{
		Addr:    AddrOf(nt.slug),
		Message: fmt.Sprintf("created %s on branch %s from %s", AddrOf(nt.slug), nt.branch, nt.start),
		Data:    map[string]string{"branch": nt.branch, "worktree": worktree, "artifacts": s.artifactsDir(rootOf(nt.slug))},
	}, nil
}

// freeSlug returns base, or base-2, base-3… when taken.
func (s *Service) freeSlug(ctx context.Context, base string) (string, error) {
	for i := 1; ; i++ {
		slug := base
		if i > 1 {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE slug = ?`, slug).Scan(&n); err != nil || n == 0 {
			return slug, err
		}
	}
}

// slugify lowercases s and joins its runs of letters and digits with single dashes, at most 48 bytes.
func slugify(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
		} else {
			dash = true
		}
	}
	slug := b.String()
	if len(slug) > 48 {
		slug = strings.TrimRight(slug[:48], "-")
	}
	return cmp.Or(slug, "task")
}

func (s *Service) setStatus(status string) func(context.Context, tree.Node, json.RawMessage, tree.Actor) (tree.Result, error) {
	return func(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
		t, err := s.Lookup(ctx, n.Addr)
		if err != nil {
			return tree.Result{}, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = ? WHERE id = ?`, status, t.ID); err != nil {
			return tree.Result{}, err
		}
		s.tree.Changed(t.parent())
		return tree.Result{Message: fmt.Sprintf("%s is %s", t.Addr(), status)}, nil
	}
}

func (s *Service) reopen(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	t, err := s.Lookup(ctx, n.Addr)
	if err != nil {
		return tree.Result{}, err
	}
	if _, err := os.Stat(t.Worktree); errors.Is(err, os.ErrNotExist) {
		if err := gitx.AddWorktree(ctx, t.ProjectPath, t.Worktree, t.Branch, ""); err != nil {
			return tree.Result{}, err
		}
	}
	return s.setStatus("active")(ctx, n, args, by)
}

func (s *Service) archive(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	t, err := s.Lookup(ctx, n.Addr)
	if err != nil {
		return tree.Result{}, err
	}
	list, err := s.subtree(ctx, t.Slug)
	if err != nil {
		return tree.Result{}, err
	}
	changed := []tree.Addr{t.parent()}
	defer func() { s.tree.Changed(changed...) }()
	for _, sub := range slices.Backward(list) {
		if sub.Status == "archived" {
			continue
		}
		if _, err := os.Stat(sub.Worktree); err == nil {
			if err := gitx.RemoveWorktree(ctx, sub.ProjectPath, sub.Worktree); err != nil {
				return tree.Result{}, err
			}
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET status = 'archived' WHERE id = ?`, sub.ID); err != nil {
			return tree.Result{}, err
		}
		changed = append(changed, sub.Addr())
	}
	return tree.Result{Message: fmt.Sprintf("archived %d task(s); branches and artifacts kept", len(changed)-1)}, nil
}

func (s *Service) open(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ Target string }
	json.Unmarshal(args, &a)
	dir := n.Fields["worktree"].(string)
	argv := map[string][]string{
		"terminal": {"-a", s.o.Apps.Terminal, dir},
		"editor":   {"-a", s.o.Apps.Editor, dir},
		"finder":   {dir},
	}[a.Target]
	if err := s.o.Open(ctx, argv...); err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: "opened " + dir + " in " + a.Target}, nil
}
