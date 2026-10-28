// Package store persists layer8's state in SQLite (pure Go driver, no cgo).
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type Workspace struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	IsGit         bool   `json:"isGit"`
	DefaultBranch string `json:"defaultBranch"`
	CreatedAt     string `json:"createdAt"`
}

type LinearRef struct {
	ID         string `json:"id"`
	Identifier string `json:"identifier"`
	URL        string `json:"url"`
	BranchName string `json:"branchName"`
}

type Task struct {
	ID            string     `json:"id"`
	WorkspaceID   string     `json:"workspaceId"`
	Slug          string     `json:"slug"`
	Title         string     `json:"title"`
	Linear        *LinearRef `json:"linear"`
	Branch        string     `json:"branch"`  // "" when the workspace is not a git repo
	BaseRef       string     `json:"baseRef"` // e.g. origin/main
	BaseSHA       string     `json:"baseSha"` // commit the branch was last rebased onto
	WorktreePath  string     `json:"worktreePath"`
	ArtifactsPath string     `json:"artifactsPath"`
	TmuxSession   string     `json:"tmuxSession"`
	Status        string     `json:"status"` // active, archived
	SyncState     string     `json:"syncState"`
	SyncMessage   string     `json:"syncMessage"`
	CreatedAt     string     `json:"createdAt"`
}

// Slice is one pull request of a task's plan, on its own stacked branch and worktree.
type Slice struct {
	ID           string     `json:"id"`
	TaskID       string     `json:"taskId"`
	Key          string     `json:"key"` // id from the plan, e.g. pr2
	Title        string     `json:"title"`
	Position     int        `json:"position"`
	DependsOn    []string   `json:"dependsOn"`
	ParentKey    string     `json:"parentKey"` // "" = stacked on the task's base
	Branch       string     `json:"branch"`
	WorktreePath string     `json:"worktreePath"`
	BaseSHA      string     `json:"baseSha"`
	TmuxSession  string     `json:"tmuxSession"`
	Linear       *LinearRef `json:"linear"`
	PRNumber     int        `json:"prNumber"`
	PRURL        string     `json:"prUrl"`
	PRState      string     `json:"prState"` // OPEN, MERGED, CLOSED, ""
	PRDraft      bool       `json:"prDraft"`
	PRBase       string     `json:"prBase"`
	CIState      string     `json:"ciState"` // SUCCESS, FAILURE, PENDING, ""
	ReviewState  string     `json:"reviewState"`
	OpenThreads  int        `json:"openThreads"`
	Mergeable    string     `json:"mergeable"`
	SyncState    string     `json:"syncState"` // ok, behind, conflict, dirty, busy, error
	SyncMessage  string     `json:"syncMessage"`
	CreatedAt    string     `json:"createdAt"`
	UpdatedAt    string     `json:"updatedAt"`
}

type Session struct {
	ID              string  `json:"id"`
	TaskID          string  `json:"taskId"`
	SliceID         string  `json:"sliceId"`
	Kind            string  `json:"kind"`
	Title           string  `json:"title"`
	ClaudeSessionID string  `json:"claudeSessionId"`
	Status          string  `json:"status"` // queued, running, waiting_approval, idle, failed, interrupted
	Cwd             string  `json:"cwd"`
	CostUSD         float64 `json:"costUsd"`
	LastError       string  `json:"lastError"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
}

type Event struct {
	ID        int64          `json:"id"`
	SessionID string         `json:"sessionId"`
	Type      string         `json:"type"`
	At        string         `json:"at"`
	Data      map[string]any `json:"data"`
}

type Approval struct {
	ID        string         `json:"id"`
	SessionID string         `json:"sessionId"`
	ToolName  string         `json:"toolName"`
	Input     map[string]any `json:"input"`
	Status    string         `json:"status"` // pending, allowed, denied, cancelled
	CreatedAt string         `json:"createdAt"`
}

// Comment is a review note on an artifact or diff line, queued until sent to an agent.
type Comment struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	SliceID   string `json:"sliceId"`
	Target    string `json:"target"` // artifact, diff
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Quote     string `json:"quote"`
	Body      string `json:"body"`
	Status    string `json:"status"` // open, sent
	CreatedAt string `json:"createdAt"`
}

const schema = `
CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY, name TEXT NOT NULL, path TEXT NOT NULL UNIQUE,
  is_git INTEGER NOT NULL, default_branch TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  slug TEXT NOT NULL UNIQUE, title TEXT NOT NULL, linear TEXT NOT NULL DEFAULT '',
  branch TEXT NOT NULL DEFAULT '', base_ref TEXT NOT NULL DEFAULT '', base_sha TEXT NOT NULL DEFAULT '',
  worktree_path TEXT NOT NULL, artifacts_path TEXT NOT NULL, tmux_session TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL, sync_state TEXT NOT NULL DEFAULT '', sync_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS slices (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  key TEXT NOT NULL, title TEXT NOT NULL, position INTEGER NOT NULL, depends_on TEXT NOT NULL DEFAULT '[]',
  parent_key TEXT NOT NULL DEFAULT '', branch TEXT NOT NULL, worktree_path TEXT NOT NULL,
  base_sha TEXT NOT NULL DEFAULT '', tmux_session TEXT NOT NULL DEFAULT '', linear TEXT NOT NULL DEFAULT '',
  pr_number INTEGER NOT NULL DEFAULT 0, pr_url TEXT NOT NULL DEFAULT '', pr_state TEXT NOT NULL DEFAULT '',
  pr_draft INTEGER NOT NULL DEFAULT 0, pr_base TEXT NOT NULL DEFAULT '', ci_state TEXT NOT NULL DEFAULT '',
  review_state TEXT NOT NULL DEFAULT '', open_threads INTEGER NOT NULL DEFAULT 0, mergeable TEXT NOT NULL DEFAULT '',
  sync_state TEXT NOT NULL DEFAULT '', sync_message TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL, UNIQUE(task_id, key));
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  slice_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL, title TEXT NOT NULL,
  claude_session_id TEXT NOT NULL DEFAULT '', status TEXT NOT NULL, cwd TEXT NOT NULL,
  cost_usd REAL NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL, updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS session_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  type TEXT NOT NULL, at TEXT NOT NULL, data TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS session_events_by_session ON session_events(session_id, id);
CREATE TABLE IF NOT EXISTS approvals (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL, input TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS comments (
  id TEXT PRIMARY KEY, task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  slice_id TEXT NOT NULL DEFAULT '', target TEXT NOT NULL, path TEXT NOT NULL, line INTEGER NOT NULL,
  quote TEXT NOT NULL DEFAULT '', body TEXT NOT NULL, status TEXT NOT NULL, created_at TEXT NOT NULL);
`

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	// One connection: SQLite serialises writers anyway, and this keeps PRAGMAs per-connection simple.
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func NewID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func Now() string { return time.Now().UTC().Format(time.RFC3339Nano) }

var ErrNotFound = errors.New("not found")

func jsonOrEmpty(v any) string {
	switch x := v.(type) {
	case *LinearRef:
		if x == nil {
			return ""
		}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func linearFrom(s string) *LinearRef {
	if s == "" {
		return nil
	}
	var l LinearRef
	if json.Unmarshal([]byte(s), &l) != nil {
		return nil
	}
	return &l
}

type scanner interface{ Scan(dest ...any) error }

// ---- workspaces

func (s *Store) InsertWorkspace(w Workspace) (Workspace, error) {
	w.ID, w.CreatedAt = NewID(), Now()
	_, err := s.db.Exec(`INSERT INTO workspaces VALUES (?,?,?,?,?,?)`, w.ID, w.Name, w.Path, w.IsGit, w.DefaultBranch, w.CreatedAt)
	if err != nil {
		return w, fmt.Errorf("add workspace: %w", err)
	}
	return w, nil
}

func scanWorkspace(r scanner) (Workspace, error) {
	var w Workspace
	err := r.Scan(&w.ID, &w.Name, &w.Path, &w.IsGit, &w.DefaultBranch, &w.CreatedAt)
	return w, err
}

func (s *Store) Workspaces() ([]Workspace, error) {
	rows, err := s.db.Query(`SELECT id,name,path,is_git,default_branch,created_at FROM workspaces ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Workspace{}
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) Workspace(id string) (Workspace, error) {
	w, err := scanWorkspace(s.db.QueryRow(`SELECT id,name,path,is_git,default_branch,created_at FROM workspaces WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

func (s *Store) DeleteWorkspace(id string) error {
	_, err := s.db.Exec(`DELETE FROM workspaces WHERE id=?`, id)
	return err
}

// ---- tasks

const taskCols = `id,workspace_id,slug,title,linear,branch,base_ref,base_sha,worktree_path,artifacts_path,tmux_session,status,sync_state,sync_message,created_at`

func scanTask(r scanner) (Task, error) {
	var t Task
	var lin string
	err := r.Scan(&t.ID, &t.WorkspaceID, &t.Slug, &t.Title, &lin, &t.Branch, &t.BaseRef, &t.BaseSHA, &t.WorktreePath,
		&t.ArtifactsPath, &t.TmuxSession, &t.Status, &t.SyncState, &t.SyncMessage, &t.CreatedAt)
	t.Linear = linearFrom(lin)
	return t, err
}

func (s *Store) InsertTask(t Task) (Task, error) {
	if t.ID == "" {
		t.ID = NewID()
	}
	t.CreatedAt = Now()
	_, err := s.db.Exec(`INSERT INTO tasks (`+taskCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.WorkspaceID, t.Slug, t.Title, jsonOrEmpty(t.Linear), t.Branch, t.BaseRef, t.BaseSHA, t.WorktreePath,
		t.ArtifactsPath, t.TmuxSession, t.Status, t.SyncState, t.SyncMessage, t.CreatedAt)
	return t, err
}

func (s *Store) Tasks() ([]Task, error) {
	rows, err := s.db.Query(`SELECT ` + taskCols + ` FROM tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) Task(id string) (Task, error) {
	t, err := scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) SlugTaken(slug string) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE slug=?`, slug).Scan(&n)
	return n > 0
}

func (s *Store) SetTaskStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE tasks SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) SetTaskSync(id, state, msg, baseSHA string) error {
	_, err := s.db.Exec(`UPDATE tasks SET sync_state=?, sync_message=?, base_sha=CASE WHEN ?='' THEN base_sha ELSE ? END WHERE id=?`,
		state, msg, baseSHA, baseSHA, id)
	return err
}

// ---- slices

const sliceCols = `id,task_id,key,title,position,depends_on,parent_key,branch,worktree_path,base_sha,tmux_session,linear,
pr_number,pr_url,pr_state,pr_draft,pr_base,ci_state,review_state,open_threads,mergeable,sync_state,sync_message,created_at,updated_at`

func scanSlice(r scanner) (Slice, error) {
	var x Slice
	var deps, lin string
	err := r.Scan(&x.ID, &x.TaskID, &x.Key, &x.Title, &x.Position, &deps, &x.ParentKey, &x.Branch, &x.WorktreePath, &x.BaseSHA,
		&x.TmuxSession, &lin, &x.PRNumber, &x.PRURL, &x.PRState, &x.PRDraft, &x.PRBase, &x.CIState, &x.ReviewState,
		&x.OpenThreads, &x.Mergeable, &x.SyncState, &x.SyncMessage, &x.CreatedAt, &x.UpdatedAt)
	_ = json.Unmarshal([]byte(deps), &x.DependsOn)
	if x.DependsOn == nil {
		x.DependsOn = []string{}
	}
	x.Linear = linearFrom(lin)
	return x, err
}

func (s *Store) InsertSlice(x Slice) (Slice, error) {
	x.ID = NewID()
	x.CreatedAt, x.UpdatedAt = Now(), Now()
	if x.DependsOn == nil {
		x.DependsOn = []string{}
	}
	_, err := s.db.Exec(`INSERT INTO slices (`+sliceCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.TaskID, x.Key, x.Title, x.Position, jsonOrEmpty(x.DependsOn), x.ParentKey, x.Branch, x.WorktreePath, x.BaseSHA,
		x.TmuxSession, jsonOrEmpty(x.Linear), x.PRNumber, x.PRURL, x.PRState, x.PRDraft, x.PRBase, x.CIState, x.ReviewState,
		x.OpenThreads, x.Mergeable, x.SyncState, x.SyncMessage, x.CreatedAt, x.UpdatedAt)
	return x, err
}

func (s *Store) querySlices(where string, args ...any) ([]Slice, error) {
	rows, err := s.db.Query(`SELECT `+sliceCols+` FROM slices `+where+` ORDER BY task_id, position`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Slice{}
	for rows.Next() {
		x, err := scanSlice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) Slices() ([]Slice, error) { return s.querySlices("") }

func (s *Store) TaskSlices(taskID string) ([]Slice, error) {
	return s.querySlices("WHERE task_id=?", taskID)
}

func (s *Store) Slice(id string) (Slice, error) {
	x, err := scanSlice(s.db.QueryRow(`SELECT `+sliceCols+` FROM slices WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

// UpdateSlice writes every mutable field.
func (s *Store) UpdateSlice(x Slice) error {
	_, err := s.db.Exec(`UPDATE slices SET title=?, parent_key=?, base_sha=?, tmux_session=?, linear=?, pr_number=?, pr_url=?,
	  pr_state=?, pr_draft=?, pr_base=?, ci_state=?, review_state=?, open_threads=?, mergeable=?, sync_state=?, sync_message=?,
	  updated_at=? WHERE id=?`,
		x.Title, x.ParentKey, x.BaseSHA, x.TmuxSession, jsonOrEmpty(x.Linear), x.PRNumber, x.PRURL, x.PRState, x.PRDraft, x.PRBase,
		x.CIState, x.ReviewState, x.OpenThreads, x.Mergeable, x.SyncState, x.SyncMessage, Now(), x.ID)
	return err
}

// ---- sessions

const sessionCols = `id,task_id,slice_id,kind,title,claude_session_id,status,cwd,cost_usd,last_error,created_at,updated_at`

func scanSession(r scanner) (Session, error) {
	var x Session
	err := r.Scan(&x.ID, &x.TaskID, &x.SliceID, &x.Kind, &x.Title, &x.ClaudeSessionID, &x.Status, &x.Cwd, &x.CostUSD,
		&x.LastError, &x.CreatedAt, &x.UpdatedAt)
	return x, err
}

func (s *Store) InsertSession(x Session) (Session, error) {
	x.ID = NewID()
	x.CreatedAt, x.UpdatedAt = Now(), Now()
	if x.Status == "" {
		x.Status = "queued"
	}
	_, err := s.db.Exec(`INSERT INTO sessions (`+sessionCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		x.ID, x.TaskID, x.SliceID, x.Kind, x.Title, x.ClaudeSessionID, x.Status, x.Cwd, x.CostUSD, x.LastError, x.CreatedAt, x.UpdatedAt)
	return x, err
}

func (s *Store) Sessions() ([]Session, error) {
	rows, err := s.db.Query(`SELECT ` + sessionCols + ` FROM sessions ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		x, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) Session(id string) (Session, error) {
	x, err := scanSession(s.db.QueryRow(`SELECT `+sessionCols+` FROM sessions WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrNotFound
	}
	return x, err
}

func (s *Store) SetSessionStatus(id, status, lastError string) error {
	_, err := s.db.Exec(`UPDATE sessions SET status=?, last_error=?, updated_at=? WHERE id=?`, status, lastError, Now(), id)
	return err
}

func (s *Store) SetClaudeSessionID(id, claudeID string) error {
	_, err := s.db.Exec(`UPDATE sessions SET claude_session_id=? WHERE id=?`, claudeID, id)
	return err
}

// SetSessionCost stores the CLI's cumulative cost; it never goes down.
func (s *Store) SetSessionCost(id string, usd float64) error {
	_, err := s.db.Exec(`UPDATE sessions SET cost_usd=MAX(cost_usd, ?) WHERE id=?`, usd, id)
	return err
}

// RecoverSessions marks work interrupted by a previous shutdown; it can be resumed.
func (s *Store) RecoverSessions() error {
	if _, err := s.db.Exec(`UPDATE sessions SET status='interrupted', updated_at=? WHERE status IN ('queued','running','waiting_approval')`, Now()); err != nil {
		return err
	}
	_, err := s.db.Exec(`UPDATE approvals SET status='cancelled' WHERE status='pending'`)
	return err
}

// ---- events

func (s *Store) AddEvent(sessionID, typ string, data map[string]any) (Event, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return Event{}, err
	}
	at := Now()
	res, err := s.db.Exec(`INSERT INTO session_events (session_id,type,at,data) VALUES (?,?,?,?)`, sessionID, typ, at, string(b))
	if err != nil {
		return Event{}, err
	}
	id, _ := res.LastInsertId()
	return Event{ID: id, SessionID: sessionID, Type: typ, At: at, Data: data}, nil
}

func (s *Store) Events(sessionID string, afterID int64) ([]Event, error) {
	rows, err := s.db.Query(`SELECT id,session_id,type,at,data FROM session_events WHERE session_id=? AND id>? ORDER BY id`, sessionID, afterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var e Event
		var data string
		if err := rows.Scan(&e.ID, &e.SessionID, &e.Type, &e.At, &data); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(data), &e.Data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// ---- approvals

func (s *Store) InsertApproval(sessionID, tool string, input map[string]any) (Approval, error) {
	a := Approval{ID: NewID(), SessionID: sessionID, ToolName: tool, Input: input, Status: "pending", CreatedAt: Now()}
	b, _ := json.Marshal(input)
	_, err := s.db.Exec(`INSERT INTO approvals VALUES (?,?,?,?,?,?)`, a.ID, sessionID, tool, string(b), a.Status, a.CreatedAt)
	return a, err
}

func (s *Store) SetApprovalStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE approvals SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) PendingApprovals() ([]Approval, error) {
	rows, err := s.db.Query(`SELECT id,session_id,tool_name,input,status,created_at FROM approvals WHERE status='pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Approval{}
	for rows.Next() {
		var a Approval
		var in string
		if err := rows.Scan(&a.ID, &a.SessionID, &a.ToolName, &in, &a.Status, &a.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(in), &a.Input)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- comments

func (s *Store) InsertComment(c Comment) (Comment, error) {
	c.ID, c.CreatedAt, c.Status = NewID(), Now(), "open"
	_, err := s.db.Exec(`INSERT INTO comments VALUES (?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.TaskID, c.SliceID, c.Target, c.Path, c.Line, c.Quote, c.Body, c.Status, c.CreatedAt)
	return c, err
}

func (s *Store) Comments(taskID string) ([]Comment, error) {
	rows, err := s.db.Query(`SELECT id,task_id,slice_id,target,path,line,quote,body,status,created_at FROM comments
	  WHERE task_id=? ORDER BY created_at`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.SliceID, &c.Target, &c.Path, &c.Line, &c.Quote, &c.Body, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) DeleteComment(id string) error {
	_, err := s.db.Exec(`DELETE FROM comments WHERE id=? AND status='open'`, id)
	return err
}

func (s *Store) MarkCommentsSent(ids []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := tx.Exec(`UPDATE comments SET status='sent' WHERE id=?`, id); err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}
