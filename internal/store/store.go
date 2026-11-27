// Package store opens the layer8 SQLite database and keeps its schema current.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// migrations are the schema steps in order; the database's user_version counts those applied.
var migrations = []string{
	`CREATE TABLE projects (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		path TEXT NOT NULL UNIQUE,
		default_branch TEXT NOT NULL,
		created_by TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	CREATE TABLE actions_log (
		id INTEGER PRIMARY KEY,
		at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		actor_kind TEXT NOT NULL,
		actor_id TEXT NOT NULL,
		target TEXT NOT NULL,
		action TEXT NOT NULL,
		args TEXT NOT NULL,
		ok INTEGER NOT NULL,
		message TEXT NOT NULL
	);`,
	`CREATE TABLE tasks (
		id INTEGER PRIMARY KEY,
		slug TEXT NOT NULL UNIQUE,
		parent_id INTEGER REFERENCES tasks(id),
		root_id INTEGER REFERENCES tasks(id),
		project_id INTEGER NOT NULL REFERENCES projects(id),
		title TEXT NOT NULL,
		branch TEXT NOT NULL,
		base_ref TEXT NOT NULL,
		worktree TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'done', 'archived')),
		linear TEXT,
		pr TEXT,
		created_by TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	CREATE INDEX tasks_project ON tasks (project_id);
	CREATE INDEX tasks_parent ON tasks (parent_id);`,
	`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		id8 TEXT NOT NULL UNIQUE,
		task_id INTEGER NOT NULL REFERENCES tasks(id),
		harness TEXT NOT NULL,
		model TEXT NOT NULL DEFAULT '',
		effort TEXT NOT NULL DEFAULT '',
		permission_mode TEXT NOT NULL DEFAULT '',
		harness_session_id TEXT NOT NULL DEFAULT '',
		fork INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'idle' CHECK (status IN ('queued', 'starting', 'running', 'waiting', 'idle', 'done', 'failed')),
		title TEXT NOT NULL,
		skill TEXT NOT NULL DEFAULT '',
		workflow_step TEXT NOT NULL DEFAULT '',
		cost_usd REAL NOT NULL DEFAULT 0,
		context_tokens INTEGER NOT NULL DEFAULT 0,
		capabilities TEXT,
		error TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	CREATE INDEX sessions_task ON sessions (task_id);
	CREATE TABLE session_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL REFERENCES sessions(id),
		type TEXT NOT NULL,
		at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
		data TEXT NOT NULL
	);
	CREATE INDEX session_events_session ON session_events (session_id, id);
	CREATE TABLE approvals (
		id INTEGER PRIMARY KEY,
		session_id TEXT NOT NULL REFERENCES sessions(id),
		request TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'allowed', 'denied', 'expired')),
		decided_by TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);
	CREATE INDEX approvals_session ON approvals (session_id, status);`,
	`CREATE TABLE workflow_runs (
		task_id INTEGER PRIMARY KEY REFERENCES tasks(id),
		workflow TEXT NOT NULL,
		steps TEXT NOT NULL,
		step INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL CHECK (status IN ('running', 'waiting', 'done', 'stopped')),
		autopilot INTEGER NOT NULL DEFAULT 0,
		entries TEXT NOT NULL DEFAULT '[]',
		created_by TEXT NOT NULL,
		updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);`,
}

// Open opens the database at path and applies pending migrations.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

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
