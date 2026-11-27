// Package tree is the node tree: every user-visible thing is a node with an address,
// read with Get and changed with actions applied through Do.
package tree

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
)

// ErrNotFound is returned when an address names no node.
var ErrNotFound = errors.New("not found")

// Addr is a node address such as "app", "project:api-gateway" or "task:eng-482/pr2".
type Addr string

// Kind returns the part before the colon.
func (a Addr) Kind() string {
	kind, _, _ := strings.Cut(string(a), ":")
	return kind
}

// Path returns the part after the colon.
func (a Addr) Path() string {
	_, path, _ := strings.Cut(string(a), ":")
	return path
}

// Node is the one read shape of the tree.
type Node struct {
	Addr     Addr           `json:"addr"`
	Kind     string         `json:"kind"`
	Title    string         `json:"title"`
	Fields   map[string]any `json:"fields,omitempty"`
	Children []Summary      `json:"children"`
	Actions  []Action       `json:"actions"`
}

// Summary is a child entry of a node.
type Summary struct {
	Addr     Addr      `json:"addr"`
	Kind     string    `json:"kind"`
	Title    string    `json:"title"`
	Status   string    `json:"status,omitempty"`
	Children []Summary `json:"children,omitempty"`
}

// Kind declares how nodes of one kind are read and which actions they offer.
type Kind struct {
	Name     string
	Get      func(ctx context.Context, addr Addr) (Node, error)
	Children func(ctx context.Context, addr Addr) ([]Summary, error)
	Actions  []Action
}

// Action is a change a node offers. Args is a JSON Schema; When reports whether it applies now.
type Action struct {
	Name string                                                                            `json:"name"`
	Help string                                                                            `json:"help"`
	Args json.RawMessage                                                                   `json:"args,omitempty"`
	When func(n Node) bool                                                                 `json:"-"`
	Run  func(ctx context.Context, n Node, args json.RawMessage, by Actor) (Result, error) `json:"-"`
}

// Op asks for one action on one node.
type Op struct {
	Target Addr            `json:"target"`
	Action string          `json:"action"`
	Args   json.RawMessage `json:"args,omitempty"`
}

// Result is the outcome of one Op.
type Result struct {
	OK      bool   `json:"ok"`
	Addr    Addr   `json:"addr"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}

// Actor is who triggered an action: user, cli, mcp, session:<id8> or plugin:<name>.
type Actor struct{ Kind, ID string }

// String formats the actor as kind or kind:id.
func (a Actor) String() string {
	if a.ID == "" {
		return a.Kind
	}
	return a.Kind + ":" + a.ID
}

// ParseActor parses the String form.
func ParseActor(s string) Actor {
	kind, id, _ := strings.Cut(s, ":")
	return Actor{Kind: kind, ID: id}
}

// Tree holds the registered kinds.
type Tree struct {
	db    *sql.DB
	kinds map[string]*Kind

	// OnChange receives the addresses of nodes that changed.
	OnChange func(addrs []Addr)
	// Here resolves the "here" address for an actor.
	Here func(ctx context.Context, by Actor) (Addr, error)
}

// New returns an empty tree that records actions in db.
func New(db *sql.DB) *Tree {
	return &Tree{db: db, kinds: map[string]*Kind{}}
}

// Register adds a kind.
func (t *Tree) Register(k Kind) {
	if _, ok := t.kinds[k.Name]; ok {
		panic("tree: kind registered twice: " + k.Name)
	}
	t.kinds[k.Name] = &k
}

// Extend appends actions to a registered kind.
func (t *Tree) Extend(kind string, actions ...Action) {
	k := t.kind(kind)
	k.Actions = append(k.Actions, actions...)
}

// AddChildren appends a source of children to a registered kind.
func (t *Tree) AddChildren(kind string, children func(ctx context.Context, addr Addr) ([]Summary, error)) {
	k := t.kind(kind)
	prev := k.Children
	if prev == nil {
		k.Children = children
		return
	}
	k.Children = func(ctx context.Context, addr Addr) ([]Summary, error) {
		first, err := prev(ctx, addr)
		if err != nil {
			return nil, err
		}
		rest, err := children(ctx, addr)
		return append(first, rest...), err
	}
}

// AddFields merges the fields returned by fields into every node of a registered kind.
func (t *Tree) AddFields(kind string, fields func(ctx context.Context, n Node) (map[string]any, error)) {
	k := t.kind(kind)
	get := k.Get
	k.Get = func(ctx context.Context, addr Addr) (Node, error) {
		n, err := get(ctx, addr)
		if err != nil {
			return n, err
		}
		n.Addr = addr
		extra, err := fields(ctx, n)
		if err != nil {
			return Node{}, err
		}
		if n.Fields == nil {
			n.Fields = map[string]any{}
		}
		maps.Copy(n.Fields, extra)
		return n, nil
	}
}

// Kinds returns the registered kind names.
func (t *Tree) Kinds() []string {
	names := make([]string, 0, len(t.kinds))
	for name := range t.kinds {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Actions returns every action declared on a registered kind, whether or not it is valid now.
func (t *Tree) Actions(kind string) []Action {
	return slices.Clone(t.kind(kind).Actions)
}

func (t *Tree) kind(name string) *Kind {
	k, ok := t.kinds[name]
	if !ok {
		panic("tree: unknown kind: " + name)
	}
	return k
}

// Changed reports that the nodes at addrs changed.
func (t *Tree) Changed(addrs ...Addr) {
	if t.OnChange == nil {
		return
	}
	slices.Sort(addrs)
	t.OnChange(slices.Compact(addrs))
}

// Resolve maps "here" to the actor's own node and returns other addresses unchanged.
func (t *Tree) Resolve(ctx context.Context, by Actor, addr Addr) (Addr, error) {
	if addr != "here" {
		return addr, nil
	}
	if t.Here == nil {
		return "", errors.New("here is only defined inside a layer8 session")
	}
	return t.Here(ctx, by)
}

// Get reads a node with depth levels of child summaries and the actions valid now.
func (t *Tree) Get(ctx context.Context, by Actor, addr Addr, depth int) (Node, error) {
	addr, err := t.Resolve(ctx, by, addr)
	if err != nil {
		return Node{}, err
	}
	k, ok := t.kinds[addr.Kind()]
	if !ok {
		return Node{}, fmt.Errorf("%w: %s", ErrNotFound, addr)
	}
	n, err := k.Get(ctx, addr)
	if err != nil {
		return Node{}, err
	}
	n.Addr, n.Kind = addr, k.Name
	if n.Children, err = t.children(ctx, addr, depth); err != nil {
		return Node{}, err
	}
	if n.Children == nil {
		n.Children = []Summary{}
	}
	n.Actions = []Action{}
	for _, a := range k.Actions {
		if a.When == nil || a.When(n) {
			n.Actions = append(n.Actions, a)
		}
	}
	return n, nil
}

func (t *Tree) children(ctx context.Context, addr Addr, depth int) ([]Summary, error) {
	k := t.kinds[addr.Kind()]
	if depth <= 0 || k == nil || k.Children == nil {
		return nil, nil
	}
	list, err := k.Children(ctx, addr)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].Kind == "" {
			list[i].Kind = list[i].Addr.Kind()
		}
		if list[i].Children, err = t.children(ctx, list[i].Addr, depth-1); err != nil {
			return nil, err
		}
	}
	return list, nil
}

// Do applies each op independently, records it in actions_log, and returns one Result per op.
func (t *Tree) Do(ctx context.Context, by Actor, ops []Op) []Result {
	results := make([]Result, len(ops))
	for i, op := range ops {
		res, err := t.do(ctx, by, op)
		if err != nil {
			res = Result{Addr: op.Target, Message: err.Error()}
		}
		t.log(by, op, res)
		results[i] = res
	}
	return results
}

func (t *Tree) do(ctx context.Context, by Actor, op Op) (Result, error) {
	n, err := t.Get(ctx, by, op.Target, 1)
	if err != nil {
		return Result{}, err
	}
	i := slices.IndexFunc(n.Actions, func(a Action) bool { return a.Name == op.Action })
	if i < 0 {
		if slices.ContainsFunc(t.kinds[n.Kind].Actions, func(a Action) bool { return a.Name == op.Action }) {
			return Result{}, fmt.Errorf("%s: action %q is not available right now", n.Addr, op.Action)
		}
		return Result{}, fmt.Errorf("%s has no action %q", n.Addr, op.Action)
	}
	a := n.Actions[i]
	args := op.Args
	if len(args) == 0 || string(args) == "null" {
		args = json.RawMessage(`{}`)
	}
	if err := validate(a.Args, args); err != nil {
		return Result{}, fmt.Errorf("%s %s: %w", n.Addr, a.Name, err)
	}
	res, err := a.Run(ctx, n, args, by)
	if err != nil {
		return Result{}, err
	}
	res.OK = true
	if res.Addr == "" {
		res.Addr = n.Addr
	}
	t.Changed(n.Addr, res.Addr)
	return res, nil
}

func (t *Tree) log(by Actor, op Op, res Result) {
	args := string(op.Args)
	if args == "" {
		args = "{}"
	}
	_, err := t.db.Exec(`INSERT INTO actions_log (actor_kind, actor_id, target, action, args, ok, message) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		by.Kind, by.ID, op.Target, op.Action, args, res.OK, res.Message)
	if err != nil {
		slog.Warn("actions_log insert failed", "err", err)
	}
}
