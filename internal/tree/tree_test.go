package tree_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tree"
)

type lamps map[string]bool

func newTree(t *testing.T) (*tree.Tree, lamps, *[][]tree.Addr, *sql.DB) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	tr := tree.New(db)
	state := lamps{"desk": false, "hall": true}
	var changes [][]tree.Addr
	tr.OnChange = func(addrs []tree.Addr) { changes = append(changes, addrs) }

	tr.Register(tree.Kind{
		Name: "room",
		Get:  func(ctx context.Context, a tree.Addr) (tree.Node, error) { return tree.Node{Title: "Room"}, nil },
		Children: func(ctx context.Context, a tree.Addr) ([]tree.Summary, error) {
			return []tree.Summary{{Addr: "lamp:desk", Title: "desk"}, {Addr: "lamp:hall", Title: "hall"}}, nil
		},
	})
	isOn := func(n tree.Node) bool { return n.Fields["on"] == true }
	tr.Register(tree.Kind{
		Name: "lamp",
		Get: func(ctx context.Context, a tree.Addr) (tree.Node, error) {
			on, ok := state[a.Path()]
			if !ok {
				return tree.Node{}, tree.ErrNotFound
			}
			return tree.Node{Title: a.Path(), Fields: map[string]any{"on": on}}, nil
		},
		Actions: []tree.Action{
			{Name: "switch-on", Help: "Turn it on", When: func(n tree.Node) bool { return !isOn(n) },
				Args: json.RawMessage(`{"type":"object","properties":{"level":{"type":"integer"},"color":{"enum":["warm","cold"]}},"required":["level"]}`),
				Run: func(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
					state[n.Addr.Path()] = true
					return tree.Result{Message: "on by " + by.String()}, nil
				}},
			{Name: "switch-off", Help: "Turn it off", When: isOn,
				Run: func(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
					return tree.Result{}, errors.New("stuck")
				}},
		},
	})
	return tr, state, &changes, db
}

func TestGetFiltersActionsAndExpandsChildren(t *testing.T) {
	tr, _, _, _ := newTree(t)
	ctx := context.Background()
	user := tree.Actor{Kind: "user"}

	desk, err := tr.Get(ctx, user, "lamp:desk", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(desk.Actions) != 1 || desk.Actions[0].Name != "switch-on" || desk.Kind != "lamp" || desk.Addr != "lamp:desk" {
		t.Fatalf("desk = %+v", desk)
	}

	room, _ := tr.Get(ctx, user, "room", 0)
	if len(room.Children) != 0 {
		t.Fatalf("depth 0 children = %v", room.Children)
	}
	room, _ = tr.Get(ctx, user, "room", 1)
	if len(room.Children) != 2 || room.Children[0].Kind != "lamp" {
		t.Fatalf("depth 1 children = %+v", room.Children)
	}

	if _, err := tr.Get(ctx, user, "lamp:attic", 1); !errors.Is(err, tree.ErrNotFound) {
		t.Fatalf("missing lamp err = %v", err)
	}
	if _, err := tr.Get(ctx, user, "ghost:x", 1); !errors.Is(err, tree.ErrNotFound) {
		t.Fatalf("unknown kind err = %v", err)
	}
}

func TestDoValidatesRunsAndRecords(t *testing.T) {
	tr, state, changes, db := newTree(t)
	ctx := context.Background()
	by := tree.Actor{Kind: "session", ID: "9c41e0aa"}

	cases := []struct {
		op   tree.Op
		ok   bool
		want string
	}{
		{tree.Op{Target: "lamp:desk", Action: "switch-on"}, false, "args.level is required"},
		{tree.Op{Target: "lamp:desk", Action: "switch-on", Args: json.RawMessage(`{"level":"high"}`)}, false, "args.level must be of type integer"},
		{tree.Op{Target: "lamp:desk", Action: "switch-on", Args: json.RawMessage(`{"level":1.5}`)}, false, "must be of type integer"},
		{tree.Op{Target: "lamp:desk", Action: "switch-on", Args: json.RawMessage(`{"level":1,"color":"red"}`)}, false, "must be one of"},
		{tree.Op{Target: "lamp:desk", Action: "switch-on", Args: json.RawMessage(`{"level":1,"x":1}`)}, false, "args.x is not a known argument"},
		{tree.Op{Target: "lamp:desk", Action: "explode"}, false, `has no action "explode"`},
		{tree.Op{Target: "lamp:hall", Action: "switch-on", Args: json.RawMessage(`{"level":1}`)}, false, "not available right now"},
		{tree.Op{Target: "lamp:attic", Action: "switch-on"}, false, "not found"},
		{tree.Op{Target: "lamp:hall", Action: "switch-off"}, false, "stuck"},
		{tree.Op{Target: "lamp:desk", Action: "switch-on", Args: json.RawMessage(`{"level":3,"color":"warm"}`)}, true, "on by session:9c41e0aa"},
	}
	ops := make([]tree.Op, len(cases))
	for i, c := range cases {
		ops[i] = c.op
	}
	results := tr.Do(ctx, by, ops)
	for i, c := range cases {
		r := results[i]
		if r.OK != c.ok || !strings.Contains(r.Message, c.want) {
			t.Errorf("op %d: got %+v, want ok=%v message~%q", i, r, c.ok, c.want)
		}
	}
	if !state["desk"] {
		t.Fatal("desk lamp not switched on")
	}
	if last := results[len(results)-1]; last.Addr != "lamp:desk" {
		t.Fatalf("result addr = %q", last.Addr)
	}
	if len(*changes) != 1 || (*changes)[0][0] != "lamp:desk" || len((*changes)[0]) != 1 {
		t.Fatalf("changes = %v", *changes)
	}

	var n, okCount int
	var actor string
	db.QueryRow(`SELECT count(*), sum(ok), max(actor_kind || ':' || actor_id) FROM actions_log`).Scan(&n, &okCount, &actor)
	if n != len(cases) || okCount != 1 || actor != "session:9c41e0aa" {
		t.Fatalf("actions_log: n=%d ok=%d actor=%q", n, okCount, actor)
	}
}

func TestHere(t *testing.T) {
	tr, _, _, _ := newTree(t)
	ctx := context.Background()
	if _, err := tr.Get(ctx, tree.Actor{Kind: "user"}, "here", 0); err == nil {
		t.Fatal("here without hook should fail")
	}
	tr.Here = func(ctx context.Context, by tree.Actor) (tree.Addr, error) { return tree.Addr("lamp:" + by.ID), nil }
	n, err := tr.Get(ctx, tree.Actor{Kind: "session", ID: "hall"}, "here", 0)
	if err != nil || n.Addr != "lamp:hall" {
		t.Fatalf("here = %+v, %v", n, err)
	}
}

func TestExtendAndAddChildren(t *testing.T) {
	tr, _, _, _ := newTree(t)
	ctx := context.Background()
	tr.Extend("room", tree.Action{Name: "paint", Help: "Paint it"})
	tr.AddChildren("room", func(ctx context.Context, a tree.Addr) ([]tree.Summary, error) {
		return []tree.Summary{{Addr: "lamp:porch", Title: "porch", Status: "broken"}}, nil
	})
	room, err := tr.Get(ctx, tree.Actor{Kind: "user"}, "room", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(room.Children) != 3 || len(room.Actions) != 1 {
		t.Fatalf("room = %+v", room)
	}
	text := tree.Render(room)
	for _, want := range []string{"room  Room", "  lamp:porch  porch  [broken]", "  paint — Paint it"} {
		if !strings.Contains(text, want) {
			t.Errorf("render missing %q:\n%s", want, text)
		}
	}
	desk, _ := tr.Get(ctx, tree.Actor{Kind: "user"}, "lamp:desk", 1)
	if text := tree.Render(desk); !strings.Contains(text, "switch-on {level, color?} — Turn it on") || !strings.Contains(text, "  on: false") {
		t.Errorf("render:\n%s", text)
	}
}

func TestAddFields(t *testing.T) {
	tr, _, _, _ := newTree(t)
	tr.AddFields("lamp", func(ctx context.Context, n tree.Node) (map[string]any, error) {
		return map[string]any{"watts": len(n.Addr.Path())}, nil
	})
	desk, err := tr.Get(context.Background(), tree.Actor{Kind: "user"}, "lamp:desk", 0)
	if err != nil {
		t.Fatal(err)
	}
	if desk.Fields["watts"] != 4 || desk.Fields["on"] != false || len(desk.Actions) != 1 {
		t.Fatalf("desk = %+v", desk)
	}
}

func TestActorString(t *testing.T) {
	for _, s := range []string{"user", "cli", "session:9c41e0aa", "plugin:prs"} {
		if got := tree.ParseActor(s).String(); got != s {
			t.Errorf("ParseActor(%q).String() = %q", s, got)
		}
	}
}
