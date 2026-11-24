package store

import "strings"

// Comment is a note on one line of a task's artifact or diff, waiting to be sent to a session.
type Comment struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	Target    string `json:"target"`
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Quote     string `json:"quote"`
	Body      string `json:"body"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"createdAt"`
}

func (s *Store) InsertComment(c Comment) error {
	_, err := s.db.Exec(`INSERT INTO comments (id, task_id, target, path, line, quote, body, status, created_at)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.TaskID, c.Target, c.Path, c.Line, c.Quote, c.Body, c.Status, c.CreatedAt)
	return err
}

// Comments returns a task's comments in the given statuses (all when none), by path and line.
func (s *Store) Comments(taskID string, statuses ...string) ([]Comment, error) {
	q := `SELECT id, task_id, target, path, line, quote, body, status, created_at FROM comments WHERE task_id = ?`
	args := []any{taskID}
	if len(statuses) > 0 {
		q += ` AND status IN (` + strings.TrimSuffix(strings.Repeat("?,", len(statuses)), ",") + `)`
		for _, st := range statuses {
			args = append(args, st)
		}
	}
	rows, err := s.db.Query(q+` ORDER BY target, path, line, created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.TaskID, &c.Target, &c.Path, &c.Line, &c.Quote, &c.Body, &c.Status, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

func (s *Store) SetCommentStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE comments SET status = ? WHERE id = ?`, status, id)
	return err
}
