package diff

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Register adds the changes, ahead and behind fields and the commit and push actions to tasks.
func Register(t *tree.Tree) {
	t.AddFields("task", fields)
	t.Extend("task",
		tree.Action{Name: "commit", Help: "Commit the worktree's changes, or only the given paths", When: hasUncommitted, Run: commit,
			Args: json.RawMessage(`{"type":"object","required":["message"],"properties":{
				"message":{"type":"string"},
				"paths":{"type":"array","items":{"type":"string"},"description":"Paths to commit, all changes by default"}}}`)},
		tree.Action{Name: "push", Help: "Push the branch to origin, replacing it only if it has not moved since", When: hasCommits, Run: push},
	)
}

func fields(ctx context.Context, n tree.Node) (map[string]any, error) {
	if str(n, "status") == "archived" {
		return nil, nil
	}
	dir, base := str(n, "worktree"), str(n, "base")
	stats, err := Summarize(ctx, dir, base)
	if err != nil {
		return nil, nil
	}
	ahead, behind, err := AheadBehind(ctx, dir, base)
	if err != nil {
		return nil, nil
	}
	return map[string]any{"changes": stats, "ahead": ahead, "behind": behind}, nil
}

func str(n tree.Node, field string) string {
	s, _ := n.Fields[field].(string)
	return s
}

func hasUncommitted(n tree.Node) bool {
	st, ok := n.Fields["git"].(gitx.Status)
	return str(n, "status") == "active" && ok && st.Changed > 0
}

func hasCommits(n tree.Node) bool {
	ahead, _ := n.Fields["ahead"].(int)
	return str(n, "status") == "active" && ahead > 0
}

func commit(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct {
		Message string
		Paths   []string
	}
	json.Unmarshal(args, &a)
	if strings.TrimSpace(a.Message) == "" {
		return tree.Result{}, fmt.Errorf("commit message is empty")
	}
	dir := str(n, "worktree")
	paths := append([]string{"--"}, a.Paths...)
	if _, err := gitx.Run(ctx, dir, append([]string{"add", "--all"}, paths...)...); err != nil {
		return tree.Result{}, err
	}
	if len(a.Paths) == 0 {
		paths = nil
	}
	if _, err := gitx.Run(ctx, dir, append([]string{"commit", "--quiet", "-m", a.Message}, paths...)...); err != nil {
		return tree.Result{}, err
	}
	sha, err := gitx.Run(ctx, dir, "rev-parse", "--short", "HEAD")
	return tree.Result{Message: "committed " + sha, Data: map[string]string{"sha": sha}}, err
}

func push(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	branch := str(n, "branch")
	if err := Push(ctx, str(n, "worktree"), branch); err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: "pushed " + branch + " to origin"}, nil
}

// Push pushes branch to origin and tracks it, replacing the remote branch only if it is still at the sha last fetched.
func Push(ctx context.Context, dir, branch string) error {
	ref := "refs/heads/" + branch
	seen, _ := gitx.Run(ctx, dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch)
	_, err := gitx.RunEnv(ctx, dir, []string{"GIT_TERMINAL_PROMPT=0"}, "push", "--quiet", "--force-with-lease="+ref+":"+seen, "--set-upstream", "origin", branch+":"+ref)
	return err
}

// Handler serves GET /api/diff?task=<addr>&mode=all|uncommitted|committed.
func Handler(t *tree.Tree) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		n, err := t.Get(r.Context(), tree.Actor{Kind: "user"}, tree.Addr(q.Get("task")), 0)
		if err == nil && n.Kind != "task" {
			err = fmt.Errorf("%s is not a task", n.Addr)
		}
		if err != nil {
			reply(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		d, err := Compute(r.Context(), str(n, "worktree"), str(n, "base"), Mode(cmp.Or(q.Get("mode"), "all")))
		if err != nil {
			reply(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		reply(w, http.StatusOK, d)
	})
}

func reply(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(body)
}
