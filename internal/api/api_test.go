package api_test

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/crashPopeye/layer8/internal/api"
	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

const token = "s3cret"

func handler(t *testing.T) http.Handler {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := api.NewHub()
	tasks := task.NewService(st, gitx.Git{}, home, hub.Notify)
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	return api.New(st, hub, tasks, token, ui)
}

func TestGuard(t *testing.T) {
	h := handler(t)
	cases := []struct {
		name   string
		host   string
		origin string
		auth   string
		cookie string
		want   int
	}{
		{"bearer", "127.0.0.1:7878", "", "Bearer " + token, "", http.StatusOK},
		{"cookie", "127.0.0.1:7878", "http://127.0.0.1:7878", "", token, http.StatusOK},
		{"missing token", "127.0.0.1:7878", "", "", "", http.StatusUnauthorized},
		{"wrong token", "localhost:7878", "", "Bearer nope", "", http.StatusUnauthorized},
		{"foreign origin", "127.0.0.1:7878", "https://evil.example", "Bearer " + token, "", http.StatusForbidden},
		{"sandboxed iframe", "127.0.0.1:7878", "null", "", token, http.StatusForbidden},
		{"other local port", "127.0.0.1:7878", "http://127.0.0.1:3000", "", token, http.StatusForbidden},
		{"rebound host", "evil.example:7878", "", "Bearer " + token, "", http.StatusForbidden},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
			r.Host = c.host
			if c.origin != "" {
				r.Header.Set("Origin", c.origin)
			}
			if c.auth != "" {
				r.Header.Set("Authorization", c.auth)
			}
			if c.cookie != "" {
				r.AddCookie(&http.Cookie{Name: api.CookieName, Value: c.cookie})
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.want {
				t.Fatalf("status = %d, want %d: %s", w.Code, c.want, w.Body)
			}
		})
	}
}

func TestStaticUINeedsNoToken(t *testing.T) {
	w := httptest.NewRecorder()
	handler(t).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7878/", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "doctype") {
		t.Fatalf("status = %d: %s", w.Code, w.Body)
	}
}

func TestAuthSetsCookie(t *testing.T) {
	h := handler(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7878/auth?t="+token, nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("status = %d, location = %q", w.Code, w.Header().Get("Location"))
	}
	cookie := w.Header().Get("Set-Cookie")
	for _, want := range []string{api.CookieName + "=" + token, "HttpOnly", "SameSite=Strict", "Path=/"} {
		if !strings.Contains(cookie, want) {
			t.Fatalf("Set-Cookie %q lacks %q", cookie, want)
		}
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7878/auth?t=nope", nil))
	if w.Code != http.StatusUnauthorized || w.Header().Get("Set-Cookie") != "" {
		t.Fatalf("bad token: status = %d, cookie = %q", w.Code, w.Header().Get("Set-Cookie"))
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	upstream := filepath.Join(root, "upstream.git")
	git(t, root, "init", "-q", "--bare", "-b", "main", upstream)
	seed := filepath.Join(root, "seed")
	git(t, root, "clone", "-q", upstream, seed)
	if err := os.WriteFile(filepath.Join(seed, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, seed, "add", "-A")
	git(t, seed, "commit", "-qm", "init")
	git(t, seed, "push", "-q", "origin", "HEAD:main")
	clone := filepath.Join(root, "work")
	git(t, root, "clone", "-q", upstream, clone)
	return clone
}

func TestCreateTaskThroughClient(t *testing.T) {
	srv := httptest.NewServer(handler(t))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, token)

	events := subscribe(t, srv.URL)

	p, err := c.AddProject(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateTask(task.CreateInput{ProjectID: p.ID, Title: "Smoke task", Workflow: "oneshot"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Slug != "smoke-task" || created.Branch != "l8/smoke-task" {
		t.Fatalf("task = %+v", created)
	}
	if _, err := os.Stat(filepath.Join(created.Worktree, task.LinkPath, "task.md")); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{`"kind":"project"`, `"kind":"task","id":"` + created.ID + `"`} {
		select {
		case got := <-events:
			if !strings.Contains(got, want) {
				t.Fatalf("event %q, want %s", got, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no event with %s", want)
		}
	}

	listed, err := c.Tasks(store.TaskFilter{ProjectID: p.ID, Status: "active"})
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("tasks = %+v, %v", listed, err)
	}
	done, err := c.SetTaskStatus("smoke-task", "done")
	if err != nil || done.Status != "done" {
		t.Fatalf("done = %+v, %v", done, err)
	}
	detail, err := c.Task(created.ID)
	if err != nil || detail.Status != "done" || !strings.HasPrefix(detail.TaskMD, "# Smoke task") {
		t.Fatalf("detail = %+v, %v", detail, err)
	}
	if _, err := c.CreateTask(task.CreateInput{ProjectID: p.ID}); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("untitled task err = %v", err)
	}
}

// subscribe streams the data lines of /api/events into a channel.
func subscribe(t *testing.T, base string) <-chan string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, base+"/api/events", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	lines := make(chan string, 16)
	scanner := bufio.NewScanner(res.Body)
	if !scanner.Scan() || scanner.Text() != ": connected" {
		t.Fatalf("stream did not open: %q", scanner.Text())
	}
	go func() {
		for scanner.Scan() {
			if data, ok := strings.CutPrefix(scanner.Text(), "data: "); ok {
				lines <- data
			}
		}
	}()
	return lines
}
