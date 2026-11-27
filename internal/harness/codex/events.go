package codex

import (
	"cmp"
	"encoding/json"
	"strings"
	"sync"

	"github.com/crashPopeye/layer8/internal/harness"
)

type tokenUsage struct {
	Total struct {
		TotalTokens int `json:"totalTokens"`
	} `json:"total"`
	Last struct {
		InputTokens  int `json:"inputTokens"`
		CachedTokens int `json:"cachedInputTokens"`
		OutputTokens int `json:"outputTokens"`
		TotalTokens  int `json:"totalTokens"`
	} `json:"last"`
	ContextWindow int `json:"modelContextWindow"`
}

type change struct {
	Path string `json:"path"`
	Kind struct {
		Type string `json:"type"`
	} `json:"kind"`
	Diff string `json:"diff"`
}

type item struct {
	Type             string          `json:"type"`
	ID               string          `json:"id"`
	ClientID         string          `json:"clientId"`
	Text             string          `json:"text"`
	Summary          []string        `json:"summary"`
	Content          json.RawMessage `json:"content"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	AggregatedOutput string          `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	Status           string          `json:"status"`
	Changes          []change        `json:"changes"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           json.RawMessage `json:"result"`
	Error            *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// toolCall is the shared tool_use form of a codex item.
type toolCall struct {
	name  string
	input any
}

// asTool returns the tool_use form of commandExecution, fileChange and mcpToolCall items.
func asTool(it item) (toolCall, bool) {
	switch it.Type {
	case "commandExecution":
		return toolCall{"Bash", map[string]any{"command": it.Command, "cwd": it.Cwd}}, true
	case "fileChange":
		name := "Write"
		var changes []map[string]any
		for _, c := range it.Changes {
			if c.Kind.Type != "add" {
				name = "Edit"
			}
			changes = append(changes, map[string]any{"path": c.Path, "kind": c.Kind.Type, "diff": c.Diff})
		}
		input := map[string]any{"changes": changes}
		if len(it.Changes) > 0 {
			input["file_path"] = it.Changes[0].Path
		}
		return toolCall{name, input}, true
	case "mcpToolCall":
		return toolCall{"mcp__" + it.Server + "__" + it.Tool, it.Arguments}, true
	}
	return toolCall{}, false
}

// toolResult returns the tool_result content of a completed tool item.
func toolResult(it item) (content any, isError bool) {
	switch it.Type {
	case "commandExecution":
		return it.AggregatedOutput, it.Status != "completed" || it.ExitCode != nil && *it.ExitCode != 0
	case "mcpToolCall":
		if it.Error != nil {
			return it.Error.Message, true
		}
		var r struct {
			Content json.RawMessage `json:"content"`
		}
		json.Unmarshal(it.Result, &r)
		return r.Content, it.Status == "failed"
	}
	return it.Status, it.Status != "completed"
}

func (p *proc) notify(method string, raw json.RawMessage) {
	var n struct {
		Delta     string     `json:"delta"`
		Item      item       `json:"item"`
		Turn      turn       `json:"turn"`
		Usage     tokenUsage `json:"tokenUsage"`
		WillRetry bool       `json:"willRetry"`
		Error     struct {
			Message           string `json:"message"`
			AdditionalDetails string `json:"additionalDetails"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &n) != nil {
		return
	}
	switch method {
	case "item/agentMessage/delta":
		p.emit(harness.EventText, true, map[string]any{"text": n.Delta})
	case "item/reasoning/summaryTextDelta", "item/reasoning/textDelta":
		p.emit(harness.EventThinking, true, map[string]any{"text": n.Delta})
	case "item/started":
		if n.Item.Type == "userMessage" && n.Item.ClientID != "" {
			p.emit(harness.EventDelivered, false, map[string]any{"id": n.Item.ClientID})
		}
		if call, ok := asTool(n.Item); ok {
			p.tools[n.Item.ID] = call
			p.emit(harness.EventToolUse, false, map[string]any{"id": n.Item.ID, "name": call.name, "input": call.input})
		}
	case "item/completed":
		p.itemCompleted(n.Item)
	case "turn/started":
		p.tmu.Lock()
		p.turn = n.Turn.ID
		p.tmu.Unlock()
	case "turn/completed":
		p.turnCompleted(n.Turn)
	case "thread/tokenUsage/updated":
		p.usage = n.Usage
	case "error":
		if !n.WillRetry {
			p.emit(harness.EventError, false, map[string]any{"message": n.Error.Message, "details": n.Error.AdditionalDetails})
		}
	case "mcpServer/startupStatus/updated":
		var s map[string]any
		json.Unmarshal(raw, &s)
		s["subtype"] = "mcp_status"
		p.emit(harness.EventSystem, true, s)
	}
}

func (p *proc) itemCompleted(it item) {
	switch it.Type {
	case "agentMessage":
		p.text = it.Text
		p.emit(harness.EventText, false, map[string]any{"text": it.Text})
	case "reasoning":
		parts := it.Summary
		if len(parts) == 0 {
			json.Unmarshal(it.Content, &parts)
		}
		if text := strings.Join(parts, "\n\n"); text != "" {
			p.emit(harness.EventThinking, false, map[string]any{"text": text})
		}
	default:
		if _, ok := asTool(it); !ok {
			return
		}
		delete(p.tools, it.ID)
		content, isError := toolResult(it)
		p.emit(harness.EventToolResult, false, map[string]any{"toolUseId": it.ID, "content": content, "isError": isError})
	}
}

type turn struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	DurationMS int    `json:"durationMs"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *proc) turnCompleted(t turn) {
	subtype, result := t.Status, p.text
	if t.Status == "completed" {
		subtype = "success"
	}
	if t.Error != nil {
		result = t.Error.Message
	}
	p.text = ""
	u := p.usage
	p.endTurn(map[string]any{"subtype": subtype, "isError": t.Status != "completed", "result": result, "sessionId": p.thread,
		"costUsd": 0.0, "durationMs": t.DurationMS, "usage": u, "contextTokens": u.Last.TotalTokens, "contextWindow": u.ContextWindow})
}

// serverRequest turns codex approval requests into permission requests and refuses
// the requests layer8 does not handle.
func (p *proc) serverRequest(id json.RawMessage, method string, raw json.RawMessage) {
	var r struct {
		ItemID     string `json:"itemId"`
		Command    string `json:"command"`
		Cwd        string `json:"cwd"`
		Reason     string `json:"reason"`
		ServerName string `json:"serverName"`
		Message    string `json:"message"`
	}
	json.Unmarshal(raw, &r)
	req := harness.PermissionRequest{ID: string(id), ToolUseID: r.ItemID, Description: r.Reason}
	answer := approvalAnswer
	switch method {
	case "item/commandExecution/requestApproval":
		req.Tool = "Bash"
		req.Input, _ = json.Marshal(map[string]any{"command": cmp.Or(r.Command, p.tools[r.ItemID].command()), "cwd": r.Cwd})
	case "item/fileChange/requestApproval":
		call := p.tools[r.ItemID]
		req.Tool = cmp.Or(call.name, "Edit")
		req.Input, _ = json.Marshal(call.input)
	case "mcpServer/elicitation/request":
		req.Tool, req.Input, req.Description = "mcp__"+r.ServerName, raw, r.Message
		answer = elicitationAnswer
	default:
		p.reply(id, nil, &rpcError{Code: -32601, Message: "layer8 does not handle " + method})
		return
	}
	var once sync.Once
	p.emu.Lock()
	defer p.emu.Unlock()
	p.sink.OnPermission(req, func(d harness.Decision) {
		once.Do(func() { p.reply(id, answer(d), nil) })
	})
}

func (c toolCall) command() string {
	in, _ := c.input.(map[string]any)
	s, _ := in["command"].(string)
	return s
}

func approvalAnswer(d harness.Decision) any {
	switch {
	case !d.Allow:
		return map[string]string{"decision": "decline"}
	case d.Always:
		return map[string]string{"decision": "acceptForSession"}
	}
	return map[string]string{"decision": "accept"}
}

func elicitationAnswer(d harness.Decision) any {
	if !d.Allow {
		return map[string]any{"action": "decline", "content": nil}
	}
	return map[string]any{"action": "accept", "content": map[string]any{}}
}
