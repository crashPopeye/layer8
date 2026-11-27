package core

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// sessionHeader tells the MCP server which session is calling, so look() starts at its task.
const sessionHeader = "X-Layer8-Session"

const lookHelp = `See layer8 the way the user does: a tree of nodes (app, projects, tasks, PR slices, sessions), one per line as
  ref  title  key=value…  [verbs]
With no ref you see your own task. Pass a ref (e.g. "app", "task:eng-123", "slice:eng-123/pr1") to look elsewhere.
Call a listed verb with act. Arguments are shown as name=choice|choice; "?" marks optional ones.`

const actHelp = `Run one verb on a node from look, e.g. act("slice:eng-123/pr1", "fix-ci") or
act("task:eng-123", "show", {"panel": "changes"}) to put something in front of the user.`

var mcpTools = []map[string]any{
	{"name": "look", "description": lookHelp, "annotations": map[string]any{"readOnlyHint": true},
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{
			"ref": map[string]any{"type": "string", "description": "node to look at; empty = your task"}}}},
	{"name": "act", "description": actHelp,
		"inputSchema": map[string]any{"type": "object", "required": []string{"ref", "verb"}, "properties": map[string]any{
			"ref":  map[string]any{"type": "string"},
			"verb": map[string]any{"type": "string"},
			"args": map[string]any{"type": "object", "description": "the verb's arguments by name"}}}},
}

type rpcRequest struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// serveMCP is a stateless MCP server over Streamable HTTP that answers with plain
// JSON: initialize, tools/list and tools/call for look and act.
func (a *App) serveMCP(w http.ResponseWriter, r *http.Request) {
	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
		return
	}
	if req.ID == nil {
		w.WriteHeader(http.StatusAccepted) // a notification
		return
	}
	result, rpcErr := a.mcpCall(r.Context(), r.Header.Get(sessionHeader), req)
	out := map[string]any{"jsonrpc": "2.0", "id": req.ID}
	if rpcErr != nil {
		out["error"] = rpcErr
	} else {
		out["result"] = result
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

func (a *App) mcpCall(ctx context.Context, sessionID string, req rpcRequest) (any, map[string]any) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "layer8", "version": a.Version},
			"instructions":    "layer8 is the app running you. Use look to see tasks, PRs and sessions, and act to drive them or show the user something.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools}, nil
	case "tools/call":
		var p struct {
			Name      string `json:"name"`
			Arguments struct {
				Ref  string `json:"ref"`
				Verb string `json:"verb"`
				Args Args   `json:"args"`
			} `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return nil, map[string]any{"code": -32602, "message": err.Error()}
		}
		ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		var text string
		var err error
		switch p.Name {
		case "look":
			var n *Node
			if n, err = a.Look(p.Arguments.Ref, sessionID); err == nil {
				text = n.Text()
			}
		case "act":
			var v any
			if v, err = a.Act(ctx, p.Arguments.Ref, p.Arguments.Verb, p.Arguments.Args); err == nil {
				text = Describe(v)
			}
		default:
			return nil, map[string]any{"code": -32602, "message": "unknown tool " + p.Name}
		}
		if err != nil {
			return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": err.Error()}}}, nil
		}
		return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}, nil
	}
	return nil, map[string]any{"code": -32601, "message": "method not found: " + req.Method}
}
