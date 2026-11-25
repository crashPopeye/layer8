package api

import (
	"net/http"

	"github.com/crashPopeye/layer8/internal/task"
)

// settings is what the UI may know of config.json: never the key itself.
type settings struct {
	LinearConnected bool   `json:"linearConnected"`
	Theme           string `json:"theme"`
}

func (s *server) settings() settings {
	return settings{LinearConnected: s.tasks.LinearConnected(), Theme: s.tasks.Theme()}
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.settings())
}

func (s *server) putConfig(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LinearAPIKey *string `json:"linearApiKey"`
		Theme        *string `json:"theme"`
	}
	if !decode(w, r, &in) {
		return
	}
	var err error
	if in.LinearAPIKey != nil {
		err = s.tasks.SetLinearKey(*in.LinearAPIKey)
	}
	if in.Theme != nil && err == nil {
		err = s.tasks.SetTheme(*in.Theme)
	}
	reply(w, s.settings(), err)
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
