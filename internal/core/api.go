package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/store"
)

// Hooks are provided by the process hosting the API (the daemon).
type Hooks struct {
	OpenURL func(string) error
	// Shutdown stops the daemon; launchd starts it again (used to switch to a new version).
	Shutdown func()
}

type State struct {
	Version     string           `json:"version"`
	Projects    []store.Project  `json:"projects"`
	Folders     []store.Folder   `json:"folders"`
	Tasks       []store.Task     `json:"tasks"`
	Slices      []store.Slice    `json:"slices"`
	Sessions    []store.Session  `json:"sessions"`
	Approvals   []store.Approval `json:"approvals"`
	Running     int              `json:"running"`
	Queued      int              `json:"queued"`
	MaxRunning  int              `json:"maxRunning"`
	HasLinear   bool             `json:"hasLinear"`
	LinearOAuth bool             `json:"linearOAuth"` // connected with "Connect Linear" rather than an API key
}

func (a *App) linearRedirect() string {
	return fmt.Sprintf("http://127.0.0.1:%d/api/linear/callback", a.Config.Get().Port)
}

func (a *App) State() (State, error) {
	var s State
	var err error
	s.Version = a.Version
	if s.Projects, err = a.Store.Projects(); err != nil {
		return s, err
	}
	if s.Folders, err = a.Store.Folders(); err != nil {
		return s, err
	}
	if s.Tasks, err = a.Store.Tasks(); err != nil {
		return s, err
	}
	if s.Slices, err = a.Store.Slices(); err != nil {
		return s, err
	}
	if s.Sessions, err = a.Store.Sessions(); err != nil {
		return s, err
	}
	if s.Approvals, err = a.Store.PendingApprovals(); err != nil {
		return s, err
	}
	s.Running, s.Queued = a.Sessions.Counts()
	cfg := a.Config.Get()
	s.MaxRunning = cfg.MaxConcurrentSessions
	s.HasLinear, s.LinearOAuth = a.hasLinear(), a.Linear.Connected()
	return s, nil
}

// decorate adds rendered HTML to agent text so the UI never renders markdown itself.
func decorate(e store.Event) store.Event {
	if e.Type == "text" {
		if t, ok := e.Data["text"].(string); ok {
			d := make(map[string]any, len(e.Data)+1)
			for k, v := range e.Data {
				d[k] = v
			}
			d["html"] = Markdown(t)
			e.Data = d
		}
	}
	return e
}

// secret masking: the UI never receives tokens back.
const secretSet = "__set__"

func mask(s string) string {
	if s == "" {
		return ""
	}
	return secretSet
}

type httpError struct {
	status int
	msg    string
}

func (e *httpError) Error() string { return e.msg }

func bad(msg string) error { return &httpError{http.StatusBadRequest, msg} }

type handler func(r *http.Request) (any, error)

// Handler serves the UI's JSON API under /api/.
func (a *App) Handler(h Hooks) http.Handler {
	mux := http.NewServeMux()
	route := func(pattern string, fn handler) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			defer func() {
				if p := recover(); p != nil {
					log.Printf("api panic %s %s: %v", r.Method, r.URL.Path, p)
					w.WriteHeader(http.StatusInternalServerError)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "internal error"})
				}
			}()
			out, err := fn(r)
			if err != nil {
				status := http.StatusInternalServerError
				var he *httpError
				switch {
				case errors.As(err, &he):
					status = he.status
				case errors.Is(err, store.ErrNotFound):
					status = http.StatusNotFound
				default:
					status = http.StatusUnprocessableEntity // a git/GitHub/Linear failure the user can read and act on
				}
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
				return
			}
			if out == nil {
				out = map[string]bool{"ok": true}
			}
			_ = json.NewEncoder(w).Encode(out)
		})
	}
	ctx := func(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
		return context.WithTimeout(r.Context(), d)
	}

	route("GET /api/state", func(r *http.Request) (any, error) { return a.State() })

	route("GET /api/config", func(r *http.Request) (any, error) {
		c := a.Config.Get()
		c.LinearAPIKey, c.GitHubToken = mask(c.LinearAPIKey), mask(c.GitHubToken)
		return c, nil
	})
	route("PUT /api/config", func(r *http.Request) (any, error) {
		var in config.Config
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		cur := a.Config.Get()
		if in.LinearAPIKey == secretSet {
			in.LinearAPIKey = cur.LinearAPIKey
		}
		if in.GitHubToken == secretSet {
			in.GitHubToken = cur.GitHubToken
		}
		if err := a.Config.Save(in); err != nil {
			return nil, err
		}
		a.bus.Changed()
		return nil, nil
	})

	mux.HandleFunc("GET /api/update/manifest", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
		defer cancel()
		b, err := a.updateManifest(ctx, "http://"+r.Host)
		if err != nil {
			// Tauri's updater treats any non-2xx as "check failed"; 204 means "no update".
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	})
	mux.HandleFunc("GET /api/update/asset/{id}", a.serveUpdateAsset)
	route("GET /api/version", func(r *http.Request) (any, error) {
		return map[string]any{"version": a.Version, "pid": os.Getpid()}, nil
	})
	route("POST /api/shutdown", func(r *http.Request) (any, error) {
		var in struct{ Force bool }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if running, queued := a.Sessions.Counts(); running+queued > 0 && !in.Force {
			return nil, &httpError{http.StatusConflict, "agents are working"}
		}
		if h.Shutdown == nil {
			return nil, bad("shutdown unavailable")
		}
		go h.Shutdown()
		return nil, nil
	})
	route("POST /api/projects", func(r *http.Request) (any, error) {
		var in struct{ Name string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		return a.AddProject(in.Name)
	})
	route("DELETE /api/projects/{id}", func(r *http.Request) (any, error) {
		return nil, a.RemoveProject(r.PathValue("id"))
	})
	route("POST /api/projects/{id}/folders", func(r *http.Request) (any, error) {
		var in struct{ Path string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		c, cancel := ctx(r, time.Minute)
		defer cancel()
		return a.AddFolder(c, r.PathValue("id"), in.Path)
	})
	route("DELETE /api/folders/{id}", func(r *http.Request) (any, error) {
		return nil, a.RemoveFolder(r.PathValue("id"))
	})

	route("GET /api/linear/issues", func(r *http.Request) (any, error) {
		c, cancel := ctx(r, 30*time.Second)
		defer cancel()
		lc, err := a.linear(c)
		if err != nil {
			return nil, err
		}
		if q := strings.TrimSpace(r.URL.Query().Get("q")); q != "" {
			return lc.Search(c, q)
		}
		return lc.MyIssues(c)
	})

	route("POST /api/linear/connect", func(r *http.Request) (any, error) {
		u, err := a.Linear.AuthorizeURL(a.Config.Get().LinearClientID, a.linearRedirect())
		if err != nil {
			return nil, bad(err.Error())
		}
		if h.OpenURL != nil {
			if err := h.OpenURL(u); err != nil {
				return nil, err
			}
		}
		return map[string]string{"url": u}, nil
	})
	route("DELETE /api/linear/connect", func(r *http.Request) (any, error) {
		err := a.Linear.Disconnect()
		a.bus.Changed()
		return nil, err
	})
	// The browser lands here from Linear, without layer8's token (the server lets this
	// path through); the one-time state from AuthorizeURL authenticates it.
	mux.HandleFunc("GET /api/linear/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		err := errors.New(q.Get("error_description") + " " + q.Get("error"))
		if q.Get("code") != "" {
			c, cancel := ctx(r, 30*time.Second)
			defer cancel()
			err = a.Linear.Finish(c, a.Config.Get().LinearClientID, a.linearRedirect(), q.Get("state"), q.Get("code"))
		}
		a.bus.Changed()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		msg := "Linear is connected. You can close this tab and go back to layer8."
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			msg = "Linear sign-in failed: " + html.EscapeString(strings.TrimSpace(err.Error()))
		}
		fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>layer8</title><body style="font:15px system-ui;padding:48px">%s</body>`, msg)
	})

	route("POST /api/tasks", func(r *http.Request) (any, error) {
		var in NewTask
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		c, cancel := ctx(r, 3*time.Minute)
		defer cancel()
		return a.CreateTask(c, in)
	})
	route("POST /api/tasks/{id}/archive", func(r *http.Request) (any, error) {
		var in struct{ RemoveWorktrees bool }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		c, cancel := ctx(r, 2*time.Minute)
		defer cancel()
		return nil, a.ArchiveTask(c, r.PathValue("id"), in.RemoveWorktrees)
	})
	route("POST /api/tasks/{id}/sync", func(r *http.Request) (any, error) {
		c, cancel := ctx(r, 5*time.Minute)
		defer cancel()
		return nil, a.SyncTask(c, r.PathValue("id"))
	})
	route("GET /api/tasks/{id}/artifacts", func(r *http.Request) (any, error) {
		return a.Artifacts(r.PathValue("id"))
	})
	route("GET /api/tasks/{id}/artifacts/{name}", func(r *http.Request) (any, error) {
		content, err := a.ReadArtifact(r.PathValue("id"), r.PathValue("name"))
		if err != nil {
			return nil, err
		}
		out := map[string]string{"content": content}
		if strings.HasSuffix(strings.ToLower(r.PathValue("name")), ".md") {
			out["html"] = Markdown(content)
		}
		return out, nil
	})
	route("GET /api/tasks/{id}/comments", func(r *http.Request) (any, error) {
		return a.Store.Comments(r.PathValue("id"))
	})
	route("POST /api/tasks/{id}/slices", func(r *http.Request) (any, error) {
		c, cancel := ctx(r, 5*time.Minute)
		defer cancel()
		return a.CreateSlices(c, r.PathValue("id"))
	})
	route("POST /api/tasks/{id}/single-slice", func(r *http.Request) (any, error) {
		c, cancel := ctx(r, time.Minute)
		defer cancel()
		return a.CreateSingleSlice(c, r.PathValue("id"))
	})

	route("POST /api/sessions", func(r *http.Request) (any, error) {
		var in StartSession
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		c, cancel := ctx(r, 3*time.Minute)
		defer cancel()
		return a.StartSession(c, in)
	})
	route("GET /api/sessions/{id}/events", func(r *http.Request) (any, error) {
		after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
		evs, err := a.Store.Events(r.PathValue("id"), after)
		for i := range evs {
			evs[i] = decorate(evs[i])
		}
		return evs, err
	})
	route("POST /api/sessions/{id}/messages", func(r *http.Request) (any, error) {
		var in struct{ Text string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		return nil, a.SendMessage(r.PathValue("id"), in.Text)
	})
	route("POST /api/sessions/{id}/interrupt", func(r *http.Request) (any, error) {
		return nil, a.Sessions.Interrupt(r.PathValue("id"))
	})
	route("POST /api/sessions/{id}/resume-terminal", func(r *http.Request) (any, error) {
		c, cancel := ctx(r, 30*time.Second)
		defer cancel()
		return nil, a.ResumeInTerminal(c, r.PathValue("id"))
	})
	route("POST /api/approvals/{id}", func(r *http.Request) (any, error) {
		var in struct {
			Allow   bool
			Message string
		}
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if err := a.Sessions.Decide(r.PathValue("id"), in.Allow, in.Message); err != nil {
			return nil, &httpError{http.StatusConflict, err.Error()}
		}
		return nil, nil
	})

	route("POST /api/open", func(r *http.Request) (any, error) {
		var in struct{ TaskID, SliceID, Target string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		if in.Target == "editor" {
			return nil, a.OpenEditor(in.TaskID, in.SliceID)
		}
		c, cancel := ctx(r, 30*time.Second)
		defer cancel()
		return nil, a.OpenTerminal(c, in.TaskID, in.SliceID)
	})
	route("POST /api/open-url", func(r *http.Request) (any, error) {
		var in struct{ URL string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || h.OpenURL == nil {
			return nil, bad("only http(s) links can be opened")
		}
		return nil, h.OpenURL(u.String())
	})

	route("GET /api/diff", func(r *http.Request) (any, error) {
		q := r.URL.Query()
		c, cancel := ctx(r, time.Minute)
		defer cancel()
		return a.Diff(c, q.Get("task"), q.Get("slice"), q.Get("sha"))
	})
	route("POST /api/commit", func(r *http.Request) (any, error) {
		var in struct{ TaskID, SliceID, Message string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		c, cancel := ctx(r, time.Minute)
		defer cancel()
		sha, err := a.Commit(c, in.TaskID, in.SliceID, in.Message)
		return map[string]string{"sha": sha}, err
	})

	route("POST /api/comments", func(r *http.Request) (any, error) {
		var in store.Comment
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		return a.AddComment(in)
	})
	route("DELETE /api/comments/{id}", func(r *http.Request) (any, error) {
		return nil, a.DeleteComment(r.PathValue("id"))
	})
	route("POST /api/comments/send", func(r *http.Request) (any, error) {
		var in struct{ TaskID, SliceID, SessionID string }
		if err := readJSON(r, &in); err != nil {
			return nil, err
		}
		c, cancel := ctx(r, time.Minute)
		defer cancel()
		return a.SendComments(c, in.TaskID, in.SliceID, in.SessionID)
	})

	sliceAction := func(path string, fn func(context.Context, string) (any, error)) {
		route("POST /api/slices/{id}/"+path, func(r *http.Request) (any, error) {
			c, cancel := ctx(r, 5*time.Minute)
			defer cancel()
			return fn(c, r.PathValue("id"))
		})
	}
	sliceAction("pr", func(c context.Context, id string) (any, error) { return a.OpenPR(c, id) })
	sliceAction("push", func(c context.Context, id string) (any, error) { return nil, a.PushSlice(c, id) })
	sliceAction("fix-ci", func(c context.Context, id string) (any, error) { return a.FixCI(c, id) })
	sliceAction("review", func(c context.Context, id string) (any, error) { return a.AddressReview(c, id) })
	sliceAction("resolve", func(c context.Context, id string) (any, error) { return a.ResolveConflicts(c, id) })
	route("GET /api/slices/{id}/pr", func(r *http.Request) (any, error) {
		c, cancel := ctx(r, time.Minute)
		defer cancel()
		return a.PRDetails(c, r.PathValue("id"))
	})

	route("POST /api/sync", func(r *http.Request) (any, error) {
		go a.SyncAll(a.ctx)
		return nil, nil
	})
	route("POST /api/github/refresh", func(r *http.Request) (any, error) {
		go a.PollGitHub(a.ctx)
		return nil, nil
	})
	return mux
}

func readJSON(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return bad("could not read request")
	}
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return bad("invalid JSON: " + err.Error())
	}
	return nil
}
