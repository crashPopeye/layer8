//go:build e2e

// Package e2e runs the cut-over flow against a real layer8d and the real claude CLI: make e2e.
package e2e

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type daemon struct {
	url, token string
}

type result struct {
	OK      bool
	Addr    string
	Message string
}

type node struct {
	Addr     string
	Fields   map[string]any
	Children []struct{ Addr, Kind, Title, Status string }
}

func (d daemon) do(t *testing.T, target, action string, args any) result {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"ops": []any{map[string]any{"target": target, "action": action, "args": args}}})
	req, _ := http.NewRequest("POST", d.url+"/api/do", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+d.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out struct{ Results []result }
	json.NewDecoder(res.Body).Decode(&out)
	if len(out.Results) != 1 || !out.Results[0].OK {
		t.Fatalf("%s %s: %+v", target, action, out.Results)
	}
	return out.Results[0]
}

func (d daemon) get(t *testing.T, addr string) node {
	t.Helper()
	req, _ := http.NewRequest("GET", d.url+"/api/node?addr="+addr, nil)
	req.Header.Set("Authorization", "Bearer "+d.token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var n node
	if err := json.NewDecoder(res.Body).Decode(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// fakeGitHub answers the pull request calls open-pr and the status poll make, and records the pull requests opened.
func fakeGitHub(t *testing.T) (*httptest.Server, func() []map[string]any) {
	var mu sync.Mutex
	var opened []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/e2e/demo/pulls", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		list := []map[string]int{}
		for i := range opened {
			list = append(list, map[string]int{"number": i + 1})
		}
		json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("POST /repos/e2e/demo/pulls", func(w http.ResponseWriter, r *http.Request) {
		var pr map[string]any
		json.NewDecoder(r.Body).Decode(&pr)
		mu.Lock()
		opened = append(opened, pr)
		n := len(opened)
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, `{"number":%d}`, n)
	})
	mux.HandleFunc("POST /graphql", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":{"repository":{"pullRequest":{"number":1,"url":"https://github.com/e2e/demo/pull/1","state":"OPEN",
			"baseRefName":"main","mergeable":"MERGEABLE","reviewThreads":{"nodes":[]},"commits":{"nodes":[]}}}}}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Logf("fake github: unexpected %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	})
	gh := httptest.NewServer(mux)
	t.Cleanup(gh.Close)
	return gh, func() []map[string]any {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]any(nil), opened...)
	}
}

// demoRepo creates a repository whose origin is a github.com URL that git rewrites to a local bare repository.
func demoRepo(t *testing.T, root string) (repo, bare string) {
	repo, bare = filepath.Join(root, "demo"), filepath.Join(root, "demo.git")
	git(t, root, "init", "-q", "--bare", "-b", "main", bare)
	git(t, root, "init", "-q", "-b", "main", repo)
	git(t, repo, "config", "user.name", "layer8 e2e")
	git(t, repo, "config", "user.email", "e2e@layer8.invalid")
	git(t, repo, "config", "url."+bare+".insteadOf", "https://github.com/e2e/demo.git")
	git(t, repo, "remote", "add", "origin", "https://github.com/e2e/demo.git")
	os.MkdirAll(filepath.Join(repo, ".claude"), 0o755)
	os.WriteFile(filepath.Join(repo, ".claude", "settings.json"), []byte(`{"model":"haiku","permissions":{"allow":["Bash","Edit","Write","Read","Glob","Grep","Skill","mcp__layer8"]}}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "hello.txt"), []byte("hello\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "README.md"), []byte("# demo\n\nA demo repository for the layer8 end-to-end check.\n"), 0o644)
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "Initial commit")
	git(t, repo, "push", "-q", "origin", "main")
	git(t, repo, "fetch", "-q", "origin")
	return repo, bare
}

func freeAddr(t *testing.T) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func startDaemon(t *testing.T, root, githubAPI string) (daemon, string, string) {
	bin := filepath.Join(root, "layer8d")
	if out, err := exec.Command("go", "build", "-o", bin, "../cmd/layer8d").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := filepath.Join(root, "home")
	os.MkdirAll(home, 0o700)
	addr := freeAddr(t)
	cfg, _ := json.Marshal(map[string]any{"addr": addr, "githubApi": githubAPI, "githubToken": "e2e", "autoApproveDo": true, "maxSessions": 2})
	os.WriteFile(filepath.Join(home, "config.json"), cfg, 0o600)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "serve")
	cmd.Env = append(os.Environ(), "LAYER8_HOME="+home)
	cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
	cmd.WaitDelay = 10 * time.Second
	logFile, _ := os.Create(filepath.Join(root, "daemon.log"))
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		cmd.Wait()
		logFile.Close()
		if t.Failed() {
			log, _ := os.ReadFile(logFile.Name())
			t.Logf("daemon log:\n%s", log)
		}
	})
	url := "http://" + addr
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if res, err := http.Get(url + "/api/version"); err == nil {
			res.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("daemon did not start")
		}
	}
	token, _ := os.ReadFile(filepath.Join(home, "token"))
	return daemon{url: url, token: strings.TrimSpace(string(token))}, home, bin
}

func TestCutOver(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude is not installed")
	}
	root := t.TempDir()
	gh, opened := fakeGitHub(t)
	repo, bare := demoRepo(t, root)
	d, home, bin := startDaemon(t, root, gh.URL)
	began := time.Now()

	project := d.do(t, "app", "add-project", map[string]string{"path": repo}).Addr
	task := d.do(t, project, "create-task", map[string]string{"title": "Greet layer8"}).Addr
	tn := d.get(t, task)
	artifacts, worktree, branch := tn.Fields["artifacts"].(string), tn.Fields["worktree"].(string), tn.Fields["branch"].(string)
	os.WriteFile(filepath.Join(artifacts, "01-ticket.md"), []byte("# Greet layer8\n\n"+
		"Change the single line of `hello.txt` from `hello` to `hello, layer8`. Change nothing else; there are no tests to run.\n"), 0o644)
	d.do(t, task, "run-workflow", map[string]any{"workflow": "quick-fix", "autopilot": true})

	var wf map[string]any
	approvals := 0
	for deadline := time.Now().Add(20 * time.Minute); ; time.Sleep(3 * time.Second) {
		tn = d.get(t, task)
		wf, _ = tn.Fields["workflow"].(map[string]any)
		status, _ := wf["status"].(string)
		var sessions []string
		for _, c := range tn.Children {
			if c.Kind == "session" {
				sessions = append(sessions, c.Addr+"="+c.Status)
			}
			if c.Kind == "session" && c.Status == "waiting" {
				d.do(t, c.Addr, "allow", map[string]bool{"always": true})
				approvals++
			}
		}
		t.Logf("%s workflow %s step %v %v", time.Since(began).Round(time.Second), status, wf["step"], sessions)
		if status != "running" && status != "waiting" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("workflow still %s after 20 min", status)
		}
	}
	if wf["status"] != "done" {
		t.Fatalf("workflow ended %v: %v", wf["status"], wf["progress"])
	}

	if got := git(t, worktree, "show", branch+":hello.txt"); got != "hello, layer8" {
		t.Errorf("hello.txt on %s = %q", branch, got)
	}
	if ahead := git(t, worktree, "rev-list", "--count", "origin/main.."+branch); ahead == "0" {
		t.Errorf("%s has no commit over main", branch)
	}
	if pushed := git(t, bare, "rev-parse", "refs/heads/"+branch); pushed != git(t, worktree, "rev-parse", branch) {
		t.Errorf("bare remote has %s at %s", branch, pushed)
	}
	description, err := os.ReadFile(filepath.Join(artifacts, "pr-description.md"))
	if err != nil || !strings.HasPrefix(string(description), "# ") {
		t.Errorf("pr-description.md: %v %q", err, description)
	}
	prs := opened()
	if len(prs) != 1 || prs[0]["head"] != branch || prs[0]["base"] != "main" || prs[0]["body"] == "" {
		t.Errorf("pull requests opened: %+v", prs)
	}
	if pr, _ := tn.Fields["pr"].(map[string]any); pr == nil || pr["number"] != float64(1) {
		t.Errorf("task pr field = %v", tn.Fields["pr"])
	}

	db, err := sql.Open("sqlite", "file:"+filepath.Join(home, "layer8.db")+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT actor_kind, action, count(*) FROM actions_log WHERE ok GROUP BY 1, 2 ORDER BY 1, 2`)
	if err != nil {
		t.Fatal(err)
	}
	bySession := map[string]bool{}
	for rows.Next() {
		var kind, action string
		var n int
		rows.Scan(&kind, &action, &n)
		t.Logf("actions_log: %-8s %-14s ×%d", kind, action, n)
		if kind == "session" {
			bySession[action] = true
		}
	}
	rows.Close()
	for _, action := range []string{"advance", "open-pr"} {
		if !bySession[action] {
			t.Errorf("no %s by a session in actions_log", action)
		}
	}
	var cost float64
	db.QueryRow(`SELECT coalesce(sum(cost_usd), 0) FROM sessions`).Scan(&cost)

	cli := exec.Command(bin, "get", task)
	cli.Env = append(os.Environ(), "LAYER8_HOME="+home)
	out, err := cli.CombinedOutput()
	if err != nil || !strings.Contains(string(out), branch) {
		t.Errorf("layer8 get %s: %v\n%s", task, err, out)
	}
	t.Logf("cut-over flow done in %s, %d approvals answered by the user, sessions cost $%.3f\n%s\npr-description.md:\n%s",
		time.Since(began).Round(time.Second), approvals, cost, out, description)
}
