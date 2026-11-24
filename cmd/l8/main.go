// Command l8 drives the layer8 daemon from a terminal or from an agent's Bash tool.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
)

const usage = `usage:
  l8 project add <path>
  l8 project list
  l8 task create --title T [--project P] [--workflow freeform] [--prompt P] [--adopt DIR] [--autonomous]
  l8 task list [--project P] [--status active|done|archived]
  l8 task show <id|slug>
  l8 task done <id|slug>
  l8 task archive <id|slug>
`

type command func(c *client.Client, out io.Writer, args []string) error

var commands = map[string]command{
	"project add":  projectAdd,
	"project list": projectList,
	"task create":  taskCreate,
	"task list":    taskList,
	"task show":    taskShow,
	"task done":    taskStatus("done"),
	"task archive": taskStatus("archived"),
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
	t, err := c.Task(args[0])
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
		t, err := c.SetTaskStatus(args[0], status)
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
