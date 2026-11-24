// Package store keeps layer8's entities in one SQLite file.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a looked-up row does not exist.
var ErrNotFound = errors.New("not found")

// migrations run in order; the index of the last applied one is PRAGMA user_version.
// Later phases append entries and never edit existing ones.
var migrations = []string{
	`CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT, path TEXT UNIQUE, base_branch TEXT,
	  instructions TEXT, linear_team TEXT, linear_write_back INT DEFAULT 0, created_at INT);
	CREATE TABLE tasks (id TEXT PRIMARY KEY, project_id TEXT REFERENCES projects, parent_id TEXT REFERENCES tasks,
	  slug TEXT UNIQUE, title TEXT, workflow TEXT, phase TEXT, status TEXT,
	  branch TEXT, base_ref TEXT, worktree TEXT, ticket TEXT, ticket_url TEXT,
	  auto_advance INT DEFAULT 0, autonomous INT DEFAULT 0, pr_number INT, pr_url TEXT, created_at INT);`,
}

type Store struct{ db *sql.DB }

// Open opens (or creates) the database at path and applies pending migrations.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

type Project struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Path            string `json:"path"`
	BaseBranch      string `json:"baseBranch"`
	Instructions    string `json:"instructions"`
	LinearTeam      string `json:"linearTeam"`
	LinearWriteBack bool   `json:"linearWriteBack"`
	CreatedAt       int64  `json:"createdAt"`
}

type Task struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	ParentID    string `json:"parentId"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Workflow    string `json:"workflow"`
	Phase       string `json:"phase"`
	Status      string `json:"status"`
	Branch      string `json:"branch"`
	BaseRef     string `json:"baseRef"`
	Worktree    string `json:"worktree"`
	Ticket      string `json:"ticket"`
	TicketURL   string `json:"ticketUrl"`
	AutoAdvance bool   `json:"autoAdvance"`
	Autonomous  bool   `json:"autonomous"`
	PRNumber    int    `json:"prNumber"`
	PRURL       string `json:"prUrl"`
	CreatedAt   int64  `json:"createdAt"`
}

// TaskFilter narrows ListTasks; empty fields match everything.
type TaskFilter struct {
	ProjectID string
	Status    string
}

const projectCols = `id, name, path, COALESCE(base_branch,''), COALESCE(instructions,''), COALESCE(linear_team,''),
  linear_write_back, created_at`

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	err := row.Scan(&p.ID, &p.Name, &p.Path, &p.BaseBranch, &p.Instructions, &p.LinearTeam, &p.LinearWriteBack, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) InsertProject(p Project) error {
	_, err := s.db.Exec(`INSERT INTO projects (id, name, path, base_branch, instructions, linear_team, linear_write_back, created_at)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Path, p.BaseBranch, p.Instructions, p.LinearTeam, p.LinearWriteBack, p.CreatedAt)
	return err
}

func (s *Store) UpdateProject(p Project) error {
	_, err := s.db.Exec(`UPDATE projects SET name = ?, base_branch = ?, instructions = ?, linear_team = ?, linear_write_back = ?
	  WHERE id = ?`, p.Name, p.BaseBranch, p.Instructions, p.LinearTeam, p.LinearWriteBack, p.ID)
	return err
}

func (s *Store) Project(id string) (Project, error) {
	return scanProject(s.db.QueryRow(`SELECT `+projectCols+` FROM projects WHERE id = ?`, id))
}

func (s *Store) Projects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT ` + projectCols + ` FROM projects ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

const taskCols = `id, project_id, COALESCE(parent_id,''), slug, title, COALESCE(workflow,''), COALESCE(phase,''), status,
  COALESCE(branch,''), COALESCE(base_ref,''), COALESCE(worktree,''), COALESCE(ticket,''), COALESCE(ticket_url,''),
  auto_advance, autonomous, COALESCE(pr_number,0), COALESCE(pr_url,''), created_at`

func scanTask(row interface{ Scan(...any) error }) (Task, error) {
	var t Task
	err := row.Scan(&t.ID, &t.ProjectID, &t.ParentID, &t.Slug, &t.Title, &t.Workflow, &t.Phase, &t.Status,
		&t.Branch, &t.BaseRef, &t.Worktree, &t.Ticket, &t.TicketURL,
		&t.AutoAdvance, &t.Autonomous, &t.PRNumber, &t.PRURL, &t.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) InsertTask(t Task) error {
	_, err := s.db.Exec(`INSERT INTO tasks (id, project_id, parent_id, slug, title, workflow, phase, status,
	  branch, base_ref, worktree, ticket, ticket_url, auto_advance, autonomous, pr_number, pr_url, created_at)
	  VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, 0), ?, ?)`,
		t.ID, t.ProjectID, t.ParentID, t.Slug, t.Title, t.Workflow, t.Phase, t.Status,
		t.Branch, t.BaseRef, t.Worktree, t.Ticket, t.TicketURL, t.AutoAdvance, t.Autonomous, t.PRNumber, t.PRURL, t.CreatedAt)
	return err
}

// Task looks a task up by id or by slug.
func (s *Store) Task(idOrSlug string) (Task, error) {
	return scanTask(s.db.QueryRow(`SELECT `+taskCols+` FROM tasks WHERE id = ? OR slug = ?`, idOrSlug, idOrSlug))
}

// SlugTaken reports whether a task already uses slug.
func (s *Store) SlugTaken(slug string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE slug = ?`, slug).Scan(&n)
	return n > 0, err
}

// ListTasks returns tasks matching f, newest first.
func (s *Store) ListTasks(f TaskFilter) ([]Task, error) {
	var where []string
	var args []any
	if f.ProjectID != "" {
		where, args = append(where, "project_id = ?"), append(args, f.ProjectID)
	}
	if f.Status != "" {
		where, args = append(where, "status = ?"), append(args, f.Status)
	}
	q := `SELECT ` + taskCols + ` FROM tasks`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	rows, err := s.db.Query(q+` ORDER BY created_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *Store) SetTaskStatus(id, status string) error {
	res, err := s.db.Exec(`UPDATE tasks SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
