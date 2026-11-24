// Package client is a typed HTTP client of the layer8 daemon.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

// Home is layer8's state folder: $LAYER8_HOME, else ~/.layer8.
func Home() string {
	if dir := os.Getenv("LAYER8_HOME"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".layer8")
}

// Addr is the daemon's listen address: $LAYER8_ADDR, else 127.0.0.1:7878.
func Addr() string {
	if addr := os.Getenv("LAYER8_ADDR"); addr != "" {
		return addr
	}
	return "127.0.0.1:7878"
}

// TokenPath is where the daemon keeps its API token.
func TokenPath(home string) string { return filepath.Join(home, "token") }

type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a client of the daemon at base (e.g. http://127.0.0.1:7878).
func New(base, token string) *Client {
	return &Client{base: strings.TrimSuffix(base, "/"), token: token, http: &http.Client{Timeout: 5 * time.Minute}}
}

// FromEnv returns a client of the local daemon, reading its token from Home().
func FromEnv() (*Client, error) {
	token, err := os.ReadFile(TokenPath(Home()))
	if err != nil {
		return nil, fmt.Errorf("layer8 daemon token: %w (is the daemon running?)", err)
	}
	return New("http://"+Addr(), strings.TrimSpace(string(token))), nil
}

func (c *Client) do(method, path string, in, out any) error {
	var body bytes.Buffer
	if in != nil {
		if err := json.NewEncoder(&body).Encode(in); err != nil {
			return err
		}
	}
	res, err := c.request(method, path, "application/json", &body)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

// request sends body to the daemon and turns an error status into an error.
func (c *Client) request(method, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", contentType)
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		defer res.Body.Close()
		e := struct{ Error string }{res.Status}
		json.NewDecoder(res.Body).Decode(&e)
		return nil, fmt.Errorf("%s %s: %s", method, path, e.Error)
	}
	return res, nil
}

func (c *Client) Version() (string, error) {
	var v struct{ Version string }
	err := c.do(http.MethodGet, "/api/version", nil, &v)
	return v.Version, err
}

func (c *Client) Projects() ([]store.Project, error) {
	var ps []store.Project
	return ps, c.do(http.MethodGet, "/api/projects", nil, &ps)
}

func (c *Client) AddProject(path string) (store.Project, error) {
	var p store.Project
	return p, c.do(http.MethodPost, "/api/projects", map[string]string{"path": path}, &p)
}

func (c *Client) Tasks(f store.TaskFilter) ([]store.Task, error) {
	q := url.Values{}
	if f.ProjectID != "" {
		q.Set("project", f.ProjectID)
	}
	if f.ParentID != "" {
		q.Set("parent", f.ParentID)
	}
	if f.Status != "" {
		q.Set("status", f.Status)
	}
	var ts []store.Task
	return ts, c.do(http.MethodGet, "/api/tasks?"+q.Encode(), nil, &ts)
}

// Task fetches a task by id or slug.
func (c *Client) Task(idOrSlug string) (task.Detail, error) {
	var t task.Detail
	return t, c.do(http.MethodGet, "/api/tasks/"+url.PathEscape(idOrSlug), nil, &t)
}

// CreateTask creates a task or sub-task, with its first session when in.Start is set.
func (c *Client) CreateTask(in task.CreateInput) (task.Created, error) {
	var t task.Created
	return t, c.do(http.MethodPost, "/api/tasks", in, &t)
}

func (c *Client) SetTaskStatus(idOrSlug, status string) (store.Task, error) {
	var t store.Task
	return t, c.do(http.MethodPut, "/api/tasks/"+url.PathEscape(idOrSlug), map[string]string{"status": status}, &t)
}

// Sessions lists a task's sessions.
func (c *Client) Sessions(taskRef string) ([]session.View, error) {
	var ss []session.View
	return ss, c.do(http.MethodGet, "/api/tasks/"+url.PathEscape(taskRef)+"/sessions", nil, &ss)
}

func (c *Client) StartSession(taskRef string, in session.StartInput) (store.Session, error) {
	var s store.Session
	return s, c.do(http.MethodPost, "/api/tasks/"+url.PathEscape(taskRef)+"/sessions", in, &s)
}

// Events returns a session's events after seq.
func (c *Client) Events(sessionID string, after int) ([]store.Event, error) {
	var es []store.Event
	return es, c.do(http.MethodGet, fmt.Sprintf("/api/sessions/%s/events?after=%d", url.PathEscape(sessionID), after), nil, &es)
}

// Send messages a session and reports whether the message was queued behind a running turn.
func (c *Client) Send(sessionID, text string) (bool, error) {
	var r struct{ Queued bool }
	return r.Queued, c.do(http.MethodPost, "/api/sessions/"+url.PathEscape(sessionID)+"/messages", map[string]string{"text": text}, &r)
}

func (c *Client) Interrupt(sessionID string) error {
	return c.do(http.MethodPost, "/api/sessions/"+url.PathEscape(sessionID)+"/interrupt", nil, nil)
}

func (c *Client) Stop(sessionID string) error {
	return c.do(http.MethodPost, "/api/sessions/"+url.PathEscape(sessionID)+"/stop", nil, nil)
}

// Decide answers an approval with allow, always or deny.
func (c *Client) Decide(approvalID string, d session.Decision) error {
	return c.do(http.MethodPost, "/api/approvals/"+url.PathEscape(approvalID), d, nil)
}

// Artifacts lists a task's artifacts, newest first.
func (c *Client) Artifacts(taskRef string) ([]task.Artifact, error) {
	var as []task.Artifact
	return as, c.do(http.MethodGet, "/api/tasks/"+url.PathEscape(taskRef)+"/artifacts", nil, &as)
}

// ReadArtifact returns the content of a task's artifact.
func (c *Client) ReadArtifact(taskRef, path string) ([]byte, error) {
	res, err := c.request(http.MethodGet, artifactPath(taskRef, path), "", nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	return io.ReadAll(res.Body)
}

// WriteArtifact creates or replaces a task's artifact with data.
func (c *Client) WriteArtifact(taskRef, path string, data io.Reader) (task.Artifact, error) {
	var a task.Artifact
	res, err := c.request(http.MethodPut, artifactPath(taskRef, path), "application/octet-stream", data)
	if err != nil {
		return a, err
	}
	defer res.Body.Close()
	return a, json.NewDecoder(res.Body).Decode(&a)
}

func artifactPath(taskRef, path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return "/api/tasks/" + url.PathEscape(taskRef) + "/artifacts/" + strings.Join(segments, "/")
}

// Attention returns how many sessions wait for an approval.
func (c *Client) Attention() (int, error) {
	var r struct{ Waiting int }
	return r.Waiting, c.do(http.MethodGet, "/api/attention", nil, &r)
}

// Event is one message of the daemon's SSE stream.
type Event struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	TaskID string `json:"taskId"`
	Status string `json:"status"`
	Label  string `json:"label"`
}

// Subscribe calls fn with every event the daemon publishes until ctx is done or the stream ends.
func (c *Client) Subscribe(ctx context.Context, fn func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/events", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("GET /api/events: %s", res.Status)
	}
	lines := bufio.NewScanner(res.Body)
	lines.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for lines.Scan() {
		data, ok := strings.CutPrefix(lines.Text(), "data: ")
		if !ok {
			continue
		}
		var e Event
		if json.Unmarshal([]byte(data), &e) == nil {
			fn(e)
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return lines.Err()
}
