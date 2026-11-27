// Package testkit builds real git repositories and a task tree for tests.
package testkit

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Env is a tree with the project and task kinds over a repository whose origin is a local bare clone.
type Env struct {
	T      *testing.T
	Tree   *tree.Tree
	DB     *sql.DB
	Tasks  *tasks.Service
	Home   string
	Repo   string
	Origin string
}

// Git runs git in dir with a fixed identity and fails the test on error.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitx.Run(t.Context(), dir, append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Write creates or replaces a file under dir.
func Write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// New creates repository "api" on branch main with files, pushes it to a bare origin, and adds it as a project.
func New(t *testing.T, files map[string]string, register func(*Env)) *Env {
	t.Helper()
	root, _ := filepath.EvalSymlinks(t.TempDir())
	e := &Env{T: t, Home: filepath.Join(root, "home"), Repo: filepath.Join(root, "api"), Origin: filepath.Join(root, "origin.git")}
	Git(t, root, "init", "-q", "-b", "main", e.Repo)
	for name, content := range files {
		Write(t, e.Repo, name, content)
	}
	Git(t, e.Repo, "add", "--all")
	Git(t, e.Repo, "commit", "-q", "-m", "init")
	Git(t, root, "clone", "-q", "--bare", e.Repo, e.Origin)
	Git(t, e.Repo, "remote", "add", "origin", e.Origin)
	Git(t, e.Repo, "fetch", "-q", "origin")

	db, err := store.Open(filepath.Join(root, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e.DB, e.Tree = db, tree.New(db)
	projects.Register(e.Tree, db, "test")
	e.Tasks = tasks.Register(e.Tree, db, tasks.Options{Home: e.Home})
	register(e)
	e.OK("app", "add-project", map[string]any{"path": e.Repo})
	return e
}

// Do runs one action as the user.
func (e *Env) Do(target tree.Addr, action string, args any) tree.Result {
	e.T.Helper()
	raw, _ := json.Marshal(args)
	return e.Tree.Do(e.T.Context(), tree.Actor{Kind: "user"}, []tree.Op{{Target: target, Action: action, Args: raw}})[0]
}

// OK runs one action as the user and fails the test unless it succeeds.
func (e *Env) OK(target tree.Addr, action string, args any) tree.Result {
	e.T.Helper()
	r := e.Do(target, action, args)
	if !r.OK {
		e.T.Fatalf("%s %s: %s", target, action, r.Message)
	}
	return r
}

// Get reads a node without children.
func (e *Env) Get(addr tree.Addr) tree.Node {
	e.T.Helper()
	n, err := e.Tree.Get(e.T.Context(), tree.Actor{Kind: "user"}, addr, 0)
	if err != nil {
		e.T.Fatal(err)
	}
	return n
}

// Has reports whether the node offers the action now.
func Has(n tree.Node, action string) bool {
	return slices.ContainsFunc(n.Actions, func(a tree.Action) bool { return a.Name == action })
}
