package task

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/store"
	"github.com/google/uuid"
)

// CommentInput is a new comment on a line of an artifact or of the task's diff.
type CommentInput struct {
	Target string `json:"target"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Quote  string `json:"quote"`
	Body   string `json:"body"`
}

// AddComment stores an open comment on the task.
func (s *Service) AddComment(taskRef string, in CommentInput) (store.Comment, error) {
	if in.Target == "" {
		in.Target = "artifact"
	}
	if in.Target != "artifact" && in.Target != "diff" {
		return store.Comment{}, fmt.Errorf("%w: target must be artifact or diff", ErrInvalid)
	}
	if strings.TrimSpace(in.Body) == "" || strings.TrimSpace(in.Path) == "" {
		return store.Comment{}, fmt.Errorf("%w: a comment needs a path and a body", ErrInvalid)
	}
	t, err := s.store.Task(taskRef)
	if err != nil {
		return store.Comment{}, err
	}
	c := store.Comment{
		ID:        uuid.Must(uuid.NewV7()).String(),
		TaskID:    t.ID,
		Target:    in.Target,
		Path:      path.Clean(in.Path),
		Line:      in.Line,
		Quote:     strings.TrimSpace(in.Quote),
		Body:      strings.TrimSpace(in.Body),
		Status:    "open",
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.store.InsertComment(c); err != nil {
		return store.Comment{}, err
	}
	s.notify("comments", t.ID)
	return c, nil
}

// Comments lists a task's comments by path and line.
func (s *Service) Comments(taskRef string) ([]store.Comment, error) {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return nil, err
	}
	return s.store.Comments(t.ID)
}

// SendInput picks the open comments to send (all when IDs is empty), the session that
// receives them, and whether they are resolved rather than just marked sent.
type SendInput struct {
	SessionID string   `json:"sessionId"`
	IDs       []string `json:"ids"`
	Resolve   bool     `json:"resolve"`
}

// SendComments sends the chosen open comments to a session as one message with send,
// then marks them sent or resolved. It returns how many were sent.
func (s *Service) SendComments(ctx context.Context, taskRef string, in SendInput, send func(ctx context.Context, sessionID, text string) error) (int, error) {
	if in.SessionID == "" {
		return 0, fmt.Errorf("%w: pick a session to send the comments to", ErrInvalid)
	}
	t, err := s.store.Task(taskRef)
	if err != nil {
		return 0, err
	}
	open, err := s.store.Comments(t.ID, "open")
	if err != nil {
		return 0, err
	}
	if len(in.IDs) > 0 {
		open = slices.DeleteFunc(open, func(c store.Comment) bool { return !slices.Contains(in.IDs, c.ID) })
	}
	if len(open) == 0 {
		return 0, fmt.Errorf("%w: no open comments to send", ErrInvalid)
	}
	if err := send(ctx, in.SessionID, CommentsMessage(open)); err != nil {
		return 0, err
	}
	status := "sent"
	if in.Resolve {
		status = "resolved"
	}
	for _, c := range open {
		if err := s.store.SetCommentStatus(c.ID, status); err != nil {
			return 0, err
		}
	}
	s.notify("comments", t.ID)
	return len(open), nil
}

// CommentsMessage is the message a session receives for review comments: each one's
// location, the quoted text and the comment.
func CommentsMessage(comments []store.Comment) string {
	var b strings.Builder
	b.WriteString("Please address these review comments:\n")
	for _, c := range comments {
		where := c.Path
		if c.Target == "artifact" {
			where = LinkPath + "/" + c.Path
		}
		if c.Line > 0 {
			where = fmt.Sprintf("%s:%d", where, c.Line)
		}
		fmt.Fprintf(&b, "\n%s\n", where)
		if c.Quote != "" {
			fmt.Fprintf(&b, "> %s\n", strings.ReplaceAll(c.Quote, "\n", "\n> "))
		}
		fmt.Fprintf(&b, "%s\n", c.Body)
	}
	return b.String()
}
