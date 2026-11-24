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
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

const usage = `usage:
  l8 project add <path>
  l8 project list
  l8 task create --title T [--project P] [--workflow freeform] [--prompt P] [--adopt DIR] [--autonomous]
  l8 task list [--project P] [--status active|done|archived]
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

"current" is the task or session this command runs in (L8_TASK_ID, L8_SESSION_ID);
"parent" is the current task's parent task.
`

type command func(c *client.Client, out io.Writer, args []string) error

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
}

func main() {
	if len(os.Args) < 3 || commands[os.Args[1]+" "+os.Args[2]] == nil {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	c, err := client.FromEnv()
	if err == nil {
		err = commands[os.Args[1]+" "+os.Args[2]](c, os.Stdout, os.Args[3:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "l8:", err)
		os.Exit(1)
	}
}

func projectAdd(c *client.Client, out io.Writer, args []string) error {
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
	fmt.Fprintf(out, "added %s (%s, base %s)\n", p.Name, p.Path, p.BaseBranch)
	return nil
}

func projectList(c *client.Client, out io.Writer, args []string) error {
	projects, err := c.Projects()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, p := range projects {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, p.BaseBranch, p.Path, p.ID)
	}
	return w.Flush()
}

func taskCreate(c *client.Client, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("task create", flag.ContinueOnError)
	var in task.CreateInput
	project := fs.String("project", "", "project id, name or path (default: the only project)")
	fs.StringVar(&in.Title, "title", "", "task title")
	fs.StringVar(&in.Workflow, "workflow", "freeform", strings.Join(task.Workflows, "|"))
	fs.StringVar(&in.Prompt, "prompt", "", "what the task is about; written to task.md")
	fs.StringVar(&in.AdoptPath, "adopt", "", "use this existing worktree instead of creating one")
	fs.BoolVar(&in.Autonomous, "autonomous", false, "agents answer their own questions")
	if err := fs.Parse(args); err != nil {
		return err
	}
	p, err := findProject(c, *project)
	if err != nil {
		return err
	}
	in.ProjectID = p.ID
	if in.AdoptPath != "" {
		if in.AdoptPath, err = filepath.Abs(in.AdoptPath); err != nil {
			return err
		}
	}
	t, err := c.CreateTask(in)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "created %s on %s at %s\n", t.Slug, t.Branch, t.Worktree)
	return nil
}

func taskList(c *client.Client, out io.Writer, args []string) error {
	fs := flag.NewFlagSet("task list", flag.ContinueOnError)
	project := fs.String("project", "", "project id, name or path")
	status := fs.String("status", "", strings.Join(task.Statuses, "|"))
	if err := fs.Parse(args); err != nil {
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
	tasks, err := c.Tasks(f)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, t := range tasks {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", t.Slug, t.Status, t.Workflow, t.Branch, t.Title)
	}
	return w.Flush()
}

func taskShow(c *client.Client, out io.Writer, args []string) error {
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
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, kv := range [][2]string{
		{"title", t.Title}, {"id", t.ID}, {"slug", t.Slug}, {"status", t.Status}, {"workflow", t.Workflow},
		{"branch", t.Branch}, {"base", t.BaseRef}, {"worktree", t.Worktree}, {"artifacts", t.ArtifactsDir},
	} {
		fmt.Fprintf(w, "%s\t%s\n", kv[0], kv[1])
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(out, "\n%s", t.TaskMD)
	return nil
}

func taskStatus(status string) command {
	return func(c *client.Client, out io.Writer, args []string) error {
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
		fmt.Fprintf(out, "%s is %s\n", t.Slug, t.Status)
		return nil
	}
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

func sessionStart(c *client.Client, out io.Writer, args []string) error {
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
	fmt.Fprintf(out, "started %s (%s), %s\n", s.ID, s.Label, s.Status)
	return nil
}

func sessionList(c *client.Client, out io.Writer, args []string) error {
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
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, s := range sessions {
		status := s.Status
		if s.Queued > 0 {
			status += fmt.Sprintf(" (+%d queued)", s.Queued)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t$%.2f\n", s.ID, s.Label, status, s.CostUSD)
	}
	return w.Flush()
}

func sessionRead(c *client.Client, out io.Writer, args []string) error {
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
	var messages []string
	for _, e := range events {
		var body struct{ Text string }
		json.Unmarshal(e.Body, &body)
		switch {
		case e.Kind == "user":
			messages = append(messages, "> "+body.Text)
		case e.Kind == "text" && e.ParentToolUseID == "":
			messages = append(messages, body.Text)
		}
	}
	messages = messages[max(0, len(messages)-*last):]
	fmt.Fprintln(out, strings.Join(messages, "\n\n"))
	return nil
}

func sessionSend(c *client.Client, out io.Writer, args []string) error {
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
	if queued {
		fmt.Fprintln(out, "queued; it is sent when the current turn ends")
	} else {
		fmt.Fprintln(out, "sent")
	}
	return nil
}

func sessionStop(c *client.Client, out io.Writer, args []string) error {
	id, err := sessionArg(args)
	if err != nil {
		return err
	}
	if err := c.Stop(id); err != nil {
		return err
	}
	fmt.Fprintln(out, "stopped", id)
	return nil
}

func artifactList(c *client.Client, out io.Writer, args []string) error {
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
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, a := range artifacts {
		fmt.Fprintf(w, "%s\t%s\t%s\n", a.Path, a.Type, time.UnixMilli(a.ModTime).Format(time.DateTime))
	}
	return w.Flush()
}

func artifactRead(c *client.Client, out io.Writer, args []string) error {
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
	_, err = out.Write(data)
	return err
}

func artifactWrite(c *client.Client, out io.Writer, args []string) error {
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
	fmt.Fprintf(out, "wrote %s (%d bytes)\n", a.Path, a.Size)
	return nil
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
