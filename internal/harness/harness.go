// Package harness defines how the daemon drives an agent CLI, whatever the vendor.
package harness

import (
	"context"
	"encoding/json"
	"time"
)

// Harness starts agent processes and reports what an agent can do in a directory.
type Harness interface {
	Start(ctx context.Context, o StartOptions, sink Sink) (Proc, error)
	Probe(ctx context.Context, cwd string) (Capabilities, error)
}

// Proc is one running agent process.
type Proc interface {
	Send(msg UserMessage) error
	Interrupt(ctx context.Context) error
	Close(grace time.Duration)
	Done() <-chan struct{}
}

// StartOptions configures one agent process.
type StartOptions struct {
	Cwd                string
	AddDirs            []string
	Model              string
	Effort             string
	PermissionMode     string
	ResumeID           string
	Fork               bool
	MCPURL             string
	MCPHeaders         map[string]string
	AllowedTools       []string
	PluginDirs         []string
	AppendSystemPrompt string
	Env                []string
}

// UserMessage is one user turn.
type UserMessage struct {
	Text   string
	Images []Image
	Files  []string
}

// Image is an inline image attachment.
type Image struct {
	MediaType string
	Data      []byte
}

// Sink receives everything a process reports. OnEvent and OnPermission are called
// in order from one goroutine and must not block; respond may be called later from
// any goroutine. OnExit is called once, with nil after Close or a clean exit.
type Sink interface {
	OnEvent(e Event)
	OnPermission(req PermissionRequest, respond func(Decision))
	OnExit(err error)
}

// EventType is the session event vocabulary shared by every harness.
type EventType string

const (
	EventUser       EventType = "user"
	EventText       EventType = "text"
	EventThinking   EventType = "thinking"
	EventToolUse    EventType = "tool_use"
	EventToolResult EventType = "tool_result"
	EventApproval   EventType = "approval"
	EventResult     EventType = "result"
	EventError      EventType = "error"
	EventSystem     EventType = "system"
)

// Event is one transcript entry. Partial events are live fragments, such as text
// deltas, that a later full event supersedes: worth broadcasting, not storing.
type Event struct {
	Type    EventType      `json:"type"`
	Partial bool           `json:"partial,omitempty"`
	Data    map[string]any `json:"data"`
}

// PermissionRequest asks whether the agent may run a tool.
type PermissionRequest struct {
	ID          string          `json:"id"`
	Tool        string          `json:"tool"`
	Input       json.RawMessage `json:"input"`
	ToolUseID   string          `json:"toolUseId,omitempty"`
	Description string          `json:"description,omitempty"`
	Suggestions json.RawMessage `json:"suggestions,omitempty"`
}

// Decision answers a PermissionRequest. Always also allows matching calls for the
// rest of the session. UpdatedInput replaces the tool input when set.
type Decision struct {
	Allow        bool
	Always       bool
	UpdatedInput json.RawMessage
	Message      string
}

// Capabilities lists what an agent offers in a directory.
type Capabilities struct {
	Commands   []Command   `json:"commands"`
	Models     []Model     `json:"models"`
	Agents     []Agent     `json:"agents"`
	MCPServers []MCPServer `json:"mcpServers"`
}

// Command is a slash command or skill. Plugin names the plugin that provides it.
type Command struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	ArgumentHint string `json:"argumentHint,omitempty"`
	Plugin       string `json:"plugin,omitempty"`
	Builtin      bool   `json:"builtin,omitempty"`
}

// Model is a selectable model with the effort levels it accepts.
type Model struct {
	Value       string   `json:"value"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description,omitempty"`
	Efforts     []string `json:"efforts,omitempty"`
}

// Agent is a subagent the main agent can delegate to.
type Agent struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// MCPServer is a configured MCP server or connector and its connection status.
type MCPServer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
}
