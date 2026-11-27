package github

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/diff"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Service owns the pull request actions of tasks and refreshes open pull requests.
type Service struct {
	db   *sql.DB
	tree *tree.Tree
	gh   *Client
}

// Register adds the open-pr and refresh-pr actions to tasks; it needs diff.Register for the ahead field.
func Register(t *tree.Tree, db *sql.DB, gh *Client) *Service {
	s := &Service{db: db, tree: t, gh: gh}
	t.Extend("task",
		tree.Action{Name: "open-pr", Help: "Push the branch and open its pull request against the task's base, or link the one already open",
			When: canOpen, Run: s.openPR,
			Args: json.RawMessage(`{"type":"object","properties":{
				"title":{"type":"string","description":"Defaults to the task title"},
				"body":{"type":"string","description":"Defaults to the pr-description.md artifact"},
				"draft":{"type":"boolean"}}}`)},
		tree.Action{Name: "refresh-pr", Help: "Read the pull request's state, checks and review threads now", When: hasPR, Run: s.refreshPR},
	)
	return s
}

func str(n tree.Node, field string) string {
	s, _ := n.Fields[field].(string)
	return s
}

func prOf(n tree.Node) *PR {
	raw, _ := n.Fields["pr"].(json.RawMessage)
	var pr *PR
	json.Unmarshal(raw, &pr)
	return pr
}

func canOpen(n tree.Node) bool {
	ahead, _ := n.Fields["ahead"].(int)
	pr := prOf(n)
	return str(n, "status") == "active" && ahead > 0 && (pr == nil || pr.State != "open")
}

func hasPR(n tree.Node) bool {
	return str(n, "status") != "archived" && prOf(n) != nil
}

func (s *Service) openPR(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct {
		Title, Body string
		Draft       bool
	}
	json.Unmarshal(args, &a)
	dir, branch := str(n, "worktree"), str(n, "branch")
	if err := diff.Push(ctx, dir, branch); err != nil {
		return tree.Result{}, err
	}
	repo, err := Repo(ctx, dir)
	if err != nil {
		return tree.Result{}, err
	}
	number, err := s.gh.FindOpenPR(ctx, repo, branch)
	if err != nil {
		return tree.Result{}, err
	}
	verb := "linked"
	if number == 0 {
		if a.Body == "" {
			body, _ := os.ReadFile(filepath.Join(str(n, "artifacts"), "pr-description.md"))
			a.Body = string(body)
		}
		pr := NewPR{Title: cmp.Or(a.Title, n.Title), Body: a.Body, Head: branch, Base: str(n, "base"), Draft: a.Draft}
		if number, err = s.gh.CreatePR(ctx, repo, pr); err != nil {
			return tree.Result{}, err
		}
		verb = "opened"
	}
	pr, err := s.refresh(ctx, n, number)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("%s PR #%d %s", verb, pr.Number, pr.URL), Data: pr}, nil
}

func (s *Service) refreshPR(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	pr, err := s.refresh(ctx, n, prOf(n).Number)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("PR #%d is %s, checks %s", pr.Number, pr.State, cmp.Or(pr.ChecksState, "none")), Data: pr}, nil
}

// refresh reads the pull request, fetches log tails of newly failed jobs, and stores it on the task when it changed.
func (s *Service) refresh(ctx context.Context, n tree.Node, number int) (PR, error) {
	repo, err := Repo(ctx, str(n, "worktree"))
	if err != nil {
		return PR{}, err
	}
	pr, err := s.gh.Status(ctx, repo, number)
	if err != nil {
		return PR{}, err
	}
	var seen []Check
	if prev := prOf(n); prev != nil {
		seen = prev.Checks
	}
	for i, c := range pr.Checks {
		if c.State != "failure" || c.Job == 0 {
			continue
		}
		if j := slices.IndexFunc(seen, func(o Check) bool { return o.Job == c.Job && o.LogTail != "" }); j >= 0 {
			pr.Checks[i].LogTail = seen[j].LogTail
		} else if tail, err := s.gh.LogTail(ctx, repo, c.Job, 40); err == nil {
			pr.Checks[i].LogTail = tail
		}
	}
	data, _ := json.Marshal(pr)
	if raw, _ := n.Fields["pr"].(json.RawMessage); string(raw) == string(data) {
		return pr, nil
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET pr = ? WHERE slug = ?`, string(data), str(n, "slug")); err != nil {
		return PR{}, err
	}
	s.tree.Changed(n.Addr)
	return pr, nil
}

// Poll refreshes the open pull requests every interval until ctx ends.
func (s *Service) Poll(ctx context.Context, every time.Duration) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
			s.RefreshOpen(ctx)
		}
	}
}

// RefreshOpen refreshes the pull request of every active task whose pull request is open.
func (s *Service) RefreshOpen(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug FROM tasks WHERE status = 'active' AND json_extract(pr, '$.state') = 'open'`)
	if err != nil {
		slog.Warn("listing open pull requests failed", "err", err)
		return
	}
	var slugs []string
	for rows.Next() {
		var slug string
		if rows.Scan(&slug) == nil {
			slugs = append(slugs, slug)
		}
	}
	rows.Close()
	for _, slug := range slugs {
		addr := tree.Addr("task:" + strings.ReplaceAll(slug, "--", "/"))
		n, err := s.tree.Get(ctx, tree.Actor{Kind: "user"}, addr, 0)
		if err == nil {
			_, err = s.refresh(ctx, n, prOf(n).Number)
		}
		if err != nil {
			slog.Warn("pull request refresh failed", "task", addr, "err", err)
		}
	}
}
