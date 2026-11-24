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
	"strings"

	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

// Version is the daemon's version, set at build time with -ldflags -X.
var Version = "dev"

// CookieName is the cookie /auth sets so the UI is signed in.
const CookieName = "l8_token"

type server struct {
	store *store.Store
	hub   *Hub
	tasks *task.Service
	token string
}

// New returns the daemon's handler. Requests must target a loopback Host and come
// from no Origin or this same origin; /api/ also needs the token as a bearer
// header or as the cookie /auth?t=<token> sets.
func New(st *store.Store, hub *Hub, tasks *task.Service, token string, ui fs.FS) http.Handler {
	s := &server{store: st, hub: hub, tasks: tasks, token: token}
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
	return s.guard(mux)
}

func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopback(r.Host) {
			writeError(w, http.StatusForbidden, "forbidden host")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
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
	case errors.Is(err, task.ErrInvalid):
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
