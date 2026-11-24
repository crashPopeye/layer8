// Command l8 drives the layer8 daemon from a terminal or from an agent's Bash tool.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

const usage = `usage: l8 <command> [--json] ...
  l8 project add <path>
  l8 project list
  l8 task create --title T [--parent <id|current>] [--project P] [--workflow freeform] [--prompt P] [--start] [--auto-advance] [--adopt DIR] [--autonomous]
  l8 task list [--project P] [--parent <id|current>] [--status active|done|archived]
  l8 task show <id|slug|current>
  l8 task done <id|slug|current>
  l8 task archive <id|slug|current>
  l8 session start --task <id|current> [--label L] --prompt P [--model M] [--effort E] [--permission-mode default|acceptEdits|auto|bypassPermissions]
  l8 session list --task <id|current>
  l8 session read <id|current> [--last 20]
  l8 session send <id|current> <text>
  l8 session stop <id|current>
  l8 artifact list [--task <id|current|parent>]
  l8 artifact read [--task <id|current|parent>] <path>
  l8 artifact write [--task <id|current|parent>] <path> < content
  l8 phase next [--task <id|current>]

"current" is the task or session this command runs in (L8_TASK_ID, L8_SESSION_ID);
"parent" is the current task's parent task. --json prints the result as JSON.
`

type command func(c *client.Client, out printer, args []string) error

// printer writes a command's result as JSON or as the command's text.
type printer struct {
	w    io.Writer
	json bool
}

func (p printer) print(v any, text func(w io.Writer)) error {
	if p.json {
		enc := json.NewEncoder(p.w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	text(p.w)
	return nil
}

// table prints rows as aligned columns.
func table(w io.Writer, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
}

var commands = map[string]command{
	"project add":    projectAdd,
	"project list":   projectList,
	"task create":    taskCreate,
	"task list":      taskList,
	"task show":      taskShow,
	"task done":      taskStatus("done"),
	"task archive":   taskStatus("archived"),
	"session start":  sessionStart,
	"session list":   sessionList,
	"session read":   sessionRead,
	"session send":   sessionSend,
	"session stop":   sessionStop,
	"artifact list":  artifactList,
	"artifact read":  artifactRead,
	"artifact write": artifactWrite,
	"phase next":     phaseNext,
}

func main() {
	if len(os.Args) < 3 || commands[os.Args[1]+" "+os.Args[2]] == nil {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	out := printer{w: os.Stdout}
	args := slices.DeleteFunc(slices.Clone(os.Args[3:]), func(a string) bool {
		if a == "--json" || a == "-json" {
			out.json = true
			return true
		}
		return false
	})
	c, err := client.FromEnv()
	if err == nil {
		err = commands[os.Args[1]+" "+os.Args[2]](c, out, args)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "l8:", err)
		os.Exit(1)
	}
}

func projectAdd(c *client.Client, out printer, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: l8 project add <path>")
	}
	path, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	p, err := c.AddProject(path)
	if err != nil {
		return err
	}
	return out.print(p, func(w io.Writer) { fmt.Fprintf(w, "added %s (%s, base %s)\n", p.Name, p.Path, p.BaseBranch) })
}

func projectList(c *client.Client, out printer, args []string) error {
	projects, err := c.Projects()
	if err != nil {
		return err
	}
	return out.print(projects, func(w io.Writer) {
		var rows [][]string
		for _, p := range projects {
			rows = append(rows, []string{p.Name, p.BaseBranch, p.Path, p.ID})
		}
		table(w, rows)
	})
}

func taskCreate(c *client.Client, out printer, args []string) error {
	fs := flag.NewFlagSet("task create", flag.ContinueOnError)
	var in task.CreateInput
	project := fs.String("project", "", "project id, name or path (default: the parent's project, else the only project)")
	parent := fs.String("parent", "", "make it a sub-task of this task (id, slug or current)")
	fs.StringVar(&in.Title, "title", "", "task title")
	fs.StringVar(&in.Workflow, "workflow", "freeform", strings.Join(task.Workflows, "|"))
	fs.StringVar(&in.Prompt, "prompt", "", "what the task is about; written to task.md and, with --start, sent to its first session")
	fs.BoolVar(&in.Start, "start", false, "start the task's first session: its workflow's first phase, or --prompt for freeform")
	fs.BoolVar(&in.AutoAdvance, "auto-advance", false, "start each next workflow phase as soon as the previous one is done")
	fs.StringVar(&in.AdoptPath, "adopt", "", "use this existing worktree instead of creating one")
	fs.BoolVar(&in.Autonomous, "autonomous", false, "agents answer their own questions (sub-tasks inherit it)")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	var err error
	if *parent != "" {
		if in.ParentID, err = current(*parent, "L8_TASK_ID"); err != nil {
			return err
		}
	}
	if *project != "" || in.ParentID == "" {
		p, err := findProject(c, *project)
		if err != nil {
			return err
		}
		in.ProjectID = p.ID
	}
	if in.AdoptPath != "" {
		if in.AdoptPath, err = filepath.Abs(in.AdoptPath); err != nil {
			return err
		}
	}
	t, err := c.CreateTask(in)
	if err != nil {
		return err
	}
	return out.print(t, func(w io.Writer) {
		fmt.Fprintf(w, "created %s (%s) on %s at %s\n", t.Slug, t.ID, t.Branch, t.Worktree)
		if t.Session != nil {
			fmt.Fprintf(w, "session %s (%s) %s\n", t.Session.ID, t.Session.Label, t.Session.Status)
		}
	})
}

func taskList(c *client.Client, out printer, args []string) error {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	project := fs.String("project", "", "project id, name or path")
	parent := fs.String("parent", "", "only sub-tasks of this task (id, slug or current)")
	status := fs.String("status", "", strings.Join(task.Statuses, "|"))
	if _, err := parse(fs, args); err != nil {
		return err
	}
	f := store.TaskFilter{Status: *status}
	if *project != "" {
		p, err := findProject(c, *project)
		if err != nil {
			return err
		}
		f.ProjectID = p.ID
	}
	if *parent != "" {
		ref, err := current(*parent, "L8_TASK_ID")
		if err != nil {
			return err
		}
		pt, err := c.Task(ref)
		if err != nil {
			return err
		}
		f.ParentID = pt.ID
	}
	tasks, err := c.Tasks(f)
	if err != nil {
		return err
	}
	return out.print(tasks, func(w io.Writer) {
		var rows [][]string
		for _, t := range tasks {
			rows = append(rows, []string{t.Slug, t.Status, t.Workflow, t.Branch, t.Title})
		}
		table(w, rows)
	})
}

func taskShow(c *client.Client, out printer, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: l8 task show <id|slug>")
	}
	ref, err := current(args[0], "L8_TASK_ID")
	if err != nil {
		return err
	}
	t, err := c.Task(ref)
	if err != nil {
		return err
	}
	return out.print(t, func(w io.Writer) {
		rows := [][]string{
			{"title", t.Title}, {"id", t.ID}, {"slug", t.Slug}, {"status", t.Status}, {"workflow", t.Workflow},
			{"branch", t.Branch}, {"base", t.BaseRef}, {"worktree", t.Worktree}, {"artifacts", t.ArtifactsDir},
			{"autonomous", fmt.Sprint(t.Autonomous)}, {"auto-advance", fmt.Sprint(t.AutoAdvance)},
		}
		if t.ParentID != "" {
			rows = append(rows, []string{"parent", t.ParentID})
		}
		if len(t.Phases) > 0 {
			rows = append(rows, []string{"phases", phaseRail(t)})
		}
		table(w, rows)
		fmt.Fprintf(w, "\n%s", t.TaskMD)
	})
}

func taskStatus(status string) command {
	return func(c *client.Client, out printer, args []string) error {
		if len(args) != 1 {
			return errors.New("usage: l8 task done|archive <id|slug>")
		}
		ref, err := current(args[0], "L8_TASK_ID")
		if err != nil {
			return err
		}
		t, err := c.SetTaskStatus(ref, status)
		if err != nil {
			return err
		}
		return out.print(t, func(w io.Writer) { fmt.Fprintf(w, "%s is %s\n", t.Slug, t.Status) })
	}
}

// phaseRail renders a task's phases on one line: done ones with ✓, the current one in brackets.
func phaseRail(t task.Detail) string {
	parts := make([]string, len(t.Phases))
	for i, p := range t.Phases {
		parts[i] = p.Name
		if p.Done {
			parts[i] += " ✓"
		}
		if p.Name == t.Phase {
			parts[i] = "[" + parts[i] + "]"
		}
	}
	rail := strings.Join(parts, " → ")
	if t.Next != "" {
		rail += "   (next: " + t.Next + ", start it with l8 phase next)"
	}
	return rail
}

func phaseNext(c *client.Client, out printer, args []string) error {
	fs := flag.NewFlagSet("phase next", flag.ContinueOnError)
	taskRef := fs.String("task", "current", "task id, slug or current")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	ref, err := current(*taskRef, "L8_TASK_ID")
	if err != nil {
		return err
	}
	s, err := c.NextPhase(ref)
	if err != nil {
		return err
	}
	return out.print(s, func(w io.Writer) { fmt.Fprintf(w, "started %s (%s), %s\n", s.ID, s.Label, s.Status) })
}

// findProject resolves a project by id, name or path; an empty ref means the only project.
func findProject(c *client.Client, ref string) (store.Project, error) {
	projects, err := c.Projects()
	if err != nil {
		return store.Project{}, err
	}
	if ref == "" {
		if len(projects) == 1 {
			return projects[0], nil
		}
		return store.Project{}, fmt.Errorf("%d projects; pick one with --project", len(projects))
	}
	abs, _ := filepath.Abs(ref)
	for _, p := range projects {
		if ref == p.ID || ref == p.Name || abs == p.Path {
			return p, nil
		}
	}
	return store.Project{}, fmt.Errorf("no project %q", ref)
}

func sessionStart(c *client.Client, out printer, args []string) error {
	fs := flag.NewFlagSet("session start", flag.ContinueOnError)
	taskRef := fs.String("task", "current", "task id, slug or current")
	var in session.StartInput
	fs.StringVar(&in.Label, "label", "session", "session label, e.g. a phase name")
	fs.StringVar(&in.Prompt, "prompt", "", "first message")
	fs.StringVar(&in.Model, "model", "", "model (default: the user's Claude Code default)")
	fs.StringVar(&in.Effort, "effort", "", "effort level")
	fs.StringVar(&in.PermissionMode, "permission-mode", "", strings.Join(session.PermissionModes, "|"))
	if _, err := parse(fs, args); err != nil {
		return err
	}
	ref, err := current(*taskRef, "L8_TASK_ID")
	if err != nil {
		return err
	}
	s, err := c.StartSession(ref, in)
	if err != nil {
		return err
	}
	return out.print(s, func(w io.Writer) { fmt.Fprintf(w, "started %s (%s), %s\n", s.ID, s.Label, s.Status) })
}

func sessionList(c *client.Client, out printer, args []string) error {
	fs := flag.NewFlagSet("session list", flag.ContinueOnError)
	taskRef := fs.String("task", "current", "task id, slug or current")
	if _, err := parse(fs, args); err != nil {
		return err
	}
	ref, err := current(*taskRef, "L8_TASK_ID")
	if err != nil {
		return err
	}
	sessions, err := c.Sessions(ref)
	if err != nil {
		return err
	}
	return out.print(sessions, func(w io.Writer) {
		var rows [][]string
		for _, s := range sessions {
			status := s.Status
			if s.Queued > 0 {
				status += fmt.Sprintf(" (+%d queued)", s.Queued)
			}
			rows = append(rows, []string{s.ID, s.Label, status, fmt.Sprintf("$%.2f", s.CostUSD)})
		}
		table(w, rows)
	})
}

// message is one turn of a conversation as l8 session read prints it.
type message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

func sessionRead(c *client.Client, out printer, args []string) error {
	fs := flag.NewFlagSet("session read", flag.ContinueOnError)
	last := fs.Int("last", 20, "number of messages")
	rest, err := parse(fs, args)
	if err != nil {
		return err
	}
	id, err := sessionArg(rest)
	if err != nil {
		return err
	}
	events, err := c.Events(id, 0)
	if err != nil {
		return err
	}
	messages := []message{}
	for _, e := range events {
		var body struct{ Text string }
		json.Unmarshal(e.Body, &body)
		switch {
		case e.Kind == "user":
			messages = append(messages, message{"user", body.Text})
		case e.Kind == "text" && e.ParentToolUseID == "":
			messages = append(messages, message{"assistant", body.Text})
		}
	}
	messages = messages[max(0, len(messages)-*last):]
	return out.print(messages, func(w io.Writer) {
		texts := make([]string, len(messages))
		for i, m := range messages {
			texts[i] = m.Text
			if m.Role == "user" {
				texts[i] = "> " + m.Text
			}
		}
		fmt.Fprintln(w, strings.Join(texts, "\n\n"))
	})
}

func sessionSend(c *client.Client, out printer, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: l8 session send <id|current> <text>")
	}
	id, err := current(args[0], "L8_SESSION_ID")
	if err != nil {
		return err
	}
	queued, err := c.Send(id, strings.Join(args[1:], " "))
	if err != nil {
		return err
	}
	return out.print(map[string]bool{"queued": queued}, func(w io.Writer) {
		if queued {
			fmt.Fprintln(w, "queued; it is sent when the current turn ends")
		} else {
			fmt.Fprintln(w, "sent")
		}
	})
}

func sessionStop(c *client.Client, out printer, args []string) error {
	id, err := sessionArg(args)
	if err != nil {
		return err
	}
	if err := c.Stop(id); err != nil {
		return err
	}
	return out.print(map[string]string{"stopped": id}, func(w io.Writer) { fmt.Fprintln(w, "stopped", id) })
}

func artifactList(c *client.Client, out printer, args []string) error {
	ref, rest, err := artifactArgs(c, "artifact list", args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return errors.New("usage: l8 artifact list [--task <id|current|parent>]")
	}
	artifacts, err := c.Artifacts(ref)
	if err != nil {
		return err
	}
	return out.print(artifacts, func(w io.Writer) {
		var rows [][]string
		for _, a := range artifacts {
			rows = append(rows, []string{a.Path, a.Type, time.UnixMilli(a.ModTime).Format(time.DateTime)})
		}
		table(w, rows)
	})
}

func artifactRead(c *client.Client, out printer, args []string) error {
	ref, rest, err := artifactArgs(c, "artifact read", args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("usage: l8 artifact read [--task <id|current|parent>] <path>")
	}
	data, err := c.ReadArtifact(ref, rest[0])
	if err != nil {
		return err
	}
	return out.print(map[string]string{"path": rest[0], "content": string(data)}, func(w io.Writer) { w.Write(data) })
}

func artifactWrite(c *client.Client, out printer, args []string) error {
	ref, rest, err := artifactArgs(c, "artifact write", args)
	if err != nil {
		return err
	}
	if len(rest) != 1 {
		return errors.New("usage: l8 artifact write [--task <id|current|parent>] <path> < content")
	}
	a, err := c.WriteArtifact(ref, rest[0], os.Stdin)
	if err != nil {
		return err
	}
	return out.print(a, func(w io.Writer) { fmt.Fprintf(w, "wrote %s (%d bytes)\n", a.Path, a.Size) })
}

// artifactArgs parses --task and resolves it to a task id; it returns the positional arguments.
func artifactArgs(c *client.Client, name string, args []string) (string, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	taskRef := fs.String("task", "current", "task id, slug, current or parent")
	rest, err := parse(fs, args)
	if err != nil {
		return "", nil, err
	}
	if *taskRef != "parent" {
		ref, err := current(*taskRef, "L8_TASK_ID")
		return ref, rest, err
	}
	ref, err := current("current", "L8_TASK_ID")
	if err != nil {
		return "", nil, err
	}
	t, err := c.Task(ref)
	if err != nil {
		return "", nil, err
	}
	if t.ParentID == "" {
		return "", nil, fmt.Errorf("task %s has no parent", t.Slug)
	}
	return t.ParentID, rest, nil
}

func sessionArg(args []string) (string, error) {
	if len(args) != 1 {
		return "", errors.New("expected one session id (or current)")
	}
	return current(args[0], "L8_SESSION_ID")
}

// current resolves the ref "current" from the environment variable env.
func current(ref, env string) (string, error) {
	if ref != "current" {
		return ref, nil
	}
	if v := os.Getenv(env); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("\"current\" only works inside a layer8 session (%s is not set)", env)
}

// parse parses flags that may come before or after positional arguments, and returns the positionals.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positional, nil
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
