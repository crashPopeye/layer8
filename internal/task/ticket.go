package task

import (
	"context"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/linear"
	"github.com/crashPopeye/layer8/internal/store"
)

// TicketFile is the name of the Linear issue's rendering in a task's artifacts folder.
const TicketFile = "ticket.md"

// InReview is the Linear state a ticket moves to when its task's PR opens and the project writes back.
const InReview = "In Review"

// ErrNoLinear is returned by ticket features while no Linear API key is set.
var ErrNoLinear = fmt.Errorf("%w: Linear is not connected; paste a personal API key in Settings", ErrInvalid)

// UseLinear makes c the Linear client of ticket features; nil disables them.
func (s *Service) UseLinear(c *linear.Client) {
	s.linearMu.Lock()
	defer s.linearMu.Unlock()
	s.linear = c
}

func (s *Service) linearClient() (*linear.Client, error) {
	s.linearMu.RLock()
	defer s.linearMu.RUnlock()
	if s.linear == nil {
		return nil, ErrNoLinear
	}
	return s.linear, nil
}

// LinearConnected reports whether a Linear API key is set.
func (s *Service) LinearConnected() bool {
	_, err := s.linearClient()
	return err == nil
}

// SetLinearKey saves the Linear API key to config.json and uses it from now on; an empty key disconnects.
func (s *Service) SetLinearKey(key string) error {
	cfg, err := config.Load(s.home)
	if err != nil {
		return err
	}
	cfg.LinearAPIKey = key
	if err := config.Save(s.home, cfg); err != nil {
		return err
	}
	s.UseLinear(linear.New(key))
	return nil
}

// Issues lists the user's open issues for an empty q, else the issues q names or matches.
func (s *Service) Issues(ctx context.Context, q string) ([]linear.Summary, error) {
	c, err := s.linearClient()
	if err != nil {
		return nil, err
	}
	return c.Find(ctx, q)
}

// Issue fetches one Linear issue with its children and comments.
func (s *Service) Issue(ctx context.Context, id string) (linear.Issue, error) {
	c, err := s.linearClient()
	if err != nil {
		return linear.Issue{}, err
	}
	return c.Issue(ctx, id)
}

// ticketSlug names a ticket's task after the last segment of its branch, e.g. flow-1297-media-ingest-retry.
func ticketSlug(i linear.Issue) string {
	if i.BranchName != "" {
		return slugify(path.Base(i.BranchName))
	}
	return slugify(i.Identifier + " " + i.Title)
}

func (s *Service) writeTicket(t store.Task, i linear.Issue) error {
	return os.WriteFile(filepath.Join(s.ArtifactsDir(t.ID), TicketFile), []byte(linear.TicketMarkdown(i)), 0o644)
}

// SubtasksInput is the workflow of the sub-tasks made from a ticket's children
// (default: the parent's), and whether their first sessions start.
type SubtasksInput struct {
	Workflow string `json:"workflow"`
	Start    bool   `json:"start"`
}

// SubtasksFromChildren creates one sub-task of the task per child issue of its
// ticket, each on that child's branch, skipping children that already have one.
func (s *Service) SubtasksFromChildren(ctx context.Context, taskRef string, in SubtasksInput) ([]Created, error) {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return nil, err
	}
	if t.Ticket == "" {
		return nil, fmt.Errorf("%w: task %s has no Linear ticket", ErrInvalid, t.Slug)
	}
	issue, err := s.Issue(ctx, t.Ticket)
	if err != nil {
		return nil, err
	}
	return s.createChildren(ctx, t, issue, in)
}

func (s *Service) createChildren(ctx context.Context, t store.Task, issue linear.Issue, in SubtasksInput) ([]Created, error) {
	existing, err := s.store.ListTasks(store.TaskFilter{ParentID: t.ID})
	if err != nil {
		return nil, err
	}
	linked := map[string]bool{}
	for _, sub := range existing {
		linked[sub.Ticket] = true
	}
	if in.Workflow == "" {
		in.Workflow = t.Workflow
	}
	created := []Created{}
	for _, child := range issue.Children {
		if linked[child.Identifier] {
			continue
		}
		c, err := s.Create(ctx, CreateInput{ParentID: t.ID, Ticket: child.Identifier, Workflow: in.Workflow, AutoAdvance: t.AutoAdvance, Start: in.Start})
		if err != nil {
			return created, fmt.Errorf("sub-task for %s: %w", child.Identifier, err)
		}
		created = append(created, c)
	}
	return created, nil
}

// writeBack moves the task's ticket to In Review when its project writes back to Linear; failures are only logged.
func (s *Service) writeBack(ctx context.Context, t store.Task) {
	if t.Ticket == "" {
		return
	}
	p, err := s.store.Project(t.ProjectID)
	if err != nil || !p.LinearWriteBack {
		return
	}
	c, err := s.linearClient()
	if err == nil {
		err = c.SetState(ctx, t.Ticket, InReview)
	}
	if err != nil {
		log.Printf("task %s: move %s to %s: %v", t.Slug, t.Ticket, InReview, err)
	}
}
