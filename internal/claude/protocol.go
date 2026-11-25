package claude

import (
	"encoding/json"
	"strings"
)

// Message is one stdout line other than the control protocol.
type Message struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	SessionID       string          `json:"session_id"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	Message         *APIMessage     `json:"message"`
	Event           *StreamEvent    `json:"event"`
	IsError         bool            `json:"is_error"`
	Result          string          `json:"result"`
	DurationMS      int64           `json:"duration_ms"`
	TotalCostUSD    float64         `json:"total_cost_usd"`
	Model           string          `json:"model"`
	SlashCommands   []string        `json:"slash_commands"`
	Skills          json.RawMessage `json:"skills"`
	Raw             json.RawMessage `json:"-"`
}

// Commands lists the slash commands and skills a system/init message announces, without the leading "/".
func (m Message) Commands() []string {
	seen := map[string]bool{}
	var out []string
	add := func(name string) {
		name = strings.TrimPrefix(name, "/")
		if name != "" && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	for _, c := range m.SlashCommands {
		add(c)
	}
	var skills []json.RawMessage
	json.Unmarshal(m.Skills, &skills)
	for _, raw := range skills {
		var named struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(raw, &named.Name) != nil {
			json.Unmarshal(raw, &named)
		}
		add(named.Name)
	}
	return out
}

// APIMessage is the Anthropic message inside assistant and user lines.
type APIMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Usage   *Usage          `json:"usage"`
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	OutputTokens             int `json:"output_tokens"`
}

// Context is how many tokens of the context window the request used.
func (u Usage) Context() int {
	return u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens + u.OutputTokens
}

// StreamEvent is a raw Anthropic streaming event, sent with --include-partial-messages.
type StreamEvent struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"delta"`
}

type Block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// Blocks decodes the content array; a plain string becomes one text block.
func (m *APIMessage) Blocks() []Block {
	if m == nil || len(m.Content) == 0 {
		return nil
	}
	var blocks []Block
	if json.Unmarshal(m.Content, &blocks) == nil {
		return blocks
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []Block{{Type: "text", Text: s}}
	}
	return nil
}

// ResultText flattens a tool_result's content, a string or blocks, to text.
func (b Block) ResultText() string {
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		return s
	}
	var parts []Block
	if json.Unmarshal(b.Content, &parts) != nil {
		return ""
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p.Type == "text" {
			out = append(out, p.Text)
		} else {
			out = append(out, "["+p.Type+"]")
		}
	}
	return strings.Join(out, "\n")
}

// PermissionRequest is a can_use_tool control request.
type PermissionRequest struct {
	RequestID   string            `json:"-"`
	ToolName    string            `json:"tool_name"`
	Input       json.RawMessage   `json:"input"`
	ToolUseID   string            `json:"tool_use_id"`
	Suggestions []json.RawMessage `json:"permission_suggestions"`
}

// PermissionDecision answers a PermissionRequest. Allowed calls run with the
// request's input; UpdatedPermissions are saved by the CLI, as the terminal's
// "don't ask again" does.
type PermissionDecision struct {
	Allow              bool
	UpdatedPermissions []json.RawMessage
	Message            string
}

// HookMatcher registers a hook callback for event on tools matching Matcher.
type HookMatcher struct {
	Event   string
	Matcher string
	ID      string
}

// HookCall is a hook_callback control request.
type HookCall struct {
	CallbackID string
	ToolUseID  string
	Input      HookInput
}

// HookInput is the hook's input as Claude Code passes it to command hooks.
type HookInput struct {
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	Cwd           string          `json:"cwd"`
}

// HookResult is what a hook returns: Deny blocks a PreToolUse call with that
// reason, AdditionalContext is shown to the model.
type HookResult struct {
	Deny              string
	AdditionalContext string
}

func (r HookResult) output(event string) map[string]any {
	specific := map[string]any{"hookEventName": event}
	if r.Deny != "" {
		specific["permissionDecision"] = "deny"
		specific["permissionDecisionReason"] = r.Deny
	}
	if r.AdditionalContext != "" {
		specific["additionalContext"] = r.AdditionalContext
	}
	if len(specific) == 1 {
		return map[string]any{}
	}
	return map[string]any{"hookSpecificOutput": specific}
}

func (d PermissionDecision) response(req PermissionRequest) map[string]any {
	if !d.Allow {
		msg := d.Message
		if msg == "" {
			msg = "The user denied this action."
		}
		return map[string]any{"behavior": "deny", "message": msg, "toolUseID": req.ToolUseID}
	}
	body := map[string]any{"behavior": "allow", "updatedInput": orEmpty(req.Input), "toolUseID": req.ToolUseID}
	if len(d.UpdatedPermissions) > 0 {
		body["updatedPermissions"] = d.UpdatedPermissions
	}
	return body
}

func hooksPayload(hooks []HookMatcher) map[string][]map[string]any {
	out := map[string][]map[string]any{}
	for _, h := range hooks {
		out[h.Event] = append(out[h.Event], map[string]any{"matcher": h.Matcher, "hookCallbackIds": []string{h.ID}})
	}
	return out
}

func orEmpty(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}
