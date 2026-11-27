// Package comments provides the comments a user leaves on artifact passages and diff lines,
// the comment kind agents resolve them through, and sending them to a session in one message.
package comments

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Comment is one comment as the tree shows it.
type Comment struct {
	Addr       tree.Addr `json:"addr"`
	Target     string    `json:"target"`
	Task       tree.Addr `json:"task"`
	Artifact   tree.Addr `json:"artifact,omitempty"`
	Path       string    `json:"path"`
	Line       int       `json:"line,omitempty"`
	LineEnd    int       `json:"lineEnd,omitempty"`
	Side       string    `json:"side,omitempty"`
	Quote      string    `json:"quote,omitempty"`
	Body       string    `json:"body"`
	Status     string    `json:"status"`
	Reply      string    `json:"reply,omitempty"`
	Author     string    `json:"author"`
	ResolvedBy string    `json:"resolvedBy,omitempty"`
	SentTo     string    `json:"sentTo,omitempty"`
	CreatedAt  string    `json:"createdAt"`

	root string
}

// owners returns the nodes that show the comment besides its own.
func (c Comment) owners() []tree.Addr {
	if c.Artifact != "" {
		return []tree.Addr{c.Artifact}
	}
	return []tree.Addr{c.Task}
}

// Service owns the comment kind and the comment actions on artifacts and tasks.
type Service struct {
	db   *sql.DB
	tree *tree.Tree
}

// Register adds the comment kind, add-comment on artifacts and tasks, send-comments on tasks,
// and the comments field on both.
func Register(t *tree.Tree, db *sql.DB) *Service {
	s := &Service{db: db, tree: t}
	t.Register(tree.Kind{
		Name: "comment",
		Get:  s.get,
		Actions: []tree.Action{
			{Name: "resolve", Help: "Mark the comment addressed, with a one-line reply saying what changed", When: statusIn("open", "sent"), Run: s.resolve,
				Args: json.RawMessage(`{"type":"object","properties":{"reply":{"type":"string"}}}`)},
			{Name: "reopen", Help: "Make the comment open again", When: statusIn("sent", "resolved"), Run: s.setStatus("open")},
			{Name: "edit", Help: "Replace the comment's text", When: statusIn("open", "sent"), Run: s.edit,
				Args: json.RawMessage(`{"type":"object","required":["body"],"properties":{"body":{"type":"string"}}}`)},
			{Name: "delete", Help: "Delete the comment", Run: s.delete},
		},
	})
	t.Extend("artifact", tree.Action{
		Name: "add-comment", Help: "Comment on the artifact, or on a passage of it", When: func(n tree.Node) bool { return n.Fields["exists"] == true },
		Run: s.addArtifactComment,
		Args: json.RawMessage(`{"type":"object","required":["body"],"properties":{
			"body":{"type":"string"},
			"quote":{"type":"string","description":"The passage commented on"},
			"line":{"type":"integer","description":"First source line of the passage, from 1"},
			"lineEnd":{"type":"integer","description":"Last source line of the passage"}}}`),
	})
	t.AddFields("artifact", s.artifactFields)
	t.AddChildren("artifact", s.artifactChildren)
	t.Extend("task",
		tree.Action{
			Name: "add-comment", Help: "Comment on a line of the task's diff", Run: s.addDiffComment,
			Args: json.RawMessage(`{"type":"object","required":["path","body"],"properties":{
				"path":{"type":"string","description":"File path relative to the worktree"},
				"body":{"type":"string"},
				"line":{"type":"integer","description":"Line number on the given side"},
				"lineEnd":{"type":"integer"},
				"side":{"enum":["old","new"],"description":"new (default) numbers lines in the current file, old in the base"},
				"quote":{"type":"string"}}}`),
		},
		tree.Action{
			Name: "send-comments", Help: "Send the open comments to a session in one message and mark them sent", When: hasOpen, Run: s.send,
			Args: json.RawMessage(`{"type":"object","properties":{
				"session":{"type":"string","description":"Session address or id8, defaults to the task's most recent live session"}}}`),
		},
	)
	t.AddFields("task", s.taskFields)
	return s
}

func statusIn(statuses ...string) func(tree.Node) bool {
	return func(n tree.Node) bool { return slices.Contains(statuses, n.Fields["status"].(string)) }
}

func hasOpen(n tree.Node) bool {
	list, _ := n.Fields["comments"].([]Comment)
	return slices.ContainsFunc(list, func(c Comment) bool { return c.Status == "open" })
}

const commentQuery = `SELECT c.id8, c.target, t.slug, c.path, c.line, c.line_end, c.side, c.quote, c.body, c.status, c.reply,
	c.author, c.resolved_by, c.sent_to, c.created_at
	FROM comments c JOIN tasks t ON t.id = c.task_id `

func (s *Service) query(ctx context.Context, where string, args ...any) ([]Comment, error) {
	rows, err := s.db.QueryContext(ctx, commentQuery+where+` ORDER BY c.created_at, c.rowid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Comment{}
	for rows.Next() {
		var c Comment
		var id8, slug string
		if err := rows.Scan(&id8, &c.Target, &slug, &c.Path, &c.Line, &c.LineEnd, &c.Side, &c.Quote, &c.Body, &c.Status, &c.Reply,
			&c.Author, &c.ResolvedBy, &c.SentTo, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Addr, c.Task, c.root = tree.Addr("comment:"+id8), tasks.AddrOf(slug), rootOf(slug)
		if c.Target == "artifact" {
			c.Artifact = tree.Addr("artifact:" + slug + "/" + c.Path)
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Service) load(ctx context.Context, a tree.Addr) (Comment, error) {
	list, err := s.query(ctx, `WHERE c.id8 = ?`, a.Path())
	if err != nil {
		return Comment{}, err
	}
	if a.Kind() != "comment" || len(list) == 0 {
		return Comment{}, fmt.Errorf("%w: %s", tree.ErrNotFound, a)
	}
	return list[0], nil
}

func (s *Service) get(ctx context.Context, a tree.Addr) (tree.Node, error) {
	c, err := s.load(ctx, a)
	if err != nil {
		return tree.Node{}, err
	}
	raw, _ := json.Marshal(c)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	return tree.Node{Title: title(c.Body), Fields: fields}, nil
}

func title(body string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(body), "\n")
	if r := []rune(line); len(r) > 60 {
		return string(r[:59]) + "…"
	}
	return line
}

func rootOf(slug string) string {
	root, _, _ := strings.Cut(slug, "--")
	return root
}

func slugOf(task tree.Addr) string { return strings.ReplaceAll(task.Path(), "/", "--") }

func (s *Service) artifactComments(ctx context.Context, a tree.Addr) ([]Comment, error) {
	root, rel, _ := strings.Cut(a.Path(), "/")
	return s.query(ctx, `WHERE c.target = 'artifact' AND t.slug = ? AND c.path = ?`, root, rel)
}

func (s *Service) artifactFields(ctx context.Context, n tree.Node) (map[string]any, error) {
	list, err := s.artifactComments(ctx, n.Addr)
	return map[string]any{"comments": list}, err
}

func (s *Service) artifactChildren(ctx context.Context, a tree.Addr) ([]tree.Summary, error) {
	list, err := s.artifactComments(ctx, a)
	out := make([]tree.Summary, len(list))
	for i, c := range list {
		out[i] = tree.Summary{Addr: c.Addr, Kind: "comment", Title: title(c.Body), Status: c.Status}
	}
	return out, err
}

// taskFields lists the task's diff comments and the comments on its tree's artifacts.
func (s *Service) taskFields(ctx context.Context, n tree.Node) (map[string]any, error) {
	slug := slugOf(n.Addr)
	list, err := s.query(ctx, `WHERE (c.target = 'diff' AND t.slug = ?) OR (c.target = 'artifact' AND t.slug = ?)`, slug, rootOf(slug))
	return map[string]any{"comments": list}, err
}

type place struct {
	Body, Quote, Side string
	Line, LineEnd     int
}

func (s *Service) insert(ctx context.Context, slug, target, path string, p place, by tree.Actor) (tree.Addr, error) {
	p.Body = strings.TrimSpace(p.Body)
	if p.Body == "" {
		return "", errors.New("comment body is empty")
	}
	if p.Line < 0 || p.LineEnd < 0 {
		return "", errors.New("line numbers start at 1")
	}
	p.LineEnd = max(p.LineEnd, p.Line)
	for {
		b := make([]byte, 4)
		rand.Read(b)
		id8 := hex.EncodeToString(b)
		_, err := s.db.ExecContext(ctx, `INSERT INTO comments (id8, task_id, target, path, line, line_end, side, quote, body, author)
			SELECT ?, id, ?, ?, ?, ?, ?, ?, ?, ? FROM tasks WHERE slug = ?`,
			id8, target, path, p.Line, p.LineEnd, p.Side, p.Quote, p.Body, by.String(), slug)
		if err == nil {
			return tree.Addr("comment:" + id8), nil
		}
		if !strings.Contains(err.Error(), "UNIQUE") {
			return "", err
		}
	}
}

func (s *Service) addArtifactComment(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var p place
	json.Unmarshal(args, &p)
	root, rel, _ := strings.Cut(n.Addr.Path(), "/")
	return s.added(ctx, root, "artifact", rel, p, by)
}

func (s *Service) addDiffComment(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var p struct {
		place
		Path string
	}
	json.Unmarshal(args, &p)
	if !filepath.IsLocal(p.Path) {
		return tree.Result{}, fmt.Errorf("path %q is not inside the worktree", p.Path)
	}
	if p.Side == "" {
		p.Side = "new"
	}
	return s.added(ctx, slugOf(n.Addr), "diff", filepath.ToSlash(filepath.Clean(p.Path)), p.place, by)
}

func (s *Service) added(ctx context.Context, slug, target, path string, p place, by tree.Actor) (tree.Result, error) {
	addr, err := s.insert(ctx, slug, target, path, p, by)
	if err != nil {
		return tree.Result{}, err
	}
	c, err := s.load(ctx, addr)
	if err != nil {
		return tree.Result{}, err
	}
	s.changed(ctx, c)
	return tree.Result{Addr: addr, Message: "added " + string(addr), Data: c}, nil
}

// changed reports the comment, the node it is on, and every task whose comments field lists it.
func (s *Service) changed(ctx context.Context, cs ...Comment) {
	var addrs []tree.Addr
	for _, c := range cs {
		addrs = append(addrs, c.Addr)
		addrs = append(addrs, c.owners()...)
		if c.Artifact == "" {
			continue
		}
		rows, err := s.db.QueryContext(ctx, `SELECT slug FROM tasks WHERE slug = ? OR slug LIKE ? || '--%'`, c.root, c.root)
		if err != nil {
			continue
		}
		for rows.Next() {
			var slug string
			rows.Scan(&slug)
			addrs = append(addrs, tasks.AddrOf(slug))
		}
		rows.Close()
	}
	s.tree.Changed(addrs...)
}

func (s *Service) update(ctx context.Context, n tree.Node, set string, args ...any) (Comment, error) {
	c, err := s.load(ctx, n.Addr)
	if err != nil {
		return c, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE comments SET `+set+` WHERE id8 = ?`, append(args, n.Addr.Path())...); err != nil {
		return c, err
	}
	s.changed(ctx, c)
	return c, nil
}

func (s *Service) resolve(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Reply string }
	json.Unmarshal(args, &a)
	_, err := s.update(ctx, n, `status = 'resolved', reply = ?, resolved_by = ?`, strings.TrimSpace(a.Reply), by.String())
	return tree.Result{Message: "resolved " + string(n.Addr)}, err
}

func (s *Service) setStatus(status string) func(context.Context, tree.Node, json.RawMessage, tree.Actor) (tree.Result, error) {
	return func(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
		_, err := s.update(ctx, n, `status = ?, reply = '', resolved_by = ''`, status)
		return tree.Result{Message: string(n.Addr) + " is " + status}, err
	}
}

func (s *Service) edit(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ Body string }
	json.Unmarshal(args, &a)
	if strings.TrimSpace(a.Body) == "" {
		return tree.Result{}, errors.New("comment body is empty")
	}
	_, err := s.update(ctx, n, `body = ?`, strings.TrimSpace(a.Body))
	return tree.Result{Message: "edited " + string(n.Addr)}, err
}

func (s *Service) delete(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	c, err := s.load(ctx, n.Addr)
	if err != nil {
		return tree.Result{}, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM comments WHERE id8 = ?`, n.Addr.Path()); err != nil {
		return tree.Result{}, err
	}
	s.changed(ctx, c)
	return tree.Result{Addr: c.owners()[0], Message: "deleted " + string(n.Addr)}, nil
}
