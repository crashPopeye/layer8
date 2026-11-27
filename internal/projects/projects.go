// Package projects provides the app root node and the project nodes under it.
package projects

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/crashPopeye/layer8/internal/tree"
)

var namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

type projects struct {
	db   *sql.DB
	tree *tree.Tree
}

// Register adds the app and project kinds to t.
func Register(t *tree.Tree, db *sql.DB, version string) {
	p := projects{db: db, tree: t}
	t.Register(tree.Kind{
		Name: "app",
		Get: func(ctx context.Context, a tree.Addr) (tree.Node, error) {
			if a != "app" {
				return tree.Node{}, fmt.Errorf("%w: %s", tree.ErrNotFound, a)
			}
			return tree.Node{Title: "layer8", Fields: map[string]any{"version": version}}, nil
		},
		Children: p.list,
		Actions: []tree.Action{{
			Name: "add-project",
			Help: "Add a local git repository as a project",
			Args: json.RawMessage(`{"type":"object","required":["path"],"properties":{
				"path":{"type":"string","description":"Absolute path inside a git repository"},
				"name":{"type":"string","description":"Project name, defaults to the repository folder name"}}}`),
			Run: p.add,
		}},
	})
	t.Register(tree.Kind{
		Name: "project",
		Get:  p.get,
		Actions: []tree.Action{
			{Name: "rename", Help: "Rename the project",
				Args: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
				Run:  p.rename},
			{Name: "remove", Help: "Remove the project from layer8; the repository stays on disk", Run: p.remove},
		},
	})
}

func (p projects) list(ctx context.Context, _ tree.Addr) ([]tree.Summary, error) {
	rows, err := p.db.QueryContext(ctx, `SELECT name FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []tree.Summary
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		list = append(list, tree.Summary{Addr: tree.Addr("project:" + name), Title: name})
	}
	return list, rows.Err()
}

func (p projects) get(ctx context.Context, a tree.Addr) (tree.Node, error) {
	var path, branch string
	err := p.db.QueryRowContext(ctx, `SELECT path, default_branch FROM projects WHERE name = ?`, a.Path()).Scan(&path, &branch)
	if errors.Is(err, sql.ErrNoRows) {
		return tree.Node{}, fmt.Errorf("%w: %s", tree.ErrNotFound, a)
	}
	if err != nil {
		return tree.Node{}, err
	}
	return tree.Node{Title: a.Path(), Fields: map[string]any{"path": path, "defaultBranch": branch}}, nil
}

func (p projects) add(ctx context.Context, _ tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Path, Name string }
	json.Unmarshal(args, &a)
	if !filepath.IsAbs(a.Path) {
		return tree.Result{}, fmt.Errorf("path must be absolute: %q", a.Path)
	}
	root, err := git(ctx, a.Path, "rev-parse", "--show-toplevel")
	if err != nil {
		return tree.Result{}, fmt.Errorf("%s is not inside a git repository", a.Path)
	}
	name := a.Name
	if name == "" {
		name = filepath.Base(root)
	}
	if err := p.checkFree(ctx, name, root); err != nil {
		return tree.Result{}, err
	}
	_, err = p.db.ExecContext(ctx, `INSERT INTO projects (name, path, default_branch, created_by) VALUES (?, ?, ?, ?)`,
		name, root, defaultBranch(ctx, root), by.String())
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Addr: tree.Addr("project:" + name), Message: "added project " + name}, nil
}

func (p projects) rename(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ Name string }
	json.Unmarshal(args, &a)
	if err := p.checkFree(ctx, a.Name, ""); err != nil {
		return tree.Result{}, err
	}
	if _, err := p.db.ExecContext(ctx, `UPDATE projects SET name = ? WHERE name = ?`, a.Name, n.Title); err != nil {
		return tree.Result{}, err
	}
	p.tree.Changed("app")
	return tree.Result{Addr: tree.Addr("project:" + a.Name), Message: "renamed " + n.Title + " to " + a.Name}, nil
}

func (p projects) remove(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	if _, err := p.db.ExecContext(ctx, `DELETE FROM projects WHERE name = ?`, n.Title); err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Addr: "app", Message: "removed project " + n.Title}, nil
}

// checkFree reports an error when name is invalid or taken, or path is already a project.
func (p projects) checkFree(ctx context.Context, name, path string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("invalid project name %q: use letters, digits, '.', '_' and '-'", name)
	}
	var existing, existingPath string
	err := p.db.QueryRowContext(ctx, `SELECT name, path FROM projects WHERE name = ? OR path = ?`, name, path).Scan(&existing, &existingPath)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return err
	case existingPath == path:
		return fmt.Errorf("%s is already project %q", path, existing)
	default:
		return fmt.Errorf("project %q already exists", name)
	}
}

// defaultBranch is origin's HEAD branch, else the checked-out branch, else main.
func defaultBranch(ctx context.Context, repo string) string {
	if ref, err := git(ctx, repo, "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil {
		return strings.TrimPrefix(ref, "origin/")
	}
	if ref, err := git(ctx, repo, "symbolic-ref", "--short", "HEAD"); err == nil {
		return ref
	}
	return "main"
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}
