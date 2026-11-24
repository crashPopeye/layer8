package task

import (
	"context"
	"fmt"
	"log"
	"slices"

	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/workflow"
)

// PhaseState is a workflow phase and whether its work exists.
type PhaseState struct {
	workflow.Phase
	Done bool `json:"done"`
}

// Progress is where a task stands in its workflow: every phase, and the phase
// ready to start next ("" when there is none yet).
type Progress struct {
	Phases []PhaseState `json:"phases"`
	Next   string       `json:"next"`
}

// progress reads t's workflow phases against what t has produced.
func (s *Service) progress(ctx context.Context, t store.Task) Progress {
	phases := workflow.Table[t.Workflow]
	if len(phases) == 0 {
		return Progress{Phases: []PhaseState{}}
	}
	e := s.evidence(ctx, t)
	p := Progress{Phases: make([]PhaseState, len(phases))}
	for i, ph := range phases {
		p.Phases[i] = PhaseState{Phase: ph, Done: ph.Done(e)}
	}
	if next, ok := s.ready(t, e); ok {
		p.Next = next.Name
	}
	return p
}

// evidence gathers the artifact types, diff and PR that phases are judged by.
func (s *Service) evidence(ctx context.Context, t store.Task) workflow.Evidence {
	e := workflow.Evidence{HasPR: t.PRNumber != 0}
	if artifacts, err := s.Artifacts(t.ID); err == nil {
		for _, a := range artifacts {
			e.ArtifactTypes = append(e.ArtifactTypes, a.Type)
		}
	}
	e.Changed, _ = s.git.HasChanges(ctx, t.Worktree, t.BaseRef)
	return e
}

// ready returns the phase to start next: the first one before any phase ran, else
// the one after the current phase once its work exists and none of its sessions is mid-turn.
func (s *Service) ready(t store.Task, e workflow.Evidence) (workflow.Phase, bool) {
	if t.Phase == "" {
		return workflow.First(t.Workflow)
	}
	current, ok := workflow.Find(t.Workflow, t.Phase)
	if !ok || !current.Done(e) || s.phaseBusy(t) {
		return workflow.Phase{}, false
	}
	return workflow.Next(t.Workflow, t.Phase)
}

func (s *Service) phaseBusy(t store.Task) bool {
	sessions, err := s.store.Sessions(t.ID)
	return err != nil || slices.ContainsFunc(sessions, func(ss store.Session) bool { return ss.Label == t.Phase && ss.InTurn })
}

// PhaseCheck runs when one of a task's sessions goes idle; it advances the task's workflow.
func (s *Service) PhaseCheck(idle store.Session) {
	s.phaseMu.Lock()
	defer s.phaseMu.Unlock()
	if t, err := s.store.Task(idle.TaskID); err == nil {
		s.advance(context.Background(), t)
	}
}

// advance acts once t's current phase is done and idle: with auto-advance it
// starts the next phase's session, otherwise it tells the UI to offer it.
// Callers hold phaseMu.
func (s *Service) advance(ctx context.Context, t store.Task) {
	if t.Phase == "" || t.Status != "active" {
		return
	}
	next, ok := s.ready(t, s.evidence(ctx, t))
	if !ok {
		return
	}
	if !t.AutoAdvance {
		s.notify("task", t.ID)
		return
	}
	if _, err := s.startPhase(ctx, &t, next, ""); err != nil {
		log.Printf("task %s: auto-advance to %s: %v", t.Slug, next.Name, err)
	}
}

// Next starts the session of the phase after the task's current one, whether or not the current one is done.
func (s *Service) Next(ctx context.Context, taskRef string) (store.Session, error) {
	if s.sessions == nil {
		return store.Session{}, fmt.Errorf("%w: sessions are not available", ErrInvalid)
	}
	s.phaseMu.Lock()
	defer s.phaseMu.Unlock()
	t, err := s.store.Task(taskRef)
	if err != nil {
		return store.Session{}, err
	}
	next, ok := workflow.Next(t.Workflow, t.Phase)
	if !ok {
		return store.Session{}, fmt.Errorf("%w: workflow %s has no phase after %q", ErrInvalid, t.Workflow, t.Phase)
	}
	return s.startPhase(ctx, &t, next, "")
}

// startPhase makes p the task's phase and starts its session, labelled after the
// phase. Callers hold phaseMu.
func (s *Service) startPhase(ctx context.Context, t *store.Task, p workflow.Phase, prompt string) (store.Session, error) {
	t.Phase = p.Name
	if err := s.store.UpdateTask(*t); err != nil {
		return store.Session{}, err
	}
	s.notify("task", t.ID)
	return s.sessions.Start(ctx, t.ID, session.StartInput{Label: p.Name, Prompt: phaseMessage(p, prompt)})
}

// phaseMessage is the first message of a phase's session: the phase's skill
// pointed at the artifacts folder, or the task's prompt for a phase without one.
func phaseMessage(p workflow.Phase, prompt string) string {
	if p.Skill == "" {
		return firstMessage(prompt)
	}
	return p.Skill + " " + LinkPath + "/"
}
