package api

import (
	"net/http"

	"github.com/crashPopeye/layer8/internal/task"
)

// sessionPick names the session that receives a message; empty starts a new one.
type sessionPick struct {
	SessionID string `json:"sessionId"`
}

func (s *server) changes(w http.ResponseWriter, r *http.Request) {
	c, err := s.tasks.Changes(r.Context(), r.PathValue("id"))
	reply(w, c, err)
}

func (s *server) commit(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Message string `json:"message"`
	}
	if !decode(w, r, &in) {
		return
	}
	sha, err := s.tasks.Commit(r.Context(), r.PathValue("id"), in.Message)
	reply(w, map[string]string{"sha": sha}, err)
}

func (s *server) pr(w http.ResponseWriter, r *http.Request) {
	info, err := s.tasks.PR(r.Context(), r.PathValue("id"))
	reply(w, info, err)
}

func (s *server) openPR(w http.ResponseWriter, r *http.Request) {
	var in task.PRInput
	if !decode(w, r, &in) {
		return
	}
	pr, err := s.tasks.OpenPR(r.Context(), r.PathValue("id"), in)
	reply(w, pr, err)
}

func (s *server) push(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]bool{"ok": true}, s.tasks.Push(r.Context(), r.PathValue("id")))
}

func (s *server) markReady(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]bool{"ok": true}, s.tasks.MarkReady(r.Context(), r.PathValue("id")))
}

func (s *server) fixCI(w http.ResponseWriter, r *http.Request) {
	var in sessionPick
	if !decode(w, r, &in) {
		return
	}
	ss, err := s.tasks.FixCI(r.Context(), r.PathValue("id"), in.SessionID)
	reply(w, ss, err)
}

func (s *server) fixReviews(w http.ResponseWriter, r *http.Request) {
	var in sessionPick
	if !decode(w, r, &in) {
		return
	}
	ss, err := s.tasks.FixReviews(r.Context(), r.PathValue("id"), in.SessionID)
	reply(w, ss, err)
}

// draft asks an agent for the commit message (/commit/draft) or the PR description (/pr/draft).
func (s *server) draft(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in sessionPick
		if !decode(w, r, &in) {
			return
		}
		ss, err := s.tasks.Draft(r.Context(), r.PathValue("id"), kind, in.SessionID)
		reply(w, ss, err)
	}
}
