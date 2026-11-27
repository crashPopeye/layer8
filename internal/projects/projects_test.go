package projects_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tree"
)

// gitRepo creates an empty repository on branch trunk and returns its resolved path.
func gitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("git", "init", "-q", "-b", "trunk", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	dir, _ = filepath.EvalSymlinks(dir)
	return dir
}

func TestProjectLifecycle(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tr := tree.New(db)
	projects.Register(tr, db, "test")
	ctx := context.Background()
	user := tree.Actor{Kind: "user"}
	repo := gitRepo(t, "api-gateway")
	os.Mkdir(filepath.Join(repo, "sub"), 0o755)
	plain := t.TempDir()

	do := func(target tree.Addr, action, args string) tree.Result {
		t.Helper()
		return tr.Do(ctx, user, []tree.Op{{Target: target, Action: action, Args: json.RawMessage(args)}})[0]
	}
	expect := func(r tree.Result, ok bool, msg string) {
		t.Helper()
		if r.OK != ok || !strings.Contains(r.Message, msg) {
			t.Fatalf("got %+v, want ok=%v message~%q", r, ok, msg)
		}
	}

	expect(do("app", "add-project", `{"path":"relative/dir"}`), false, "must be absolute")
	expect(do("app", "add-project", `{"path":"`+plain+`"}`), false, "not inside a git repository")
	r := do("app", "add-project", `{"path":"`+filepath.Join(repo, "sub")+`"}`)
	expect(r, true, "added project api-gateway")
	if r.Addr != "project:api-gateway" {
		t.Fatalf("addr = %q", r.Addr)
	}
	expect(do("app", "add-project", `{"path":"`+repo+`","name":"other"}`), false, "already project")
	expect(do("app", "add-project", `{"path":"`+gitRepo(t, "x")+`","name":"api-gateway"}`), false, "already exists")
	expect(do("app", "add-project", `{"path":"`+gitRepo(t, "y")+`","name":"bad name"}`), false, "invalid project name")

	n, err := tr.Get(ctx, user, "project:api-gateway", 1)
	if err != nil {
		t.Fatal(err)
	}
	if n.Fields["path"] != repo || n.Fields["defaultBranch"] != "trunk" {
		t.Fatalf("fields = %v", n.Fields)
	}

	expect(do("project:api-gateway", "rename", `{"name":"gateway"}`), true, "renamed api-gateway to gateway")
	app, _ := tr.Get(ctx, user, "app", 1)
	if len(app.Children) != 1 || app.Children[0].Addr != "project:gateway" {
		t.Fatalf("app children = %+v", app.Children)
	}

	expect(do("project:gateway", "remove", ""), true, "removed project gateway")
	if _, err := tr.Get(ctx, user, "project:gateway", 0); err == nil {
		t.Fatal("removed project still readable")
	}
	if _, err := os.Stat(repo); err != nil {
		t.Fatal("repository must stay on disk")
	}
}
