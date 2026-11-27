package linear

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Link is the linear field of a task linked to an issue.
type Link struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Title string `json:"title"`
	State State  `json:"state"`
}

func linkTo(i IssueRef) Link {
	return Link{ID: i.Identifier, URL: i.URL, Title: i.Title, State: i.State}
}

func (i Issue) ref() IssueRef {
	return IssueRef{ID: i.ID, Identifier: i.Identifier, Title: i.Title, URL: i.URL, State: i.State}
}

const (
	stateTTL     = time.Minute
	stateTimeout = 3 * time.Second
)

type cached struct {
	link Link
	at   time.Time
}

type links struct {
	c    *Client
	tree *tree.Tree
	ts   *tasks.Service

	mu    sync.Mutex
	cache map[string]cached
}

// RegisterTasks links tasks to Linear issues: task-from-issue on projects, link actions and live state on tasks,
// sub-issues on create-subtask and PR attachments on open-pr. It needs tasks.Register and github.Register first.
func RegisterTasks(t *tree.Tree, c *Client, ts *tasks.Service) {
	l := &links{c: c, tree: t, ts: ts, cache: map[string]cached{}}
	issueArg := json.RawMessage(`{"type":"object","required":["issue"],"properties":{"issue":{"type":"string","description":"Issue identifier such as ENG-482"}}}`)
	t.Extend("project", tree.Action{Name: "create-task-from-linear",
		Help: "Create a task from a Linear issue: its title, its branch name, and 01-ticket.md in the artifacts",
		Args: issueArg, Run: l.createTask})
	t.Extend("task",
		tree.Action{Name: "link-linear", Help: "Link the task to a Linear issue; a root task also gets 01-ticket.md",
			Args: issueArg, When: func(n tree.Node) bool { return n.Fields["status"] != "archived" }, Run: l.linkIssue},
		tree.Action{Name: "refresh-linear", Help: "Read the linked Linear issue now and rewrite 01-ticket.md", When: isLinked, Run: l.refresh},
		tree.Action{Name: "unlink-linear", Help: "Forget the task's Linear issue", When: isLinked, Run: l.unlink},
	)
	t.Wrap("task", "create-subtask", l.wrapSubtask)
	t.Wrap("task", "open-pr", l.wrapOpenPR)
	t.AddFields("task", l.fields)
}

func linkOf(n tree.Node) (Link, bool) {
	raw, _ := n.Fields["linear"].(json.RawMessage)
	var link Link
	json.Unmarshal(raw, &link)
	return link, link.ID != ""
}

func isLinked(n tree.Node) bool {
	_, ok := linkOf(n)
	return ok
}

var (
	refUnsafe = regexp.MustCompile(`[^A-Za-z0-9_/-]+`)
	slashes   = regexp.MustCompile(`/{2,}`)
)

// branchName makes Linear's suggested branch name safe for git.
func branchName(s string) string {
	s = slashes.ReplaceAllString(refUnsafe.ReplaceAllString(s, "-"), "/")
	return strings.Trim(s, "-/")
}

func (l *links) createTask(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Issue string }
	json.Unmarshal(args, &a)
	i, err := l.c.Issue(ctx, a.Issue)
	if err != nil {
		return tree.Result{}, err
	}
	create := map[string]string{"title": i.Title, "slug": i.Identifier}
	if b := branchName(i.BranchName); b != "" {
		create["branch"] = b
	}
	raw, _ := json.Marshal(create)
	res := l.tree.Do(ctx, by, []tree.Op{{Target: n.Addr, Action: "create-task", Args: raw}})[0]
	if !res.OK {
		return tree.Result{}, errors.New(res.Message)
	}
	if err := l.link(ctx, by, res.Addr, i); err != nil {
		return tree.Result{}, err
	}
	res.Message += ", linked to " + i.Identifier
	return res, nil
}

func (l *links) linkIssue(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Issue string }
	json.Unmarshal(args, &a)
	i, err := l.c.Issue(ctx, a.Issue)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("%s is linked to %s", n.Addr, i.Identifier)}, l.link(ctx, by, n.Addr, i)
}

func (l *links) refresh(ctx context.Context, n tree.Node, _ json.RawMessage, by tree.Actor) (tree.Result, error) {
	link, _ := linkOf(n)
	i, err := l.c.Issue(ctx, link.ID)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("%s is %s", i.Identifier, i.State.Name)}, l.link(ctx, by, n.Addr, i)
}

func (l *links) unlink(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	link, _ := linkOf(n)
	return tree.Result{Message: fmt.Sprintf("%s is no longer linked to %s", n.Addr, link.ID)}, l.ts.SetLinear(ctx, n.Addr, nil)
}

// link stores the issue on the task and, for a root task, writes it as 01-ticket.md.
func (l *links) link(ctx context.Context, by tree.Actor, task tree.Addr, i Issue) error {
	if err := l.store(ctx, task, linkTo(i.ref())); err != nil {
		return err
	}
	if strings.Contains(task.Path(), "/") {
		return nil
	}
	raw, _ := json.Marshal(map[string]string{"content": TicketMarkdown(i)})
	if res := l.tree.Do(ctx, by, []tree.Op{{Target: tree.Addr("artifact:" + task.Path() + "/01-ticket.md"), Action: "write", Args: raw}})[0]; !res.OK {
		return errors.New(res.Message)
	}
	return nil
}

func (l *links) store(ctx context.Context, task tree.Addr, link Link) error {
	raw, _ := json.Marshal(link)
	if err := l.ts.SetLinear(ctx, task, raw); err != nil {
		return err
	}
	l.mu.Lock()
	l.cache[link.ID] = cached{link: link, at: time.Now()}
	l.mu.Unlock()
	return nil
}

func (l *links) wrapSubtask(a tree.Action) tree.Action {
	var schema map[string]any
	json.Unmarshal(a.Args, &schema)
	schema["properties"].(map[string]any)["linear"] = map[string]any{"type": "boolean",
		"description": "Also create a Linear sub-issue under this task's issue and link it"}
	a.Args, _ = json.Marshal(schema)
	run := a.Run
	a.Run = func(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
		var opt struct {
			Name   string
			Linear bool
		}
		json.Unmarshal(args, &opt)
		if !opt.Linear {
			return run(ctx, n, args, by)
		}
		parent, ok := linkOf(n)
		if !ok {
			return tree.Result{}, fmt.Errorf("%s is not linked to a Linear issue", n.Addr)
		}
		res, err := run(ctx, n, args, by)
		if err != nil {
			return res, err
		}
		sub, err := l.c.CreateSubIssue(ctx, parent.ID, opt.Name, "")
		if err == nil {
			err = l.store(ctx, res.Addr, linkTo(sub))
		}
		if err != nil {
			res.Message += "; Linear sub-issue failed: " + err.Error()
		} else {
			res.Message += ", linked to new sub-issue " + sub.Identifier
		}
		return res, nil
	}
	return a
}

func (l *links) wrapOpenPR(a tree.Action) tree.Action {
	run := a.Run
	a.Run = func(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
		res, err := run(ctx, n, args, by)
		link, linked := linkOf(n)
		if err != nil || !linked {
			return res, err
		}
		var pr struct {
			Number int
			URL    string
		}
		data, _ := json.Marshal(res.Data)
		json.Unmarshal(data, &pr)
		if err := l.c.AttachURL(ctx, link.ID, pr.URL, fmt.Sprintf("PR #%d %s", pr.Number, n.Title)); err != nil {
			res.Message += fmt.Sprintf("; attaching to %s failed: %v", link.ID, err)
		} else {
			res.Message += ", attached to " + link.ID
		}
		return res, nil
	}
	return a
}

// fields replaces a linked task's linear field with the issue's current state, read at most once a minute.
func (l *links) fields(ctx context.Context, n tree.Node) (map[string]any, error) {
	stored, ok := linkOf(n)
	if !ok {
		return nil, nil
	}
	current := l.current(ctx, stored)
	raw, _ := json.Marshal(current)
	if current != stored {
		if err := l.ts.SetLinear(ctx, n.Addr, raw); err != nil {
			return nil, err
		}
	}
	return map[string]any{"linear": json.RawMessage(raw)}, nil
}

// current returns the cached link when fresh, else reads the issue, keeping stored when Linear fails.
func (l *links) current(ctx context.Context, stored Link) Link {
	l.mu.Lock()
	c, ok := l.cache[stored.ID]
	l.mu.Unlock()
	if ok && time.Since(c.at) < stateTTL {
		return c.link
	}
	ctx, cancel := context.WithTimeout(ctx, stateTimeout)
	defer cancel()
	link := stored
	if i, err := l.c.Issue(ctx, stored.ID); err == nil {
		link = linkTo(i.ref())
	}
	l.mu.Lock()
	l.cache[stored.ID] = cached{link: link, at: time.Now()}
	l.mu.Unlock()
	return link
}
