// Package mcp exposes the node tree to agents over MCP streamable HTTP:
// nodes are resources and the single tool "do" applies actions.
package mcp

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"

	"github.com/crashPopeye/layer8/internal/server"
	"github.com/crashPopeye/layer8/internal/tree"
)

const protocolVersion = "2025-06-18"

const instructions = `layer8 is the user's agent desktop app. Everything in it is a node with an address.
Read layer8://here for the task this session works in, or layer8://app for projects.
Each node lists the actions valid on it right now, with their args schema. Apply them with the do tool.`

const doHelp = `Apply actions to layer8 nodes. Read the target node first (resources/read layer8://here, layer8://app, layer8://project/<name>, …): it lists the actions valid right now and their args schema. Each op is {target, action, args}, where target is an address such as "app", "here" or "project:api-gateway". Ops run independently and each returns its own result.`

var doSchema = json.RawMessage(`{"type":"object","required":["ops"],"properties":{"ops":{"type":"array","items":{"type":"object","required":["target","action"],"properties":{"target":{"type":"string","description":"Node address, e.g. app, here, project:api-gateway"},"action":{"type":"string"},"args":{"type":"object"}}}}}}`)

// Server answers MCP requests. Subscriptions are kept per Mcp-Session-Id.
type Server struct {
	tree    *tree.Tree
	hub     *server.Hub
	version string

	mu   sync.Mutex
	subs map[string]map[tree.Addr]string
}

// New returns an MCP handler over t that streams subscription updates from hub.
func New(t *tree.Tree, hub *server.Hub, version string) *Server {
	return &Server{tree: t, hub: hub, version: version, subs: map[string]map[tree.Addr]string{}}
}

// ServeHTTP handles POST (JSON-RPC), GET (notification stream) and DELETE (end session).
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.post(w, r)
	case http.MethodGet:
		s.stream(w, r)
	case http.MethodDelete:
		s.mu.Lock()
		delete(s.subs, r.Header.Get("Mcp-Session-Id"))
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

func invalidParams(err error) error { return &rpcError{Code: -32602, Message: err.Error()} }

func (s *Server) post(w http.ResponseWriter, r *http.Request) {
	var req request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		reply(w, json.RawMessage("null"), nil, &rpcError{Code: -32700, Message: err.Error()})
		return
	}
	if req.ID == nil || req.Method == "" {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if req.Method == "initialize" {
		w.Header().Set("Mcp-Session-Id", rand.Text())
	}
	result, err := s.call(r, req)
	reply(w, req.ID, result, err)
}

func reply(w http.ResponseWriter, id json.RawMessage, result any, err error) {
	resp := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		var re *rpcError
		if !errors.As(err, &re) {
			re = &rpcError{Code: -32603, Message: err.Error()}
		}
		resp["error"] = re
	} else {
		resp["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) call(r *http.Request, req request) (any, error) {
	ctx, by := r.Context(), actor(r)
	var p struct {
		URI       string          `json:"uri"`
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, invalidParams(err)
		}
	}
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"resources": map[string]bool{"subscribe": true}, "tools": map[string]any{}},
			"serverInfo":      map[string]string{"name": "layer8", "version": s.version},
			"instructions":    instructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "resources/list":
		return s.list(r, by)
	case "resources/templates/list":
		var templates []map[string]string
		for _, kind := range s.tree.Kinds() {
			if kind != "app" {
				templates = append(templates, map[string]string{"uriTemplate": "layer8://" + kind + "/{path}", "name": kind, "mimeType": "application/json"})
			}
		}
		return map[string]any{"resourceTemplates": templates}, nil
	case "resources/read":
		addr, err := addrOf(p.URI)
		if err != nil {
			return nil, invalidParams(err)
		}
		n, err := s.tree.Get(ctx, by, addr, 1)
		if errors.Is(err, tree.ErrNotFound) {
			return nil, &rpcError{Code: -32002, Message: err.Error()}
		}
		if err != nil {
			return nil, invalidParams(err)
		}
		data, _ := json.Marshal(n)
		return map[string]any{"contents": []map[string]string{
			{"uri": p.URI, "mimeType": "application/json", "text": string(data)},
			{"uri": p.URI, "mimeType": "text/plain", "text": tree.Render(n)},
		}}, nil
	case "resources/subscribe", "resources/unsubscribe":
		return map[string]any{}, s.subscribe(r, by, p.URI, req.Method == "resources/subscribe")
	case "tools/list":
		return map[string]any{"tools": []map[string]any{{"name": "do", "title": "Act on layer8", "description": doHelp, "inputSchema": doSchema}}}, nil
	case "tools/call":
		if p.Name != "do" {
			return nil, invalidParams(fmt.Errorf("unknown tool %q", p.Name))
		}
		var args struct {
			Ops []tree.Op `json:"ops"`
		}
		if err := json.Unmarshal(p.Arguments, &args); err != nil {
			return nil, invalidParams(err)
		}
		return toolResult(s.tree.Do(ctx, by, args.Ops)), nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + req.Method}
}

func (s *Server) list(r *http.Request, by tree.Actor) (any, error) {
	app, err := s.tree.Get(r.Context(), by, "app", 1)
	if err != nil {
		return nil, err
	}
	resources := []map[string]string{{"uri": "layer8://app", "name": "app", "title": app.Title, "mimeType": "application/json"}}
	if by.Kind == "session" {
		resources = append(resources, map[string]string{"uri": "layer8://here", "name": "here", "title": "This session's task", "mimeType": "application/json"})
	}
	for _, c := range app.Children {
		resources = append(resources, map[string]string{"uri": uriOf(c.Addr), "name": string(c.Addr), "title": c.Title, "mimeType": "application/json"})
	}
	return map[string]any{"resources": resources}, nil
}

func toolResult(results []tree.Result) map[string]any {
	failed := slices.ContainsFunc(results, func(r tree.Result) bool { return !r.OK })
	return map[string]any{
		"content":           []map[string]string{{"type": "text", "text": tree.RenderResults(results)}},
		"structuredContent": map[string]any{"results": results},
		"isError":           failed,
	}
}

func (s *Server) subscribe(r *http.Request, by tree.Actor, uri string, on bool) error {
	addr, err := addrOf(uri)
	if err == nil {
		addr, err = s.tree.Resolve(r.Context(), by, addr)
	}
	if err != nil {
		return invalidParams(err)
	}
	id := r.Header.Get("Mcp-Session-Id")
	if id == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs[id] == nil {
		s.subs[id] = map[tree.Addr]string{}
	}
	if on {
		s.subs[id][addr] = uri
	} else {
		delete(s.subs[id], addr)
	}
	return nil
}

// stream sends notifications/resources/updated for the session's subscriptions.
func (s *Server) stream(w http.ResponseWriter, r *http.Request) {
	id := r.Header.Get("Mcp-Session-Id")
	if id == "" {
		http.Error(w, "Mcp-Session-Id required", http.StatusBadRequest)
		return
	}
	events, cancel := s.hub.Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	rc := http.NewResponseController(w)
	rc.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			change, isNode := e.Data.(server.NodeChange)
			if e.Name != "node" || !isNode {
				continue
			}
			for _, addr := range change.Addrs {
				s.mu.Lock()
				uri, subscribed := s.subs[id][addr]
				s.mu.Unlock()
				if subscribed {
					data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/resources/updated", "params": map[string]string{"uri": uri}})
					fmt.Fprintf(w, "event: message\ndata: %s\n\n", data)
				}
			}
			rc.Flush()
		}
	}
}

// actor is session:<id8> when X-Layer8-Session is set, else mcp.
func actor(r *http.Request) tree.Actor {
	if id := r.Header.Get("X-Layer8-Session"); id != "" {
		return tree.Actor{Kind: "session", ID: id}
	}
	return tree.Actor{Kind: "mcp"}
}

// addrOf maps layer8://kind/path to kind:path.
func addrOf(uri string) (tree.Addr, error) {
	rest, ok := strings.CutPrefix(uri, "layer8://")
	if !ok || rest == "" {
		return "", fmt.Errorf("not a layer8 resource: %q", uri)
	}
	kind, path, _ := strings.Cut(rest, "/")
	if path == "" {
		return tree.Addr(kind), nil
	}
	return tree.Addr(kind + ":" + path), nil
}

// uriOf maps kind:path to layer8://kind/path.
func uriOf(a tree.Addr) string {
	if a.Path() == "" {
		return "layer8://" + a.Kind()
	}
	return "layer8://" + a.Kind() + "/" + a.Path()
}
