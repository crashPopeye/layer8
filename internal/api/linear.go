package api

import (
	"net/http"

	"github.com/crashPopeye/layer8/internal/task"
)

// settings is what the UI may know of config.json: never the key itself.
type settings struct {
	LinearConnected bool `json:"linearConnected"`
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, settings{LinearConnected: s.tasks.LinearConnected()})
}

func (s *server) putConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LinearAPIKey *string `json:"linearApiKey"`
	}
	if !decode(w, r, &in) {
		return
	}
	var err error
	if in.LinearAPIKey != nil {
		err = s.tasks.SetLinearKey(*in.LinearAPIKey)
	}
	reply(w, settings{LinearConnected: s.tasks.LinearConnected()}, err)
}

func (s *server) linearIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := s.tasks.Issues(r.Context(), r.URL.Query().Get("q"))
	reply(w, issues, err)
}

func (s *server) linearIssue(w http.ResponseWriter, r *http.Request) {
	issue, err := s.tasks.Issue(r.Context(), r.PathValue("identifier"))
	reply(w, issue, err)
}

func (s *server) subtasksFromChildren(w http.ResponseWriter, r *http.Request) {
	var in task.SubtasksInput
	if !decode(w, r, &in) {
		return
	}
	created, err := s.tasks.SubtasksFromChildren(r.Context(), r.PathValue("id"), in)
	reply(w, created, err)
}
