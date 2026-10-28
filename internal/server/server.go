// Package server exposes the core API to the desktop shell, the CLI and (for
// development) a browser, over HTTP on 127.0.0.1 with a bearer token.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Origins of the Tauri webview on macOS (tauri://) and other platforms.
var tauriOrigins = map[string]bool{
	"tauri://localhost":       true,
	"http://tauri.localhost":  true,
	"https://tauri.localhost": true,
}

type Server struct {
	token string
	port  int
	api   http.Handler
	ui    fs.FS
	Hub   *Hub
}

func New(token string, port int, api http.Handler, ui fs.FS, hub *Hub) *Server {
	return &Server{token: token, port: port, api: api, ui: ui, Hub: hub}
}

func isLocalHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// originAllowed: no Origin (CLI, curl), the Tauri webview, or this server itself (browser dev).
func (s *Server) originAllowed(origin string) bool {
	if origin == "" || tauriOrigins[origin] {
		return true
	}
	for _, h := range []string{"127.0.0.1", "localhost"} {
		if origin == fmt.Sprintf("http://%s:%d", h, s.port) {
			return true
		}
	}
	return false
}

func (s *Server) tokenOK(r *http.Request) bool {
	given := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if given == "" {
		// EventSource can't set headers; it passes the token in the query instead.
		given = r.URL.Query().Get("token")
	}
	return given != "" && subtle.ConstantTimeCompare([]byte(given), []byte(s.token)) == 1
}

func jsonError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Handler: static UI at / (public: it contains no data), everything under /api/
// behind the token. Foreign Host headers are refused to block DNS rebinding,
// foreign Origins to block other web pages.
func (s *Server) Handler() http.Handler {
	static := http.FileServerFS(s.ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalHost(r.Host) {
			jsonError(w, http.StatusForbidden, "forbidden host")
			return
		}
		origin := r.Header.Get("Origin")
		if !s.originAllowed(origin) {
			jsonError(w, http.StatusForbidden, "forbidden origin")
			return
		}
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			static.ServeHTTP(w, r)
			return
		}
		if !s.tokenOK(r) {
			jsonError(w, http.StatusUnauthorized, "bad or missing token")
			return
		}
		if r.URL.Path == "/api/events" {
			s.Hub.serve(w, r)
			return
		}
		s.api.ServeHTTP(w, r)
	})
}

// ---- server-sent events

type Hub struct {
	mu      sync.Mutex
	clients map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{clients: map[chan []byte]struct{}{}} }

// Emit sends a named event to every connected client. Slow clients are dropped
// rather than allowed to block the daemon; they reconnect and refetch state.
func (h *Hub) Emit(name string, data any) {
	b, err := json.Marshal(data)
	if err != nil {
		log.Printf("sse marshal %s: %v", name, err)
		return
	}
	msg := []byte(fmt.Sprintf("event: %s\ndata: %s\n\n", name, b))
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c <- msg:
		default:
			delete(h.clients, c)
			close(c)
		}
	}
}

func (h *Hub) serve(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		jsonError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	c := make(chan []byte, 256)
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if _, ok := h.clients[c]; ok {
			delete(h.clients, c)
			close(c)
		}
		h.mu.Unlock()
	}()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-c:
			if !ok {
				return
			}
			if _, err := w.Write(msg); err != nil {
				return
			}
			flusher.Flush()
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
