package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crashPopeye/layer8/internal/daemon"
	"github.com/crashPopeye/layer8/internal/store"
)

type state struct {
	Version    string            `json:"version"`
	Workspaces []store.Workspace `json:"workspaces"`
	Tasks      []store.Task      `json:"tasks"`
	Slices     []store.Slice     `json:"slices"`
	Sessions   []store.Session   `json:"sessions"`
	Approvals  []store.Approval  `json:"approvals"`
	Running    int               `json:"running"`
	Queued     int               `json:"queued"`
	MaxRunning int               `json:"maxRunning"`
}

type cli struct {
	ctx context.Context
	c   *daemon.Client
	st  *state
}

func (x *cli) do(method, path string, body, out any) error {
	return x.c.Do(x.ctx, method, path, body, out)
}

func (x *cli) state() (*state, error) {
	if x.st == nil {
		var s state
		if err := x.do(http.MethodGet, "/api/state", nil, &s); err != nil {
			return nil, err
		}
		x.st = &s
	}
	return x.st, nil
}

// flags pulls "-x value" options (and the boolean -f) out of args; the rest stays positional.
func flags(args []string, names ...string) (map[string]string, []string) {
	opts := map[string]string{}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-f" {
			opts["f"] = "1"
			continue
		}
		matched := false
		for _, n := range names {
			if n != "f" && a == "-"+n && i+1 < len(args) {
				opts[n] = args[i+1]
				i++
				matched = true
				break
			}
		}
		if !matched {
			rest = append(rest, a)
		}
	}
	return opts, rest
}

func (x *cli) task(ref string) (store.Task, error) {
	s, err := x.state()
	if err != nil {
		return store.Task{}, err
	}
	var hits []store.Task
	for _, t := range s.Tasks {
		if t.Status != "active" {
			continue
		}
		if strings.EqualFold(t.Slug, ref) || (t.Linear != nil && strings.EqualFold(t.Linear.Identifier, ref)) || strings.HasPrefix(t.ID, ref) {
			hits = append(hits, t)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return store.Task{}, fmt.Errorf("no active task %q (see layer8 tasks)", ref)
	default:
		return store.Task{}, fmt.Errorf("%q matches %d tasks; be more specific", ref, len(hits))
	}
}

func (x *cli) slice(t store.Task, key string) (string, error) {
	if key == "" {
		return "", nil
	}
	s, _ := x.state()
	for _, sl := range s.Slices {
		if sl.TaskID == t.ID && strings.EqualFold(sl.Key, key) {
			return sl.ID, nil
		}
	}
	return "", fmt.Errorf("task %s has no PR %q", t.Slug, key)
}

func (x *cli) session(ref string) (store.Session, error) {
	s, err := x.state()
	if err != nil {
		return store.Session{}, err
	}
	var hits []store.Session
	for _, se := range s.Sessions {
		if strings.HasPrefix(se.ID, ref) {
			hits = append(hits, se)
		}
	}
	if len(hits) != 1 {
		return store.Session{}, fmt.Errorf("%q matches %d sessions", ref, len(hits))
	}
	return hits[0], nil
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func need(args []string, n int, usage string) error {
	if len(args) < n {
		return errors.New("usage: layer8 " + usage)
	}
	return nil
}

func runCLI(args []string) error {
	info, err := daemon.LocalInfo()
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	x := &cli{ctx: ctx, c: daemon.NewClient(info)}
	if _, err := x.c.Version(ctx); err != nil {
		if args[0] == "install-cli" {
			return installCLI()
		}
		if _, err := daemon.Ensure(ctx, version); err != nil {
			return fmt.Errorf("can't reach layer8d: %w (open the layer8 app once)", err)
		}
	}
	cmd, args := args[0], args[1:]
	switch cmd {
	case "status":
		s, err := x.state()
		if err != nil {
			return err
		}
		fmt.Printf("layer8d %s · %d/%d agents working · %d queued · %d approvals waiting\n", s.Version, s.Running, s.MaxRunning, s.Queued, len(s.Approvals))
		return nil
	case "tasks":
		return x.tasks()
	case "workspaces":
		s, err := x.state()
		if err != nil {
			return err
		}
		for _, w := range s.Workspaces {
			kind := "folder"
			if w.IsGit {
				kind = "git · " + w.DefaultBranch
			}
			fmt.Printf("%-20s %-50s %s\n", w.Name, w.Path, kind)
		}
		return nil
	case "workspace":
		if err := need(args, 2, "workspace add PATH"); err != nil || args[0] != "add" {
			return errors.New("usage: layer8 workspace add PATH")
		}
		abs, err := filepath.Abs(args[1])
		if err != nil {
			return err
		}
		var w store.Workspace
		if err := x.do(http.MethodPost, "/api/workspaces", map[string]string{"path": abs}, &w); err != nil {
			return err
		}
		fmt.Printf("added %s (%s)\n", w.Name, w.Path)
		return nil
	case "new":
		o, rest := flags(args, "t", "w")
		s, err := x.state()
		if err != nil {
			return err
		}
		ws := ""
		for _, w := range s.Workspaces {
			if o["w"] == "" || strings.EqualFold(w.Name, o["w"]) {
				ws = w.ID
				break
			}
		}
		if ws == "" {
			return errors.New("no matching workspace (add one in the app first)")
		}
		body := map[string]string{"workspaceId": ws, "title": o["t"]}
		if len(rest) > 0 {
			body["linearIssue"] = rest[0]
		}
		var t store.Task
		if err := x.do(http.MethodPost, "/api/tasks", body, &t); err != nil {
			return err
		}
		fmt.Printf("created %s  %s\n  %s\n", t.Slug, t.Title, t.WorktreePath)
		return nil
	case "split":
		if err := need(args, 1, "split TASK"); err != nil {
			return err
		}
		t, err := x.task(args[0])
		if err != nil {
			return err
		}
		var slices []store.Slice
		if err := x.do(http.MethodPost, "/api/tasks/"+t.ID+"/slices", nil, &slices); err != nil {
			return err
		}
		for _, sl := range slices {
			fmt.Printf("%-6s %-40s %s\n", sl.Key, sl.Title, sl.Branch)
		}
		return nil
	case "sync":
		if len(args) == 0 {
			return x.do(http.MethodPost, "/api/sync", nil, nil)
		}
		t, err := x.task(args[0])
		if err != nil {
			return err
		}
		return x.do(http.MethodPost, "/api/tasks/"+t.ID+"/sync", nil, nil)
	case "run", "chat":
		o, rest := flags(args, "s")
		kind := "chat"
		if cmd == "run" {
			if err := need(rest, 2, "run TASK research|plan|implement [-s PR] [instructions]"); err != nil {
				return err
			}
			kind, rest = rest[1], append(rest[:1], rest[2:]...)
		} else if err := need(rest, 2, "chat TASK [-s PR] message"); err != nil {
			return err
		}
		t, err := x.task(rest[0])
		if err != nil {
			return err
		}
		sid, err := x.slice(t, o["s"])
		if err != nil {
			return err
		}
		var s store.Session
		err = x.do(http.MethodPost, "/api/sessions", map[string]string{"taskId": t.ID, "sliceId": sid, "kind": kind, "prompt": strings.Join(rest[1:], " ")}, &s)
		if err != nil {
			return err
		}
		fmt.Printf("session %s started (%s); follow with: layer8 log %s -f\n", short(s.ID), s.Title, short(s.ID))
		return nil
	case "say":
		if err := need(args, 2, "say SESSION message"); err != nil {
			return err
		}
		s, err := x.session(args[0])
		if err != nil {
			return err
		}
		return x.do(http.MethodPost, "/api/sessions/"+s.ID+"/messages", map[string]string{"text": strings.Join(args[1:], " ")}, nil)
	case "stop":
		if err := need(args, 1, "stop SESSION"); err != nil {
			return err
		}
		s, err := x.session(args[0])
		if err != nil {
			return err
		}
		return x.do(http.MethodPost, "/api/sessions/"+s.ID+"/interrupt", nil, nil)
	case "log":
		o, rest := flags(args, "f")
		if err := need(rest, 1, "log SESSION [-f]"); err != nil {
			return err
		}
		s, err := x.session(rest[0])
		if err != nil {
			return err
		}
		return x.log(s, o["f"] != "")
	case "approvals":
		s, err := x.state()
		if err != nil {
			return err
		}
		if len(s.Approvals) == 0 {
			fmt.Println("nothing waiting")
		}
		for _, a := range s.Approvals {
			in, _ := json.Marshal(a.Input)
			fmt.Printf("%s  session %s  %s  %s\n", short(a.ID), short(a.SessionID), a.ToolName, clip(string(in), 120))
		}
		return nil
	case "approve", "deny":
		if err := need(args, 1, cmd+" ID [reason]"); err != nil {
			return err
		}
		s, err := x.state()
		if err != nil {
			return err
		}
		for _, a := range s.Approvals {
			if strings.HasPrefix(a.ID, args[0]) {
				return x.do(http.MethodPost, "/api/approvals/"+a.ID, map[string]any{"allow": cmd == "approve", "message": strings.Join(args[1:], " ")}, nil)
			}
		}
		return fmt.Errorf("no pending approval %q", args[0])
	case "pr", "fix-ci", "review":
		o, rest := flags(args, "s")
		if err := need(rest, 1, cmd+" TASK -s PR"); err != nil {
			return err
		}
		t, err := x.task(rest[0])
		if err != nil {
			return err
		}
		sid, err := x.slice(t, o["s"])
		if err != nil || sid == "" {
			return fmt.Errorf("pick the PR with -s (e.g. -s pr1): %v", err)
		}
		var out map[string]any
		if err := x.do(http.MethodPost, "/api/slices/"+sid+"/"+cmd, nil, &out); err != nil {
			return err
		}
		if u, ok := out["prUrl"].(string); ok && u != "" {
			fmt.Println(u)
		} else if id, ok := out["id"].(string); ok {
			fmt.Printf("session %s started; follow with: layer8 log %s -f\n", short(id), short(id))
		}
		return nil
	case "open":
		o, rest := flags(args, "s")
		if err := need(rest, 2, "open TASK [-s PR] terminal|editor"); err != nil {
			return err
		}
		t, err := x.task(rest[0])
		if err != nil {
			return err
		}
		sid, err := x.slice(t, o["s"])
		if err != nil {
			return err
		}
		return x.do(http.MethodPost, "/api/open", map[string]string{"taskId": t.ID, "sliceId": sid, "target": rest[1]}, nil)
	case "install-cli":
		return installCLI()
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func clip(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func (x *cli) tasks() error {
	s, err := x.state()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, t := range s.Tasks {
		if t.Status != "active" {
			continue
		}
		id := t.Slug
		if t.Linear != nil {
			id = t.Linear.Identifier
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", id, clip(t.Title, 50), t.Branch)
		for _, sl := range s.Slices {
			if sl.TaskID != t.ID {
				continue
			}
			pr := "no PR"
			if sl.PRNumber > 0 {
				pr = fmt.Sprintf("#%d %s CI:%s", sl.PRNumber, strings.ToLower(sl.PRState), strings.ToLower(sl.CIState))
			}
			fmt.Fprintf(w, "  %s\t%s\t%s · sync:%s\n", sl.Key, clip(sl.Title, 46), pr, sl.SyncState)
		}
		for _, se := range s.Sessions {
			if se.TaskID == t.ID {
				fmt.Fprintf(w, "  ⋅ %s\t%s\t%s · $%.2f\n", short(se.ID), clip(se.Title, 46), se.Status, se.CostUSD)
			}
		}
	}
	return w.Flush()
}

func (x *cli) log(s store.Session, follow bool) error {
	var evs []store.Event
	if err := x.do(http.MethodGet, "/api/sessions/"+s.ID+"/events", nil, &evs); err != nil {
		return err
	}
	last := int64(0)
	for _, e := range evs {
		printEvent(e)
		last = e.ID
	}
	if !follow {
		return nil
	}
	return x.c.Events(x.ctx, func(name string, data json.RawMessage) {
		if name != "layer8:events" {
			return
		}
		var batch []store.Event
		if json.Unmarshal(data, &batch) != nil {
			return
		}
		for _, e := range batch {
			if e.SessionID == s.ID && e.ID > last {
				printEvent(e)
				last = e.ID
			}
		}
	})
}

func printEvent(e store.Event) {
	ts := e.At
	if t, err := time.Parse(time.RFC3339Nano, e.At); err == nil {
		ts = t.Local().Format("15:04:05")
	}
	str := func(k string) string { v, _ := e.Data[k].(string); return v }
	switch e.Type {
	case "user":
		fmt.Printf("%s › %s\n", ts, str("text"))
	case "text":
		fmt.Printf("%s ◆ %s\n", ts, str("text"))
	case "tool_use":
		in, _ := json.Marshal(e.Data["input"])
		fmt.Printf("%s ▸ %s %s\n", ts, str("name"), clip(string(in), 140))
	case "result":
		fmt.Printf("%s ■ %s · $%.2f\n", ts, str("subtype"), e.Data["costUsd"])
	case "error":
		fmt.Printf("%s ✕ %s\n", ts, str("message"))
	}
}

// installCLI links ~/.local/bin/layer8 (or /usr/local/bin when writable) to this binary.
func installCLI() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	home, _ := os.UserHomeDir()
	dirs := []string{"/usr/local/bin", filepath.Join(home, ".local", "bin")}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			continue
		}
		link := filepath.Join(d, "layer8")
		_ = os.Remove(link)
		if err := os.Symlink(exe, link); err != nil {
			continue
		}
		fmt.Printf("installed %s → %s\n", link, exe)
		if !onPath(d) {
			fmt.Printf("add it to your PATH: echo 'export PATH=\"%s:$PATH\"' >> ~/.zshrc\n", d)
		}
		return nil
	}
	return errors.New("could not create a symlink in /usr/local/bin or ~/.local/bin")
}

func onPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			return true
		}
	}
	return false
}
