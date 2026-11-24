// Package client is a typed HTTP client of the layer8 daemon.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	req, err := http.NewRequest(method, c.base+path, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		var e struct{ Error string }
		json.NewDecoder(res.Body).Decode(&e)
		return fmt.Errorf("%s %s: %s", method, path, e.Error)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
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

func (c *Client) CreateTask(in task.CreateInput) (store.Task, error) {
	var t store.Task
	return t, c.do(http.MethodPost, "/api/tasks", in, &t)
}

func (c *Client) SetTaskStatus(idOrSlug, status string) (store.Task, error) {
	var t store.Task
	return t, c.do(http.MethodPut, "/api/tasks/"+url.PathEscape(idOrSlug), map[string]string{"status": status}, &t)
}
