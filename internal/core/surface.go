package core

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/store"
)

// Node is one thing in layer8 as agents, the CLI and ⌘K see it: a ref to name it,
// a few fields, the verbs that apply to it now, and the nodes it contains.
type Node struct {
	Ref      string  `json:"ref"`
	Kind     string  `json:"kind"`
	Title    string  `json:"title"`
	Fields   []Field `json:"fields,omitempty"`
	Verbs    []*Verb `json:"verbs,omitempty"`
	Children []*Node `json:"children,omitempty"`
	ID       string  `json:"id,omitempty"`

	aliases []string
	task    *store.Task
	slice   *store.Slice
	session *store.Session
}

type Field struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Verb is an action on one kind of node. Declaring it here is all it takes for it
// to show up in look, act, ⌘K and the CLI.
type Verb struct {
	Kind string                                                   `json:"-"`
	Name string                                                   `json:"name"`
	Help string                                                   `json:"help"`
	Args []Arg                                                    `json:"args,omitempty"`
	When func(n *Node) bool                                       `json:"-"`
	Run  func(ctx context.Context, n *Node, in Args) (any, error) `json:"-"`
}

type Arg struct {
	Name     string   `json:"name"`
	Enum     []string `json:"enum,omitempty"`
	Optional bool     `json:"optional,omitempty"`
}

type Args map[string]any

func (in Args) str(k string) string {
	s, _ := in[k].(string)
	return strings.TrimSpace(s)
}

// Focus is what the user is looking at in the window.
type Focus struct {
	TaskID    string `json:"taskId"`
	SliceID   string `json:"sliceId"`
	SessionID string `json:"sessionId"`
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (n *Node) add(k, v string) {
	if v != "" {
		n.Fields = append(n.Fields, Field{k, v})
	}
}

func (n *Node) walk(fn func(*Node)) {
	fn(n)
	for _, c := range n.Children {
		c.walk(fn)
	}
}

// Tree builds the whole node tree from the current state, with each node's verbs.
func (a *App) Tree() (*Node, error) {
	s, err := a.State()
	if err != nil {
		return nil, err
	}
	root := &Node{Ref: "app", Kind: "app", Title: "layer8"}
	root.add("version", s.Version)
	root.add("agents", fmt.Sprintf("%d/%d running, %d queued", s.Running, s.MaxRunning, s.Queued))
	root.add("approvals", fmt.Sprint(len(s.Approvals)))

	waiting := map[string]string{}
	for _, ap := range s.Approvals {
		waiting[ap.SessionID] = ap.ToolName
	}
	sessionNode := func(x store.Session) *Node {
		n := &Node{Ref: "session:" + short(x.ID), Kind: "session", Title: x.Title, ID: x.ID, session: &x}
		n.add("kind", x.Kind)
		n.add("status", x.Status)
		n.add("harness", x.Harness)
		n.add("model", x.Model)
		n.add("waiting", waiting[x.ID])
		return n
	}
	for _, p := range s.Projects {
		pn := &Node{Ref: "project:" + short(p.ID), Kind: "project", Title: p.Name, ID: p.ID, aliases: []string{p.Name}}
		var names []string
		for _, f := range s.Folders {
			if f.ProjectID != p.ID {
				continue
			}
			names = append(names, f.Name)
			for _, t := range s.Tasks {
				if t.FolderID != f.ID || t.Status != "active" {
					continue
				}
				tn := &Node{Ref: "task:" + t.Slug, Kind: "task", Title: t.Title, ID: t.ID, aliases: []string{t.Slug}, task: &t}
				if t.Linear != nil {
					tn.add("linear", t.Linear.Identifier)
					tn.aliases = append(tn.aliases, t.Linear.Identifier)
				}
				tn.add("folder", f.Name)
				tn.add("branch", t.Branch)
				tn.add("base", t.BaseRef)
				tn.add("sync", t.SyncState)
				tn.add("artifacts", t.ArtifactsPath)
				for _, x := range s.Slices {
					if x.TaskID != t.ID {
						continue
					}
					sn := &Node{Ref: "slice:" + t.Slug + "/" + x.Key, Kind: "slice", Title: x.Title, ID: x.ID, aliases: []string{t.Slug + "/" + x.Key}, task: &t, slice: &x}
					if x.PRNumber > 0 {
						sn.add("pr", fmt.Sprintf("#%d %s", x.PRNumber, x.PRState))
					}
					sn.add("ci", x.CIState)
					sn.add("review", x.ReviewState)
					if x.OpenThreads > 0 {
						sn.add("threads", fmt.Sprint(x.OpenThreads))
					}
					sn.add("sync", x.SyncState)
					tn.Children = append(tn.Children, sn)
				}
				for _, se := range s.Sessions {
					if se.TaskID != t.ID {
						continue
					}
					n := sessionNode(se)
					n.task = &t
					parent := tn
					for _, c := range tn.Children {
						if c.slice != nil && c.slice.ID == se.SliceID {
							parent, n.slice = c, c.slice
						}
					}
					parent.Children = append(parent.Children, n)
				}
				pn.Children = append(pn.Children, tn)
			}
		}
		pn.add("folders", strings.Join(names, ", "))
		root.Children = append(root.Children, pn)
	}

	f := a.Focus()
	root.walk(func(n *Node) {
		if n.ID != "" && (n.ID == f.SessionID || (f.SessionID == "" && (n.ID == f.SliceID || (f.SliceID == "" && n.ID == f.TaskID)))) {
			root.add("focus", n.Ref)
		}
		for _, v := range a.verbs {
			if v.Kind == n.Kind && (v.When == nil || v.When(n)) {
				n.Verbs = append(n.Verbs, v)
			}
		}
	})
	return root, nil
}

// Find resolves a ref like task:eng-123, slice:eng-123/pr1, session:9c41e0aa,
// or any kind followed by an id prefix.
func (root *Node) Find(ref string) (*Node, error) {
	if ref == "" || ref == "app" {
		return root, nil
	}
	kind, key, ok := strings.Cut(ref, ":")
	if !ok || key == "" {
		return nil, fmt.Errorf("bad ref %q: use kind:key, like task:eng-123", ref)
	}
	var hits []*Node
	root.walk(func(n *Node) {
		if n.Kind != kind {
			return
		}
		if n.Ref == ref || slices.ContainsFunc(n.aliases, func(s string) bool { return strings.EqualFold(s, key) }) || (len(key) >= 4 && strings.HasPrefix(n.ID, key)) {
			hits = append(hits, n)
		}
	})
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return nil, fmt.Errorf("%w: no %s matches %q (look shows every ref)", store.ErrNotFound, kind, key)
	}
	return nil, fmt.Errorf("%q matches %d nodes; use the full ref", ref, len(hits))
}

// Look returns the node at ref and everything under it. An empty ref is the
// caller's own task (or PR) when a session asks, else the whole app.
func (a *App) Look(ref, sessionID string) (*Node, error) {
	root, err := a.Tree()
	if err != nil {
		return nil, err
	}
	if ref == "" && sessionID != "" {
		if se, err := a.Store.Session(sessionID); err == nil {
			ref = "task:" + se.TaskID
			if se.SliceID != "" {
				ref = "slice:" + se.SliceID
			}
		}
	}
	return root.Find(ref)
}

// Act runs one verb on the node at ref, after checking its arguments.
func (a *App) Act(ctx context.Context, ref, verb string, in Args) (any, error) {
	root, err := a.Tree()
	if err != nil {
		return nil, err
	}
	n, err := root.Find(ref)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(n.Verbs, func(v *Verb) bool { return v.Name == verb })
	if i < 0 {
		names := make([]string, len(n.Verbs))
		for j, v := range n.Verbs {
			names[j] = v.Name
		}
		return nil, bad(fmt.Sprintf("%s has no verb %q now; it has: %s", n.Ref, verb, strings.Join(names, ", ")))
	}
	v := n.Verbs[i]
	if in == nil {
		in = Args{}
	}
	for _, arg := range v.Args {
		val := in.str(arg.Name)
		if val == "" && !arg.Optional {
			return nil, bad(fmt.Sprintf("%s needs %s", v.Name, arg.Name))
		}
		if val != "" && len(arg.Enum) > 0 && !slices.Contains(arg.Enum, val) {
			return nil, bad(fmt.Sprintf("%s must be one of %s", arg.Name, strings.Join(arg.Enum, ", ")))
		}
	}
	return v.Run(ctx, n, in)
}

// Text renders a node and its subtree one line per node, the way look shows it to agents.
func (n *Node) Text() string {
	var b strings.Builder
	var line func(n *Node, depth int)
	line = func(n *Node, depth int) {
		b.WriteString(strings.Repeat("  ", depth) + n.Ref + "  " + n.Title)
		for _, f := range n.Fields {
			b.WriteString("  " + f.Key + "=" + f.Value)
		}
		if len(n.Verbs) > 0 {
			sigs := make([]string, len(n.Verbs))
			for i, v := range n.Verbs {
				sigs[i] = v.signature()
			}
			b.WriteString("  [" + strings.Join(sigs, ", ") + "]")
		}
		b.WriteByte('\n')
		for _, c := range n.Children {
			line(c, depth+1)
		}
	}
	line(n, 0)
	return b.String()
}

func (v *Verb) signature() string {
	if len(v.Args) == 0 {
		return v.Name
	}
	parts := make([]string, len(v.Args))
	for i, arg := range v.Args {
		p := arg.Name
		if len(arg.Enum) > 0 {
			p += "=" + strings.Join(arg.Enum, "|")
		}
		if arg.Optional {
			p += "?"
		}
		parts[i] = p
	}
	return v.Name + "(" + strings.Join(parts, ", ") + ")"
}

// Describe turns a verb's result into the one line an agent reads back.
func Describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "ok"
	case string:
		return x
	case store.Session:
		return fmt.Sprintf("started session:%s %q (%s)", short(x.ID), x.Title, x.Status)
	case store.Slice:
		return fmt.Sprintf("slice %s %q pr=#%d", x.Key, x.Title, x.PRNumber)
	case []store.Slice:
		keys := make([]string, len(x))
		for i, s := range x {
			keys[i] = s.Key
		}
		return "slices: " + strings.Join(keys, ", ")
	case store.Task:
		return fmt.Sprintf("created task:%s %q", x.Slug, x.Title)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
