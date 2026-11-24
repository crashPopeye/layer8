package api_test

import (
	"bufio"
	"context"
	"encoding/json"
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
	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

const token = "s3cret"

// echoProc is a claude process that answers every message with its text and a result.
type echoProc struct{ h claude.Handlers }

func (p echoProc) Send(text string) error {
	go func() {
		p.h.OnMessage(claude.Message{Type: "system", Subtype: "init", SessionID: "claude-1"})
		p.h.OnMessage(claude.Message{Type: "assistant", Message: &claude.APIMessage{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"echo: ` + text + `"}]`)}})
		p.h.OnMessage(claude.Message{Type: "result", TotalCostUSD: 0.5})
	}()
	return nil
}
func (echoProc) Interrupt() error               { return nil }
func (echoProc) SetModel(string) error          { return nil }
func (echoProc) SetPermissionMode(string) error { return nil }
func (p echoProc) Close() error                 { p.h.OnExit(nil); return nil }

func echoSpawner(o claude.Options, h claude.Handlers) (session.Proc, error) { return echoProc{h}, nil }

func handler(t *testing.T) http.Handler {
	t.Helper()
	home := t.TempDir()
	st, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	hub := api.NewHub()
	sessions := session.NewManager(st, hub.Publish, echoSpawner, session.Config{ArtifactsRoot: filepath.Join(home, "artifacts")})
	tasks := task.NewService(st, gitx.Git{}, home, hub.Notify, sessions)
	ui := fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}
	return api.New(st, hub, tasks, sessions, token, ui)
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

func TestSessionThroughClient(t *testing.T) {
	srv := httptest.NewServer(handler(t))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, token)
	p, err := c.AddProject(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateTask(task.CreateInput{ProjectID: p.ID, Title: "Smoke task"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.StartSession(created.Slug, session.StartInput{Label: "research", Prompt: "hi"})
	if err != nil || s.Label != "research" || s.TaskID != created.ID {
		t.Fatalf("session = %+v, %v", s, err)
	}

	var events []store.Event
	deadline := time.Now().Add(5 * time.Second)
	for len(events) < 3 && time.Now().Before(deadline) {
		events, _ = c.Events(s.ID, 0)
		time.Sleep(10 * time.Millisecond)
	}
	var kinds []string
	for _, e := range events {
		kinds = append(kinds, e.Kind)
	}
	if strings.Join(kinds, ",") != "user,text,result" || !strings.Contains(string(events[1].Body), "echo: hi") {
		t.Fatalf("events = %v", kinds)
	}
	listed, err := c.Sessions(created.ID)
	if err != nil || len(listed) != 1 || listed[0].Status != "ready_for_input" || listed[0].CostUSD != 0.5 {
		t.Fatalf("sessions = %+v, %v", listed, err)
	}
	var active []store.Session
	get(t, srv.URL+"/api/sessions", &active)
	if len(active) != 1 || active[0].ID != s.ID {
		t.Fatalf("active task sessions = %+v", active)
	}
	if err := c.Stop(s.ID); err != nil {
		t.Fatal(err)
	}
	if err := c.Decide("nope", session.Decision{Decision: "allow"}); err == nil {
		t.Fatal("deciding an unknown approval succeeded")
	}
}

func TestPhasesThroughClient(t *testing.T) {
	srv := httptest.NewServer(handler(t))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, token)
	p, err := c.AddProject(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateTask(task.CreateInput{ProjectID: p.ID, Title: "Make lint", Workflow: "prd_tdd"})
	if err != nil {
		t.Fatal(err)
	}
	if d, err := c.Task(created.ID); err != nil || d.Next != "research-questions" || len(d.Phases) != 7 || d.Phases[2].Skill != "/l8:create-prd" {
		t.Fatalf("progress = %+v, %v", d.Progress, err)
	}
	s, err := c.NextPhase(created.Slug)
	if err != nil || s.Label != "research-questions" {
		t.Fatalf("next phase session = %+v, %v", s, err)
	}
	on := true
	updated, err := c.UpdateTask(created.ID, task.TaskUpdate{AutoAdvance: &on})
	if err != nil || !updated.AutoAdvance || updated.Phase != "research-questions" {
		t.Fatalf("updated = %+v, %v", updated, err)
	}
	bad := "deploy"
	if _, err := c.UpdateTask(created.ID, task.TaskUpdate{Phase: &bad}); err == nil || !strings.Contains(err.Error(), "no phase") {
		t.Fatalf("unknown phase err = %v", err)
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

func TestSubscribeAndAttention(t *testing.T) {
	srv := httptest.NewServer(handler(t))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, token)
	if n, err := c.Attention(); err != nil || n != 0 {
		t.Fatalf("attention = %d, %v", n, err)
	}
	p, err := c.AddProject(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateTask(task.CreateInput{ProjectID: p.ID, Title: "Smoke task"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	events := make(chan client.Event, 64)
	go c.Subscribe(ctx, func(e client.Event) { events <- e })
	deadline := time.After(5 * time.Second)
	for {
		if _, err := c.SetTaskStatus(created.ID, "active"); err != nil {
			t.Fatal(err)
		}
		select {
		case e := <-events:
			if e.Kind != "task" || e.ID != created.ID {
				t.Fatalf("event = %+v", e)
			}
			return
		case <-deadline:
			t.Fatal("no event through Subscribe")
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestArtifactsThroughClient(t *testing.T) {
	srv := httptest.NewServer(handler(t))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, token)
	p, err := c.AddProject(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateTask(task.CreateInput{ProjectID: p.ID, Title: "Smoke task"})
	if err != nil {
		t.Fatal(err)
	}
	prd := "---\ntype: design-prd\n---\n# PRD\n\n```task-artifact\nmockup.html\n```\n"
	if _, err := c.WriteArtifact(created.Slug, "03-prd.md", strings.NewReader(prd)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteArtifact(created.Slug, "mockup.html", strings.NewReader("<h1>mockup</h1>")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WriteArtifact(created.Slug, "../escape.md", strings.NewReader("x")); err == nil {
		t.Fatal("wrote outside the artifacts folder")
	}
	if got, err := c.ReadArtifact(created.ID, "03-prd.md"); err != nil || string(got) != prd {
		t.Fatalf("read = %q, %v", got, err)
	}

	var listed []struct{ Path, Type, URL string }
	get(t, srv.URL+"/api/tasks/"+created.ID+"/artifacts", &listed)
	types := map[string]string{}
	var mockupURL string
	for _, a := range listed {
		types[a.Path] = a.Type
		if a.Path == "mockup.html" {
			mockupURL = a.URL
		}
	}
	if types["03-prd.md"] != "design-prd" || types["mockup.html"] != "html" || types["task.md"] != "task" {
		t.Fatalf("artifacts = %+v", listed)
	}

	var rendered struct{ HTML string }
	get(t, srv.URL+"/api/tasks/"+created.Slug+"/render/03-prd.md", &rendered)
	if !strings.Contains(rendered.HTML, `<h1 data-line="4">PRD</h1>`) || !strings.Contains(rendered.HTML, `src="`+mockupURL+`"`) {
		t.Fatalf("rendered = %s (mockup at %s)", rendered.HTML, mockupURL)
	}

	res := fetch(t, srv.URL+mockupURL, "null")
	if res.StatusCode != http.StatusOK || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox allow-scripts") ||
		strings.Contains(res.Header.Get("Content-Security-Policy"), "allow-same-origin") || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("shared artifact: %d %v", res.StatusCode, res.Header)
	}
	forged := strings.Replace(mockupURL, "/"+created.ID+"/", "/"+created.ID+"/0000/", 1)
	if res := fetch(t, srv.URL+forged, ""); res.StatusCode != http.StatusNotFound {
		t.Fatalf("forged key: %d", res.StatusCode)
	}
	if res := fetch(t, srv.URL+"/api/tasks/"+created.ID+"/artifacts/mockup.html", "null"); res.StatusCode != http.StatusForbidden {
		t.Fatalf("API from a sandboxed origin: %d", res.StatusCode)
	}
}

func TestCommentsReachTheSession(t *testing.T) {
	srv := httptest.NewServer(handler(t))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, token)
	p, err := c.AddProject(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateTask(task.CreateInput{ProjectID: p.ID, Title: "Smoke task"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := c.StartSession(created.ID, session.StartInput{Label: "prd"})
	if err != nil {
		t.Fatal(err)
	}
	var comment store.Comment
	post(t, srv.URL+"/api/tasks/"+created.ID+"/comments", task.CommentInput{Path: "03-prd.md", Line: 12, Quote: "Sessions survive restarts.", Body: "How long does a resume take?"}, &comment)
	var sent struct{ Sent int }
	post(t, srv.URL+"/api/tasks/"+created.ID+"/comments/send", task.SendInput{SessionID: s.ID, Resolve: true}, &sent)
	if sent.Sent != 1 {
		t.Fatalf("sent = %d", sent.Sent)
	}
	events, err := c.Events(s.ID, 0)
	if err != nil || len(events) == 0 || events[0].Kind != "user" || !strings.Contains(string(events[0].Body), "03-prd.md:12") {
		t.Fatalf("events = %+v, %v", events, err)
	}
	var comments []store.Comment
	get(t, srv.URL+"/api/tasks/"+created.ID+"/comments", &comments)
	if len(comments) != 1 || comments[0].Status != "resolved" {
		t.Fatalf("comments = %+v", comments)
	}
}

func fetch(t *testing.T, u, origin string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func get(t *testing.T, u string, out any) {
	t.Helper()
	call(t, http.MethodGet, u, nil, out)
}

func post(t *testing.T, u string, in, out any) {
	t.Helper()
	call(t, http.MethodPost, u, in, out)
}

func call(t *testing.T, method, u string, in, out any) {
	t.Helper()
	var body strings.Builder
	if in != nil {
		json.NewEncoder(&body).Encode(in)
	}
	req, _ := http.NewRequest(method, u, strings.NewReader(body.String()))
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%s %s: %s", method, u, res.Status)
	}
	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}

func TestChangesAndCommit(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "gitconfig"))
	t.Setenv("GIT_AUTHOR_NAME", "t")
	t.Setenv("GIT_AUTHOR_EMAIL", "t@t")
	t.Setenv("GIT_COMMITTER_NAME", "t")
	t.Setenv("GIT_COMMITTER_EMAIL", "t@t")
	srv := httptest.NewServer(handler(t))
	t.Cleanup(srv.Close)
	c := client.New(srv.URL, token)
	p, err := c.AddProject(repo(t))
	if err != nil {
		t.Fatal(err)
	}
	created, err := c.CreateTask(task.CreateInput{ProjectID: p.ID, Title: "Notes"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(created.Worktree, "notes.md"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	base := srv.URL + "/api/tasks/" + created.ID
	var changes gitx.Changes
	get(t, base+"/changes", &changes)
	if len(changes.Files) != 1 || changes.Files[0].NewPath != "notes.md" || changes.Files[0].Adds != 2 || changes.Base != "origin/main" {
		t.Fatalf("changes = %+v", changes)
	}
	var committed struct{ SHA string }
	post(t, base+"/commit", map[string]string{"message": "Add notes"}, &committed)
	if len(committed.SHA) != 40 {
		t.Fatalf("commit = %+v", committed)
	}
	var info task.PRInfo
	get(t, base+"/pr", &info)
	if info.PR != nil || info.Head != "l8/notes" || info.Target != "main" || !info.Unpushed || info.Dirty || info.Title != "Notes" {
		t.Fatalf("pr info = %+v", info)
	}
}
