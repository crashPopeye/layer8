package session

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crashPopeye/layer8/internal/claude"
)

const previewBytes = 2048

// onMessage turns one stream message into stored events and session state.
func (m *Manager) onMessage(sessionID string, l *live, msg claude.Message) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.live[sessionID] != l {
		return
	}
	switch msg.Type {
	case "stream_event":
		m.onDelta(sessionID, l, msg)
	case "system":
		m.onSystem(sessionID, msg)
	case "assistant":
		m.onAssistant(sessionID, l, msg)
	case "user":
		for _, b := range msg.Message.Blocks() {
			if b.Type == "tool_result" {
				m.emit(sessionID, "tool_result", msg.ParentToolUseID, map[string]any{
					"toolUseId": b.ToolUseID, "isError": b.IsError, "preview": truncate(b.ResultText(), previewBytes),
				})
			}
		}
	case "result":
		m.onResult(sessionID, l, msg)
	}
}

func (m *Manager) onDelta(sessionID string, l *live, msg claude.Message) {
	if msg.Event == nil || msg.ParentToolUseID != "" {
		return
	}
	switch {
	case msg.Event.Type == "content_block_start":
		l.draft = ""
	case msg.Event.Type == "content_block_delta" && msg.Event.Delta.Type == "text_delta":
		l.draft += msg.Event.Delta.Text
		m.publish(map[string]any{"kind": "delta", "id": sessionID, "text": l.draft})
	}
}

func (m *Manager) onSystem(sessionID string, msg claude.Message) {
	switch msg.Subtype {
	case "init":
		s, err := m.store.Session(sessionID)
		if err != nil {
			return
		}
		s.ClaudeSessionID = msg.SessionID
		if s.Status == "starting" {
			s.Status = "running"
		}
		m.save(s)
	case "compact_boundary":
		m.emit(sessionID, "system", "", map[string]string{"subtype": msg.Subtype, "text": "Conversation compacted"})
	case "api_retry":
		m.emit(sessionID, "system", "", map[string]string{"subtype": msg.Subtype, "text": "API error, retrying"})
	}
}

func (m *Manager) onAssistant(sessionID string, l *live, msg claude.Message) {
	if msg.Message == nil {
		return
	}
	for _, b := range msg.Message.Blocks() {
		switch b.Type {
		case "text":
			if strings.TrimSpace(b.Text) != "" {
				m.emit(sessionID, "text", msg.ParentToolUseID, map[string]string{"text": b.Text})
			}
			if msg.ParentToolUseID == "" {
				l.draft = ""
			}
		case "tool_use":
			m.emit(sessionID, "tool", msg.ParentToolUseID, map[string]any{
				"id": b.ID, "name": b.Name, "input": b.Input, "summary": Summary(b.Name, b.Input),
			})
		}
	}
	if u := msg.Message.Usage; u != nil && msg.ParentToolUseID == "" {
		if s, err := m.store.Session(sessionID); err == nil && s.ContextTokens != u.Context() {
			s.ContextTokens = u.Context()
			m.save(s)
		}
	}
}

func (m *Manager) onResult(sessionID string, l *live, msg claude.Message) {
	s, err := m.store.Session(sessionID)
	if err != nil {
		return
	}
	s.CostUSD = l.baseCost + msg.TotalCostUSD
	body := map[string]any{"costUsd": s.CostUSD, "durationMs": msg.DurationMS, "isError": msg.IsError}
	if msg.IsError {
		body["text"] = msg.Result
	}
	m.emit(sessionID, "result", "", body)
	l.draft = ""
	s.InTurn = false
	if len(l.queue) > 0 {
		next := l.queue[0]
		l.queue = l.queue[1:]
		s.Status = "running"
		m.beginTurnLocked(&s, l, next)
		return
	}
	s.Status = "ready_for_input"
	m.save(s)
	m.notify(s, "attention")
}

// Summary is the one-line description of a tool call shown on its card.
func Summary(tool string, input json.RawMessage) string {
	var in map[string]any
	json.Unmarshal(input, &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	var detail string
	switch tool {
	case "Bash":
		detail = str("command")
	case "Read", "Write", "Edit", "MultiEdit", "NotebookEdit":
		detail = filepath.Base(str("file_path") + str("notebook_path"))
	case "Grep", "Glob":
		detail = str("pattern")
	case "Task", "Agent":
		detail = str("description")
	case "WebFetch":
		detail = str("url")
	case "WebSearch":
		detail = str("query")
	case "Skill":
		detail = str("skill")
	default:
		return tool
	}
	return fmt.Sprintf("%s: %s", tool, truncate(strings.Join(strings.Fields(detail), " "), 120))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8Start(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
