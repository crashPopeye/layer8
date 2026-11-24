package session

import (
	"context"
	"errors"
)

// ResumePrompt is what a session interrupted mid-turn by a daemon restart is sent when it resumes.
const ResumePrompt = "Continue where you left off."

// Recover restores the sessions a daemon restart cut off. Their pending approvals
// go stale; a session that was mid-turn resumes its transcript with ResumePrompt,
// and any other becomes ready_for_input, to be respawned on its next Send.
func (m *Manager) Recover(ctx context.Context) error {
	sessions, err := m.store.SessionsWithStatus("starting", "running", "waiting_approval", "ready_for_input")
	if err != nil {
		return err
	}
	var errs []error
	for _, s := range sessions {
		if err := m.store.StalePendingApprovals(s.ID); err != nil {
			errs = append(errs, err)
		}
		switch {
		case s.InTurn && s.ClaudeSessionID != "":
			m.emit(s.ID, "system", "", map[string]string{"subtype": "recovered", "text": "layer8 restarted; resuming the turn"})
			if _, err := m.Send(ctx, s.ID, ResumePrompt); err != nil {
				errs = append(errs, err)
				s.Status, s.InTurn = "failed", false
				errs = append(errs, m.save(s))
			}
		case s.InTurn:
			m.emit(s.ID, "system", "", map[string]string{"subtype": "recovered", "text": "layer8 restarted before claude started; send the message again"})
			s.Status, s.InTurn = "failed", false
			errs = append(errs, m.save(s))
		default:
			s.Status = "ready_for_input"
			errs = append(errs, m.save(s))
		}
	}
	return errors.Join(errs...)
}
