package linear

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/crashPopeye/layer8/internal/tree"
)

type kind struct {
	c        *Client
	tree     *tree.Tree
	redirect string
}

// Register adds the linear kind to t and lists it under app. It returns the OAuth callback handler for redirect.
func Register(t *tree.Tree, c *Client, redirect string) http.Handler {
	k := kind{c: c, tree: t, redirect: redirect}
	root := func(n tree.Node) bool { return n.Addr.Path() == "" }
	connected := func(n tree.Node) bool { return root(n) && n.Fields["connected"] == true }
	t.Register(tree.Kind{
		Name:     "linear",
		Get:      k.get,
		Children: k.children,
		Actions: []tree.Action{
			{Name: "connect", Help: "Connect with a Linear personal API key (Linear → Settings → Security & access → Personal API keys)",
				Args: json.RawMessage(`{"type":"object","required":["apiKey"],"properties":{"apiKey":{"type":"string"}}}`),
				When: func(n tree.Node) bool { return root(n) && !connected(n) },
				Run:  k.connect},
			{Name: "connect-oauth", Help: "Sign in with Linear in the browser: returns the URL to open",
				When: func(n tree.Node) bool { return root(n) && !connected(n) && c.ClientID != "" },
				Run:  k.connectOAuth},
			{Name: "disconnect", Help: "Forget the stored Linear credentials", When: connected, Run: k.disconnect},
			{Name: "search", Help: "Search Linear issues; results are in data",
				Args: json.RawMessage(`{"type":"object","required":["query"],"properties":{"query":{"type":"string"}}}`),
				When: connected, Run: k.search},
		},
	})
	t.AddChildren("app", func(context.Context, tree.Addr) ([]tree.Summary, error) {
		return []tree.Summary{{Addr: "linear", Title: "Linear"}}, nil
	})
	return http.HandlerFunc(k.callback)
}

func (k kind) get(ctx context.Context, a tree.Addr) (tree.Node, error) {
	if a.Path() != "" {
		return k.issue(ctx, a.Path())
	}
	if !k.c.Connected() {
		return tree.Node{Title: "Linear", Fields: map[string]any{"connected": false}}, nil
	}
	fields := map[string]any{"connected": true}
	if v, err := k.c.Viewer(ctx); err != nil {
		fields["error"] = err.Error()
	} else {
		fields["viewer"] = v
	}
	return tree.Node{Title: "Linear", Fields: fields}, nil
}

func (k kind) issue(ctx context.Context, id string) (tree.Node, error) {
	i, err := k.c.Issue(ctx, id)
	if err != nil {
		return tree.Node{}, err
	}
	var fields map[string]any
	data, _ := json.Marshal(i)
	json.Unmarshal(data, &fields)
	fields["markdown"] = TicketMarkdown(i)
	return tree.Node{Title: i.Identifier + " " + i.Title, Fields: fields}, nil
}

// children lists the connected user's open issues; a Linear failure yields none so reads of app never fail.
func (k kind) children(ctx context.Context, a tree.Addr) ([]tree.Summary, error) {
	if a.Path() != "" || !k.c.Connected() {
		return nil, nil
	}
	issues, err := k.c.MyIssues(ctx)
	if err != nil {
		slog.Warn("linear: listing my issues", "err", err)
		return nil, nil
	}
	return summaries(issues), nil
}

func summaries(issues []IssueRef) []tree.Summary {
	list := make([]tree.Summary, len(issues))
	for i, is := range issues {
		list[i] = tree.Summary{Addr: tree.Addr("linear:" + is.Identifier), Title: is.Identifier + " " + is.Title, Status: is.State.Name}
	}
	return list
}

func (k kind) connect(ctx context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ APIKey string }
	json.Unmarshal(args, &a)
	u, err := k.c.Connect(ctx, a.APIKey)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("connected to Linear as %s (%s)", u.Name, u.Email)}, nil
}

func (k kind) connectOAuth(context.Context, tree.Node, json.RawMessage, tree.Actor) (tree.Result, error) {
	u, err := k.c.StartOAuth(k.redirect)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: "open this URL to sign in with Linear: " + u, Data: map[string]string{"url": u}}, nil
}

func (k kind) disconnect(context.Context, tree.Node, json.RawMessage, tree.Actor) (tree.Result, error) {
	return tree.Result{Message: "disconnected from Linear"}, k.c.Disconnect()
}

func (k kind) search(ctx context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ Query string }
	json.Unmarshal(args, &a)
	issues, err := k.c.Search(ctx, a.Query)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("%d issues match %q", len(issues), a.Query), Data: summaries(issues)}, nil
}

func (k kind) callback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		http.Error(w, "Linear sign-in failed: "+e+" "+q.Get("error_description"), http.StatusBadRequest)
		return
	}
	if err := k.c.FinishOAuth(r.Context(), q.Get("state"), q.Get("code"), k.redirect); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	k.tree.Changed("linear")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, "<!doctype html><title>layer8</title><p>Linear is connected. You can close this tab.</p>")
}
