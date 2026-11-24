// Package api is the daemon's HTTP surface: JSON routes and an SSE stream for
// the UI and the l8 CLI, plus the static UI, on loopback only.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

// Version is the daemon's version, set at build time with -ldflags -X.
var Version = "dev"

// CookieName is the cookie /auth sets so the UI is signed in.
const CookieName = "l8_token"

type server struct {
	store    *store.Store
	hub      *Hub
	tasks    *task.Service
	sessions *session.Manager
	token    string
}

// New returns the daemon's handler. Requests must target a loopback Host and come
// from no Origin or this same origin; /api/ also needs the token as a bearer
// header or as the cookie /auth?t=<token> sets. /artifact/ URLs carry their own
// key instead, so sandboxed artifacts (Origin: null) can load their files.
func New(st *store.Store, hub *Hub, tasks *task.Service, sessions *session.Manager, token string, ui fs.FS) http.Handler {
	s := &server{store: st, hub: hub, tasks: tasks, sessions: sessions, token: token}
	mux := http.NewServeMux()
	mux.Handle("GET /", noCache(http.FileServerFS(ui)))
	mux.HandleFunc("GET /auth", s.signIn)
	mux.HandleFunc("GET /api/version", s.version)
	mux.Handle("GET /api/events", hub)
	mux.HandleFunc("GET /api/projects", s.listProjects)
	mux.HandleFunc("POST /api/projects", s.addProject)
	mux.HandleFunc("PUT /api/projects/{id}", s.updateProject)
	mux.HandleFunc("GET /api/tasks", s.listTasks)
	mux.HandleFunc("POST /api/tasks", s.createTask)
	mux.HandleFunc("GET /api/tasks/{id}", s.getTask)
	mux.HandleFunc("PUT /api/tasks/{id}", s.updateTask)
	mux.HandleFunc("GET /api/tasks/{id}/sessions", s.listSessions)
	mux.HandleFunc("POST /api/tasks/{id}/sessions", s.startSession)
	mux.HandleFunc("GET /api/sessions/{id}/events", s.listEvents)
	mux.HandleFunc("GET /api/sessions/{id}/approvals", s.listApprovals)
	mux.HandleFunc("POST /api/sessions/{id}/messages", s.sendMessage)
	mux.HandleFunc("POST /api/sessions/{id}/interrupt", s.interrupt)
	mux.HandleFunc("POST /api/sessions/{id}/stop", s.stop)
	mux.HandleFunc("POST /api/approvals/{id}", s.decide)
	mux.HandleFunc("GET /api/attention", s.attention)
	mux.HandleFunc("POST /api/pick-folder", s.pickFolder)
	mux.HandleFunc("GET /api/tasks/{id}/artifacts", s.listArtifacts)
	mux.HandleFunc("GET /api/tasks/{id}/artifacts/{path...}", s.readArtifact)
	mux.HandleFunc("PUT /api/tasks/{id}/artifacts/{path...}", s.writeArtifact)
	mux.HandleFunc("GET /api/tasks/{id}/render/{path...}", s.renderArtifact)
	mux.HandleFunc("GET /api/tasks/{id}/comments", s.listComments)
	mux.HandleFunc("POST /api/tasks/{id}/comments", s.addComment)
	mux.HandleFunc("POST /api/tasks/{id}/comments/send", s.sendComments)
	mux.HandleFunc("GET /artifact/{id}/{key}/{path...}", s.sharedArtifact)
	return s.guard(mux)
}

func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopback(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && !strings.HasPrefix(r.URL.Path, "/artifact/") {
			writeError(w, http.StatusForbidden, "forbidden origin")
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && !s.authorized(r) {
			writeError(w, http.StatusUnauthorized, "bad or missing token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loopback(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func (s *server) authorized(r *http.Request) bool {
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		if c, err := r.Cookie(CookieName); err == nil {
			given = c.Value
		}
	}
	return s.valid(given)
}

func (s *server) valid(token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1
}

func (s *server) signIn(w http.ResponseWriter, r *http.Request) {
	if !s.valid(r.URL.Query().Get("t")) {
		writeError(w, http.StatusUnauthorized, "bad token")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: CookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"version": Version})
}

func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.Projects()
	reply(w, projects, err)
}

func (s *server) addProject(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if !decode(w, r, &in) {
		return
	}
	p, err := s.tasks.AddProject(r.Context(), in.Path)
	reply(w, p, err)
}

func (s *server) updateProject(w http.ResponseWriter, r *http.Request) {
	var in task.ProjectUpdate
	if !decode(w, r, &in) {
		return
	}
	p, err := s.tasks.UpdateProject(r.PathValue("id"), in)
	reply(w, p, err)
}

func (s *server) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	tasks, err := s.store.ListTasks(store.TaskFilter{ProjectID: q.Get("project"), Status: q.Get("status")})
	reply(w, tasks, err)
}

func (s *server) createTask(w http.ResponseWriter, r *http.Request) {
	var in task.CreateInput
	if !decode(w, r, &in) {
		return
	}
	t, err := s.tasks.Create(r.Context(), in)
	reply(w, t, err)
}

func (s *server) getTask(w http.ResponseWriter, r *http.Request) {
	d, err := s.tasks.Detail(r.PathValue("id"))
	reply(w, d, err)
}

func (s *server) updateTask(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string `json:"status"`
	}
	if !decode(w, r, &in) {
		return
	}
	t, err := s.tasks.SetStatus(r.PathValue("id"), in.Status)
	reply(w, t, err)
}

func (s *server) listSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.sessions.Sessions(r.PathValue("id"))
	reply(w, sessions, err)
}

func (s *server) startSession(w http.ResponseWriter, r *http.Request) {
	var in session.StartInput
	if !decode(w, r, &in) {
		return
	}
	ss, err := s.sessions.Start(r.Context(), r.PathValue("id"), in)
	reply(w, ss, err)
}

func (s *server) listEvents(w http.ResponseWriter, r *http.Request) {
	after, _ := strconv.Atoi(r.URL.Query().Get("after"))
	events, err := s.store.Events(r.PathValue("id"), after)
	reply(w, events, err)
}

func (s *server) listApprovals(w http.ResponseWriter, r *http.Request) {
	approvals, err := s.store.Approvals(r.PathValue("id"))
	reply(w, approvals, err)
}

func (s *server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text string `json:"text"`
	}
	if !decode(w, r, &in) {
		return
	}
	queued, err := s.sessions.Send(r.Context(), r.PathValue("id"), in.Text)
	reply(w, map[string]bool{"queued": queued}, err)
}

func (s *server) interrupt(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]bool{"ok": true}, s.sessions.Interrupt(r.PathValue("id")))
}

func (s *server) stop(w http.ResponseWriter, r *http.Request) {
	reply(w, map[string]bool{"ok": true}, s.sessions.Stop(r.PathValue("id")))
}

func (s *server) decide(w http.ResponseWriter, r *http.Request) {
	var in session.Decision
	if !decode(w, r, &in) {
		return
	}
	reply(w, map[string]bool{"ok": true}, s.sessions.Decide(r.Context(), r.PathValue("id"), in))
}

func (s *server) attention(w http.ResponseWriter, r *http.Request) {
	waiting, err := s.store.SessionsWithStatus("waiting_approval")
	reply(w, map[string]int{"waiting": len(waiting)}, err)
}

func (s *server) pickFolder(w http.ResponseWriter, r *http.Request) {
	path, err := pickFolder(r.Context())
	reply(w, map[string]string{"path": path}, err)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return false
	}
	return true
}

func reply(w http.ResponseWriter, v any, err error) {
	switch {
	case err == nil:
		writeJSON(w, v)
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, task.ErrInvalid), errors.Is(err, session.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func noCache(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		h.ServeHTTP(w, r)
	})
}
