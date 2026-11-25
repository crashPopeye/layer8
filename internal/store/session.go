package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type Session struct {
	ID              string  `json:"id"`
	TaskID          string  `json:"taskId"`
	Label           string  `json:"label"`
	Phase           string  `json:"phase"`
	ClaudeSessionID string  `json:"claudeSessionId"`
	Model           string  `json:"model"`
	Effort          string  `json:"effort"`
	PermissionMode  string  `json:"permissionMode"`
	Status          string  `json:"status"`
	InTurn          bool    `json:"inTurn"`
	CostUSD         float64 `json:"costUsd"`
	ContextTokens   int     `json:"contextTokens"`
	ForkedFrom      string  `json:"forkedFrom"`
	CreatedAt       int64   `json:"createdAt"`
}

// Event is one rendered row of a session's conversation.
type Event struct {
	SessionID       string          `json:"sessionId"`
	Seq             int             `json:"seq"`
	Kind            string          `json:"kind"`
	ParentToolUseID string          `json:"parentToolUseId"`
	Body            json.RawMessage `json:"body"`
	CreatedAt       int64           `json:"createdAt"`
}

// Approval is a tool call waiting for, or given, the user's decision.
type Approval struct {
	ID          string          `json:"id"`
	SessionID   string          `json:"sessionId"`
	RequestID   string          `json:"requestId"`
	ToolUseID   string          `json:"toolUseId"`
	ToolName    string          `json:"toolName"`
	Input       json.RawMessage `json:"input"`
	Suggestions json.RawMessage `json:"suggestions"`
	Status      string          `json:"status"`
	CreatedAt   int64           `json:"createdAt"`
}

const sessionCols = `id, task_id, COALESCE(label,''), COALESCE(phase,''), COALESCE(claude_session_id,''),
  COALESCE(model,''), COALESCE(effort,''), COALESCE(permission_mode,''), status, in_turn,
  COALESCE(cost_usd,0), COALESCE(context_tokens,0), COALESCE(forked_from,''), created_at`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var s Session
	err := row.Scan(&s.ID, &s.TaskID, &s.Label, &s.Phase, &s.ClaudeSessionID, &s.Model, &s.Effort, &s.PermissionMode,
		&s.Status, &s.InTurn, &s.CostUSD, &s.ContextTokens, &s.ForkedFrom, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	return s, err
}

func (s *Store) InsertSession(ss Session) error {
	_, err := s.db.Exec(`INSERT INTO sessions (id, task_id, label, phase, claude_session_id, model, effort, permission_mode,
	  status, in_turn, cost_usd, context_tokens, forked_from, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ss.ID, ss.TaskID, ss.Label, ss.Phase, ss.ClaudeSessionID, ss.Model, ss.Effort, ss.PermissionMode,
		ss.Status, ss.InTurn, ss.CostUSD, ss.ContextTokens, ss.ForkedFrom, ss.CreatedAt)
	return err
}

// SaveSession writes every mutable field of ss.
func (s *Store) SaveSession(ss Session) error {
	res, err := s.db.Exec(`UPDATE sessions SET label = ?, phase = ?, claude_session_id = ?, model = ?, effort = ?,
	  permission_mode = ?, status = ?, in_turn = ?, cost_usd = ?, context_tokens = ? WHERE id = ?`,
		ss.Label, ss.Phase, ss.ClaudeSessionID, ss.Model, ss.Effort, ss.PermissionMode, ss.Status, ss.InTurn,
		ss.CostUSD, ss.ContextTokens, ss.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Session(id string) (Session, error) {
	return scanSession(s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
}

// Sessions returns a task's sessions, oldest first.
func (s *Store) Sessions(taskID string) ([]Session, error) {
	return s.querySessions(`SELECT `+sessionCols+` FROM sessions WHERE task_id = ? ORDER BY created_at, id`, taskID)
}

// ActiveTaskSessions returns the sessions of every active task, oldest first.
func (s *Store) ActiveTaskSessions() ([]Session, error) {
	return s.querySessions(`SELECT ` + sessionCols + ` FROM sessions
	  WHERE task_id IN (SELECT id FROM tasks WHERE status = 'active') ORDER BY created_at, id`)
}

// SessionsWithStatus returns every session in one of statuses, oldest first.
func (s *Store) SessionsWithStatus(statuses ...string) ([]Session, error) {
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = st
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",")
	return s.querySessions(`SELECT `+sessionCols+` FROM sessions WHERE status IN (`+marks+`) ORDER BY created_at, id`, args...)
}

func (s *Store) querySessions(query string, args ...any) ([]Session, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sessions := []Session{}
	for rows.Next() {
		ss, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, ss)
	}
	return sessions, rows.Err()
}

// AppendEvent stores body as the session's next event and returns it with its seq.
func (s *Store) AppendEvent(sessionID, kind, parentToolUseID string, body any) (Event, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return Event{}, err
	}
	e := Event{SessionID: sessionID, Kind: kind, ParentToolUseID: parentToolUseID, Body: raw, CreatedAt: time.Now().UnixMilli()}
	err = s.db.QueryRow(`INSERT INTO events (session_id, seq, kind, parent_tool_use_id, body, created_at)
	  SELECT ?, COALESCE(MAX(seq), 0) + 1, ?, ?, ?, ? FROM events WHERE session_id = ? RETURNING seq`,
		sessionID, kind, parentToolUseID, string(raw), e.CreatedAt, sessionID).Scan(&e.Seq)
	return e, err
}

// CopyEvents gives session to the conversation of session from, approvals left out since they belong to from.
func (s *Store) CopyEvents(from, to string) error {
	_, err := s.db.Exec(`INSERT INTO events (session_id, seq, kind, parent_tool_use_id, body, created_at)
	  SELECT ?, seq, kind, parent_tool_use_id, body, created_at FROM events WHERE session_id = ? AND kind != 'approval'`, to, from)
	return err
}

// Events returns a session's events with seq greater than after, in order.
func (s *Store) Events(sessionID string, after int) ([]Event, error) {
	rows, err := s.db.Query(`SELECT session_id, seq, kind, COALESCE(parent_tool_use_id,''), body, created_at
	  FROM events WHERE session_id = ? AND seq > ? ORDER BY seq`, sessionID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []Event{}
	for rows.Next() {
		var e Event
		var body string
		if err := rows.Scan(&e.SessionID, &e.Seq, &e.Kind, &e.ParentToolUseID, &body, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Body = json.RawMessage(body)
		events = append(events, e)
	}
	return events, rows.Err()
}

const approvalCols = `id, session_id, request_id, COALESCE(tool_use_id,''), tool_name, input, suggestions, status, created_at`

func scanApproval(row interface{ Scan(...any) error }) (Approval, error) {
	var a Approval
	var input, suggestions string
	err := row.Scan(&a.ID, &a.SessionID, &a.RequestID, &a.ToolUseID, &a.ToolName, &input, &suggestions, &a.Status, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	a.Input, a.Suggestions = json.RawMessage(input), json.RawMessage(suggestions)
	return a, err
}

func (s *Store) InsertApproval(a Approval) error {
	_, err := s.db.Exec(`INSERT INTO approvals (id, session_id, request_id, tool_use_id, tool_name, input, suggestions, status, created_at)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.SessionID, a.RequestID, a.ToolUseID, a.ToolName, string(orNull(a.Input)), string(orNull(a.Suggestions)), a.Status, a.CreatedAt)
	return err
}

func (s *Store) Approval(id string) (Approval, error) {
	return scanApproval(s.db.QueryRow(`SELECT `+approvalCols+` FROM approvals WHERE id = ?`, id))
}

// Approvals returns a session's approvals, oldest first.
func (s *Store) Approvals(sessionID string) ([]Approval, error) {
	rows, err := s.db.Query(`SELECT `+approvalCols+` FROM approvals WHERE session_id = ? ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	approvals := []Approval{}
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, a)
	}
	return approvals, rows.Err()
}

func (s *Store) SetApprovalStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE approvals SET status = ? WHERE id = ?`, status, id)
	return err
}

// StalePendingApprovals marks a session's pending approvals stale.
func (s *Store) StalePendingApprovals(sessionID string) error {
	_, err := s.db.Exec(`UPDATE approvals SET status = 'stale' WHERE session_id = ? AND status = 'pending'`, sessionID)
	return err
}

func orNull(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("null")
	}
	return raw
}
