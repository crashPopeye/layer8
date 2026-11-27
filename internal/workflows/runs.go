package workflows

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/tree"
)

// run is a workflow running on one task. Sub-tasks belong to the run of their nearest ancestor.
type run struct {
	TaskID    int64
	Slug      string
	Workflow  string
	Steps     []Step
	Step      int
	Status    string
	Autopilot bool
	Entries   []*entry
}

// entry is the work of the current step on one task: the run's own task, or one sub-task when the step fans out.
type entry struct {
	Task    string `json:"task"`
	State   string `json:"state"`
	Session string `json:"session,omitempty"`
	After   string `json:"after,omitempty"`
	Error   string `json:"error,omitempty"`
}

const (
	stateRunning = "running"
	stateWaiting = "waiting"
	stateDone    = "done"
	stateStopped = "stopped"
	statePending = "pending"
	stateFailed  = "failed"
)

func (r *run) step() Step { return r.Steps[r.Step] }

func (r *run) owner() tree.Addr { return taskAddr(r.Slug) }

func (r *run) active() bool { return r.Status == stateRunning || r.Status == stateWaiting }

func (r *run) entry(slug string) *entry {
	i := slices.IndexFunc(r.Entries, func(e *entry) bool { return e.Task == slug })
	if i < 0 {
		return nil
	}
	return r.Entries[i]
}

func taskAddr(slug string) tree.Addr { return tree.Addr("task:" + strings.ReplaceAll(slug, "--", "/")) }

func slugOf(a tree.Addr) string { return strings.ReplaceAll(a.Path(), "/", "--") }

func (s *Service) registerRuns() {
	s.tree.Extend("task",
		tree.Action{Name: "run-workflow", Help: "Run a workflow on this task, starting its first step", Run: s.runWorkflow,
			When: func(n tree.Node) bool { return n.Fields["status"] == "active" && !ownsActive(n) },
			Args: json.RawMessage(`{"type":"object","required":["workflow"],"properties":{
				"workflow":{"type":"string","description":"Workflow name, from the workflow:<name> children of app"},
				"autopilot":{"type":"boolean","description":"Start each next step as soon as the previous one finishes, instead of waiting for next-step"}}}`)},
		tree.Action{Name: "advance", Help: "Mark the current workflow step finished here; called by a workflow skill when its work is done", Run: s.advance,
			When: workflowStatus(stateRunning),
			Args: json.RawMessage(`{"type":"object","properties":{"step":{"type":"string","description":"Step expected to be current; nothing happens if another step is"}}}`)},
		tree.Action{Name: "next-step", Help: "Start the workflow's next step", Run: s.nextStep, When: workflowStatus(stateWaiting)},
		tree.Action{Name: "stop-workflow", Help: "Stop the workflow; its sessions keep running", Run: s.stop, When: workflowStatus(stateRunning, stateWaiting)},
	)
	s.tree.AddFields("task", s.fields)
}

func workflowField(n tree.Node) map[string]any {
	w, _ := n.Fields["workflow"].(map[string]any)
	return w
}

func workflowStatus(statuses ...string) func(tree.Node) bool {
	return func(n tree.Node) bool {
		status, _ := workflowField(n)["status"].(string)
		return slices.Contains(statuses, status)
	}
}

func ownsActive(n tree.Node) bool {
	w := workflowField(n)
	return w != nil && w["task"] == n.Addr && (w["status"] == stateRunning || w["status"] == stateWaiting)
}

// runs returns the runs on slug and its ancestors, nearest first.
func (s *Service) runs(ctx context.Context, slug string) ([]*run, error) {
	var chain []any
	for p := slug; ; {
		chain = append(chain, p)
		i := strings.LastIndex(p, "--")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	rows, err := s.db.QueryContext(ctx, `SELECT r.task_id, t.slug, r.workflow, r.steps, r.step, r.status, r.autopilot, r.entries
		FROM workflow_runs r JOIN tasks t ON t.id = r.task_id WHERE t.slug IN (?`+strings.Repeat(",?", len(chain)-1)+`)
		ORDER BY length(t.slug) DESC`, chain...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*run
	for rows.Next() {
		var r run
		var steps, entries string
		if err := rows.Scan(&r.TaskID, &r.Slug, &r.Workflow, &steps, &r.Step, &r.Status, &r.Autopilot, &entries); err != nil {
			return nil, err
		}
		if err := errors.Join(json.Unmarshal([]byte(steps), &r.Steps), json.Unmarshal([]byte(entries), &r.Entries)); err != nil {
			return nil, err
		}
		list = append(list, &r)
	}
	return list, rows.Err()
}

// current returns the nearest active run over slug, or else the nearest run, or nil.
func (s *Service) current(ctx context.Context, slug string) (*run, error) {
	list, err := s.runs(ctx, slug)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	if i := slices.IndexFunc(list, (*run).active); i >= 0 {
		return list[i], nil
	}
	return list[0], nil
}

func (s *Service) save(ctx context.Context, r *run, by tree.Actor) error {
	steps, _ := json.Marshal(r.Steps)
	entries, _ := json.Marshal(r.Entries)
	_, err := s.db.ExecContext(ctx, `INSERT INTO workflow_runs (task_id, workflow, steps, step, status, autopilot, entries, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (task_id) DO UPDATE SET workflow = excluded.workflow, steps = excluded.steps, step = excluded.step,
			status = excluded.status, autopilot = excluded.autopilot, entries = excluded.entries,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		r.TaskID, r.Workflow, steps, r.Step, r.Status, r.Autopilot, entries, by.String())
	if err != nil {
		return err
	}
	changed := []tree.Addr{r.owner()}
	for _, e := range r.Entries {
		changed = append(changed, taskAddr(e.Task))
	}
	s.tree.Changed(changed...)
	return nil
}

func (s *Service) fields(ctx context.Context, n tree.Node) (map[string]any, error) {
	r, err := s.current(ctx, slugOf(n.Addr))
	if err != nil || r == nil {
		return nil, err
	}
	steps := make([]string, len(r.Steps))
	for i, st := range r.Steps {
		steps[i] = st.Name
	}
	next := ""
	if r.Status == stateWaiting {
		next = steps[r.Step+1]
	}
	progress := map[string]any{}
	for _, e := range r.Entries {
		progress[string(taskAddr(e.Task))] = map[string]string{"state": e.State, "session": e.Session, "error": e.Error}
	}
	return map[string]any{"workflow": map[string]any{
		"name": r.Workflow, "task": r.owner(), "steps": steps, "step": steps[r.Step], "status": r.Status,
		"autopilot": r.Autopilot, "next": next, "progress": progress,
	}}, nil
}

func (s *Service) runWorkflow(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct {
		Workflow  string
		Autopilot bool
	}
	json.Unmarshal(args, &a)
	w, err := s.find(a.Workflow)
	if err != nil {
		return tree.Result{}, err
	}
	if w.Err != nil {
		return tree.Result{}, w.Err
	}
	if !slices.ContainsFunc(n.Actions, func(a tree.Action) bool { return a.Name == "start-session" }) {
		return tree.Result{}, fmt.Errorf("%s cannot start sessions", n.Addr)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM tasks WHERE slug = ?`, slugOf(n.Addr)).Scan(&id); err != nil {
		return tree.Result{}, err
	}
	r := &run{TaskID: id, Slug: slugOf(n.Addr), Workflow: w.Name, Steps: w.Steps, Status: stateRunning, Autopilot: a.Autopilot}
	msg, err := s.startStep(ctx, r, by)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Message: fmt.Sprintf("running workflow %s on %s: %s", w.Name, n.Addr, msg)}, s.save(ctx, r, by)
}

// startStep fills the entries of the run's current step and starts the sessions that can start now.
func (s *Service) startStep(ctx context.Context, r *run, by tree.Actor) (string, error) {
	r.Entries = []*entry{{Task: r.Slug, State: statePending}}
	if r.step().Fanout == "subtasks" {
		subs, err := s.subtasks(ctx, r.Slug)
		if err != nil {
			return "", err
		}
		if len(subs) > 0 {
			r.Entries = subs
		}
	}
	return s.startReady(ctx, r, by), nil
}

// subtasks returns an entry per active direct sub-task of slug, each after the sibling whose branch it is stacked on.
func (s *Service) subtasks(ctx context.Context, slug string) ([]*entry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT slug, branch, base_ref FROM tasks
		WHERE parent_id = (SELECT id FROM tasks WHERE slug = ?) AND status = 'active' ORDER BY id`, slug)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*entry
	bases := map[string]string{}
	branches := map[string]string{}
	for rows.Next() {
		var child, branch, base string
		if err := rows.Scan(&child, &branch, &base); err != nil {
			return nil, err
		}
		list = append(list, &entry{Task: child, State: statePending})
		bases[child], branches[branch] = base, child
	}
	for _, e := range list {
		e.After = branches[bases[e.Task]]
	}
	return list, rows.Err()
}

// startReady starts a session for every pending entry whose predecessor is done, in stack order.
func (s *Service) startReady(ctx context.Context, r *run, by tree.Actor) string {
	st := r.step()
	args, _ := json.Marshal(map[string]string{"skill": st.Skill, "harness": st.Harness, "model": st.Model, "effort": st.Effort, "title": st.Name})
	var started []string
	for _, e := range r.Entries {
		if e.State != statePending || (e.After != "" && r.entry(e.After) != nil && r.entry(e.After).State != stateDone) {
			continue
		}
		res := s.tree.Do(ctx, by, []tree.Op{{Target: taskAddr(e.Task), Action: "start-session", Args: compact(args)}})[0]
		if !res.OK {
			e.State, e.Error = stateFailed, res.Message
			started = append(started, fmt.Sprintf("%s failed: %s", taskAddr(e.Task), res.Message))
			continue
		}
		e.State, e.Session = stateRunning, res.Addr.Path()
		started = append(started, string(res.Addr)+" in "+string(taskAddr(e.Task)))
	}
	msg := "step " + st.Name
	if len(started) > 0 {
		msg += " started " + strings.Join(started, ", ")
	}
	if n := countState(r, statePending); n > 0 {
		msg += fmt.Sprintf("; %d waiting for the task they are stacked on", n)
	}
	return msg
}

// compact drops empty string fields so start-session applies its own defaults.
func compact(args []byte) json.RawMessage {
	var m map[string]string
	json.Unmarshal(args, &m)
	for k, v := range m {
		if v == "" {
			delete(m, k)
		}
	}
	out, _ := json.Marshal(m)
	return out
}

func countState(r *run, state string) int {
	n := 0
	for _, e := range r.Entries {
		if e.State == state {
			n++
		}
	}
	return n
}

// finish marks entries done, starts what they unblock, and moves the run on when the whole step is done.
func (s *Service) finish(ctx context.Context, r *run, done []*entry, by tree.Actor) (string, error) {
	for _, e := range done {
		e.State = stateDone
	}
	msg := s.startReady(ctx, r, by)
	if countState(r, stateDone) < len(r.Entries) {
		return fmt.Sprintf("%s: %d of %d done", msg, countState(r, stateDone), len(r.Entries)), nil
	}
	finished := r.step().Name
	switch {
	case r.Step == len(r.Steps)-1:
		r.Status = stateDone
		return fmt.Sprintf("step %s done; workflow %s is complete", finished, r.Workflow), nil
	case r.Autopilot:
		r.Step++
		msg, err := s.startStep(ctx, r, by)
		return fmt.Sprintf("step %s done; autopilot: %s", finished, msg), err
	default:
		r.Status = stateWaiting
		return fmt.Sprintf("step %s done; next step %s waits for next-step on %s", finished, r.Steps[r.Step+1].Name, r.owner()), nil
	}
}

func (s *Service) advance(ctx context.Context, n tree.Node, args json.RawMessage, by tree.Actor) (tree.Result, error) {
	var a struct{ Step string }
	json.Unmarshal(args, &a)
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.runs(ctx, slugOf(n.Addr))
	if err != nil {
		return tree.Result{}, err
	}
	r, done := pick(list, slugOf(n.Addr), by)
	if r == nil {
		return tree.Result{Message: fmt.Sprintf("%s is not working on a running workflow step here; nothing to advance", by)}, nil
	}
	if a.Step != "" && a.Step != r.step().Name {
		return tree.Result{Message: fmt.Sprintf("workflow %s is at step %s, not %s; nothing to advance", r.Workflow, r.step().Name, a.Step)}, nil
	}
	msg, err := s.finish(ctx, r, done, by)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Addr: r.owner(), Message: msg}, s.save(ctx, r, by)
}

// pick returns the running run and the entries an advance by actor on slug finishes:
// a session finishes its own entry, anyone else finishes the entry of slug, or the whole step on the run's task.
func pick(list []*run, slug string, by tree.Actor) (*run, []*entry) {
	for _, r := range list {
		if r.Status != stateRunning {
			continue
		}
		if by.Kind == "session" {
			if i := slices.IndexFunc(r.Entries, func(e *entry) bool { return e.Session == by.ID && e.State == stateRunning }); i >= 0 {
				return r, r.Entries[i : i+1]
			}
			continue
		}
		if r.Slug == slug {
			return r, r.Entries
		}
		if e := r.entry(slug); e != nil {
			return r, []*entry{e}
		}
	}
	return nil, nil
}

func (s *Service) nextStep(ctx context.Context, n tree.Node, _ json.RawMessage, by tree.Actor) (tree.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.current(ctx, slugOf(n.Addr))
	if err != nil {
		return tree.Result{}, err
	}
	if r == nil || r.Status != stateWaiting {
		return tree.Result{}, errors.New("no workflow is waiting for its next step")
	}
	r.Step++
	r.Status = stateRunning
	msg, err := s.startStep(ctx, r, by)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{Addr: r.owner(), Message: msg}, s.save(ctx, r, by)
}

func (s *Service) stop(ctx context.Context, n tree.Node, _ json.RawMessage, by tree.Actor) (tree.Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.current(ctx, slugOf(n.Addr))
	if err != nil {
		return tree.Result{}, err
	}
	if r == nil || !r.active() {
		return tree.Result{}, errors.New("no workflow is running")
	}
	r.Status = stateStopped
	return tree.Result{Addr: r.owner(), Message: fmt.Sprintf("stopped workflow %s at step %s", r.Workflow, r.step().Name)}, s.save(ctx, r, by)
}
