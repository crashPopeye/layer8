// Package workflows provides named chains of skills that run as agent sessions on a task, one step after another.
package workflows

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/crashPopeye/layer8/internal/tree"
)

//go:embed builtin/*.yaml
var builtin embed.FS

var namePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// Step is one stage of a workflow: a skill run in a fresh session, on the task or on each of its sub-tasks.
type Step struct {
	Name    string `yaml:"name" json:"name"`
	Skill   string `yaml:"skill" json:"skill"`
	Harness string `yaml:"harness,omitempty" json:"harness,omitempty"`
	Model   string `yaml:"model,omitempty" json:"model,omitempty"`
	Effort  string `yaml:"effort,omitempty" json:"effort,omitempty"`
	Fanout  string `yaml:"fanout,omitempty" json:"fanout,omitempty"`
}

// Workflow is a named list of steps, read from builtin/<name>.yaml or <dir>/<name>.yaml.
type Workflow struct {
	Name        string `yaml:"-"`
	Description string `yaml:"description"`
	Steps       []Step `yaml:"steps"`
	Source      string `yaml:"-"`
	Err         error  `yaml:"-"`
}

// Parse decodes and validates the workflow called name.
func Parse(name string, data []byte) (Workflow, error) {
	w := Workflow{Name: name}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&w); err != nil {
		return w, fmt.Errorf("workflow %s: %w", name, err)
	}
	return w, w.validate()
}

func (w Workflow) validate() error {
	var errs []error
	if !namePattern.MatchString(w.Name) {
		errs = append(errs, fmt.Errorf("name %q must be lowercase letters, digits and dashes", w.Name))
	}
	if len(w.Steps) == 0 {
		errs = append(errs, errors.New("no steps"))
	}
	seen := map[string]bool{}
	for i, s := range w.Steps {
		switch {
		case !namePattern.MatchString(s.Name):
			errs = append(errs, fmt.Errorf("step %d: name %q must be lowercase letters, digits and dashes", i+1, s.Name))
		case seen[s.Name]:
			errs = append(errs, fmt.Errorf("step %d: name %q is used twice", i+1, s.Name))
		}
		seen[s.Name] = true
		if s.Skill == "" {
			errs = append(errs, fmt.Errorf("step %s: skill is required", s.Name))
		}
		if !slices.Contains([]string{"", "claude", "codex"}, s.Harness) {
			errs = append(errs, fmt.Errorf("step %s: harness %q is not claude or codex", s.Name, s.Harness))
		}
		if !slices.Contains([]string{"", "subtasks"}, s.Fanout) {
			errs = append(errs, fmt.Errorf("step %s: fanout %q is not subtasks", s.Name, s.Fanout))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("workflow %s: %w", w.Name, err)
	}
	return nil
}

// Options configures Register.
type Options struct {
	// Dir holds the user's workflow files; one named like a built-in replaces it.
	Dir string
}

// Service owns the workflow kind and the workflow runs on tasks.
type Service struct {
	db   *sql.DB
	tree *tree.Tree
	o    Options
	mu   sync.Mutex
}

// Register adds the workflow kind as children of app, the reload-workflows app action,
// and the workflow actions and field on tasks.
func Register(t *tree.Tree, db *sql.DB, o Options) *Service {
	s := &Service{db: db, tree: t, o: o}
	t.Register(tree.Kind{Name: "workflow", Get: s.get})
	t.AddChildren("app", s.list)
	t.Extend("app", tree.Action{Name: "reload-workflows", Help: "Re-read the workflow files", Run: s.reload})
	s.registerRuns()
	return s
}

// Load returns every workflow by name: the built-ins, replaced or joined by the files in the user's folder.
// Files that fail to parse are returned with Err set.
func (s *Service) Load() []Workflow {
	byName := map[string]Workflow{}
	read := func(fsys fs.FS, pattern, source string) {
		paths, _ := fs.Glob(fsys, pattern)
		for _, p := range paths {
			name := strings.TrimSuffix(filepath.Base(p), ".yaml")
			data, err := fs.ReadFile(fsys, p)
			w := Workflow{Name: name}
			if err == nil {
				w, err = Parse(name, data)
			}
			w.Source, w.Err = source, err
			if source != "builtin" {
				w.Source = filepath.Join(s.o.Dir, p)
			}
			byName[name] = w
		}
	}
	read(builtin, "builtin/*.yaml", "builtin")
	if s.o.Dir != "" {
		read(os.DirFS(s.o.Dir), "*.yaml", "")
	}
	list := make([]Workflow, 0, len(byName))
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		list = append(list, byName[name])
	}
	return list
}

func (s *Service) find(name string) (Workflow, error) {
	list := s.Load()
	i := slices.IndexFunc(list, func(w Workflow) bool { return w.Name == name })
	if i < 0 {
		return Workflow{}, fmt.Errorf("%w: workflow:%s", tree.ErrNotFound, name)
	}
	return list[i], nil
}

func (s *Service) list(context.Context, tree.Addr) ([]tree.Summary, error) {
	var out []tree.Summary
	for _, w := range s.Load() {
		status := ""
		if w.Err != nil {
			status = "invalid"
		}
		out = append(out, tree.Summary{Addr: addr(w.Name), Kind: "workflow", Title: w.Name, Status: status})
	}
	return out, nil
}

func addr(name string) tree.Addr { return tree.Addr("workflow:" + name) }

func (s *Service) get(_ context.Context, a tree.Addr) (tree.Node, error) {
	w, err := s.find(a.Path())
	if err != nil {
		return tree.Node{}, err
	}
	fields := map[string]any{"description": w.Description, "steps": w.Steps, "source": w.Source}
	if w.Err != nil {
		fields["error"] = w.Err.Error()
	}
	return tree.Node{Title: w.Name, Fields: fields}, nil
}

func (s *Service) reload(_ context.Context, _ tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	list := s.Load()
	addrs := []tree.Addr{"app"}
	for _, w := range list {
		addrs = append(addrs, addr(w.Name))
	}
	s.tree.Changed(addrs...)
	return tree.Result{Message: fmt.Sprintf("%d workflow(s) loaded", len(list))}, nil
}

// Watch publishes a change to app and the workflows whose files changed, every interval, until ctx ends.
func (s *Service) Watch(ctx context.Context, every time.Duration) {
	seen := s.stamps()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := s.stamps()
		var changed []tree.Addr
		for name, stamp := range now {
			if seen[name] != stamp {
				changed = append(changed, addr(name))
			}
		}
		for name := range seen {
			if _, ok := now[name]; !ok {
				changed = append(changed, addr(name))
			}
		}
		if len(changed) > 0 {
			s.tree.Changed(append(changed, "app")...)
		}
		seen = now
	}
}

func (s *Service) stamps() map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(s.o.Dir)
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".yaml")
		if info, err := e.Info(); ok && err == nil {
			out[name] = fmt.Sprintf("%d/%d", info.ModTime().UnixNano(), info.Size())
		}
	}
	return out
}
