// Package server serves the HTTP API, the live event stream and the MCP endpoint on loopback.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/crashPopeye/layer8/internal/tree"
)

// Options configures New.
type Options struct {
	Tree    *tree.Tree
	Hub     *Hub
	Token   string
	Version string
	MCP     http.Handler
	// Routes are extra token-protected handlers keyed by ServeMux pattern.
	Routes map[string]http.Handler
}

// New returns the daemon's root handler.
func New(o Options) http.Handler {
	authed := func(h http.HandlerFunc) http.Handler { return requireToken(o.Token, h) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": o.Version})
	})
	mux.Handle("GET /api/node", authed(func(w http.ResponseWriter, r *http.Request) { getNode(o.Tree, w, r) }))
	mux.Handle("POST /api/do", authed(func(w http.ResponseWriter, r *http.Request) { do(o.Tree, w, r) }))
	mux.Handle("GET /api/events", authed(func(w http.ResponseWriter, r *http.Request) { events(o.Hub, w, r) }))
	mux.Handle("/mcp", requireToken(o.Token, o.MCP))
	for pattern, h := range o.Routes {
		mux.Handle(pattern, requireToken(o.Token, h))
	}
	return guard(mux)
}

func getNode(t *tree.Tree, w http.ResponseWriter, r *http.Request) {
	by, err := actor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	addr := tree.Addr(r.URL.Query().Get("addr"))
	if addr == "" {
		addr = "app"
	}
	depth := 1
	if d := r.URL.Query().Get("depth"); d != "" {
		if depth, err = strconv.Atoi(d); err != nil || depth < 0 || depth > 2 {
			writeError(w, http.StatusBadRequest, errors.New("depth must be 0, 1 or 2"))
			return
		}
	}
	n, err := t.Get(r.Context(), by, addr, depth)
	switch {
	case errors.Is(err, tree.ErrNotFound):
		writeError(w, http.StatusNotFound, err)
	case err != nil:
		writeError(w, http.StatusBadRequest, err)
	default:
		writeJSON(w, http.StatusOK, n)
	}
}

func do(t *tree.Tree, w http.ResponseWriter, r *http.Request) {
	by, err := actor(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Ops []tree.Op `json:"ops"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": t.Do(r.Context(), by, body.Ops)})
}

func events(hub *Hub, w http.ResponseWriter, r *http.Request) {
	ch, cancel := hub.Subscribe()
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	rc := http.NewResponseController(w)
	fmt.Fprint(w, ": connected\n\n")
	rc.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(e.Data)
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Name, data)
			rc.Flush()
		}
	}
}

// actor reads X-Layer8-Actor: empty or "user", "cli", or "plugin:<name>".
func actor(r *http.Request) (tree.Actor, error) {
	h := r.Header.Get("X-Layer8-Actor")
	a := tree.ParseActor(h)
	switch {
	case h == "":
		return tree.Actor{Kind: "user"}, nil
	case (a.Kind == "user" || a.Kind == "cli") && a.ID == "", a.Kind == "plugin" && a.ID != "":
		return a, nil
	}
	return tree.Actor{}, fmt.Errorf("X-Layer8-Actor %q is not allowed", h)
}

var loopbackHosts = []string{"127.0.0.1", "localhost", "::1"}

var appOrigins = []string{"tauri://localhost", "http://tauri.localhost", "http://localhost:5173"}

// guard rejects non-loopback hosts and foreign origins, and answers CORS for the app's origins.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		if !slices.Contains(loopbackHosts, host) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != "http://"+r.Host && !slices.Contains(appOrigins, origin) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Layer8-Actor, Mcp-Session-Id, Mcp-Protocol-Version")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireToken accepts "Authorization: Bearer <token>" or "?token=<token>".
func requireToken(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			got = r.URL.Query().Get("token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
