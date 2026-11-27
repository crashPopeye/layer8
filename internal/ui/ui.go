// Package ui provides the ui:layout, ui:theme and ui:plugins nodes: the window's dock layout and presets,
// its theme, and the JS plugins it loads.
package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sync"

	"github.com/crashPopeye/layer8/internal/tree"
)

// Options configures Register.
type Options struct {
	// Home holds ui.json, themes/ and plugins/.
	Home string
	// Publish sends a live event such as ui or toast to the window.
	Publish func(event string, data any)
}

// Service owns the ui kind and the files it persists.
type Service struct {
	tree    *tree.Tree
	o       Options
	mu      sync.Mutex
	reports map[string]report
}

type state struct {
	Layout  json.RawMessage            `json:"layout,omitempty"`
	Presets map[string]json.RawMessage `json:"presets,omitempty"`
	Theme   string                     `json:"theme,omitempty"`
	Mode    string                     `json:"mode,omitempty"`
	Accent  string                     `json:"accent,omitempty"`
}

var (
	namePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _-]{0,47}$`)
	addrs       = []tree.Addr{"ui:layout", "ui:theme", "ui:plugins"}
)

// BuiltinPresets are the layouts the window knows how to build without a saved layout.
var BuiltinPresets = []string{"Default", "Focus", "Review", "Wide diff"}

// Register adds the ui kind and lists its three nodes as children of app.
func Register(t *tree.Tree, o Options) *Service {
	s := &Service{tree: t, o: o, reports: map[string]report{}}
	actions := slices.Concat(s.layoutActions(), s.themeActions(), s.pluginActions())
	t.Register(tree.Kind{Name: "ui", Get: s.get, Actions: actions})
	t.AddChildren("app", func(context.Context, tree.Addr) ([]tree.Summary, error) {
		out := make([]tree.Summary, len(addrs))
		for i, a := range addrs {
			out[i] = tree.Summary{Addr: a, Kind: "ui", Title: a.Path()}
		}
		return out, nil
	})
	return s
}

func (s *Service) get(_ context.Context, a tree.Addr) (tree.Node, error) {
	st, err := s.load()
	if err != nil {
		return tree.Node{}, err
	}
	var fields map[string]any
	switch a.Path() {
	case "layout":
		fields = map[string]any{"layout": st.Layout, "presets": slices.Concat(BuiltinPresets, slices.Sorted(maps.Keys(st.Presets)))}
	case "theme":
		fields, err = s.themeFields(st)
	case "plugins":
		fields, err = s.pluginFields()
	default:
		return tree.Node{}, fmt.Errorf("%w: %s", tree.ErrNotFound, a)
	}
	return tree.Node{Title: a.Path(), Fields: fields}, err
}

func on(path string) func(tree.Node) bool {
	return func(n tree.Node) bool { return n.Addr.Path() == path }
}

func (s *Service) path(elem ...string) string {
	return filepath.Join(append([]string{s.o.Home}, elem...)...)
}

func (s *Service) load() (state, error) {
	var st state
	data, err := os.ReadFile(s.path("ui.json"))
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err == nil {
		err = json.Unmarshal(data, &st)
	}
	if err != nil {
		return st, fmt.Errorf("ui.json: %w", err)
	}
	return st, nil
}

func (s *Service) update(change func(*state) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.load()
	if err != nil {
		return err
	}
	if err := change(&st); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(s.path("ui.json"), data)
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func decode[T any](args json.RawMessage) (T, error) {
	var v T
	return v, json.Unmarshal(args, &v)
}

func (s *Service) layoutActions() []tree.Action {
	when := on("layout")
	return []tree.Action{
		{Name: "open-panel", Help: "Show a panel in the window: activate it when open, add it otherwise", When: when, Run: s.window("open-panel"),
			Args: json.RawMessage(`{"type":"object","required":["panel"],"properties":{
				"panel":{"type":"string","description":"Panel id: tasks, session, artifacts, changes, pr, files, theme, plugins, or one a plugin registered"},
				"position":{"enum":["left","center","right","right-bottom"],"description":"Where to add it when it is not open, defaults to the panel's own position"}}}`)},
		{Name: "focus", Help: "Make every panel follow this node", When: when, Run: s.window("focus"),
			Args: json.RawMessage(`{"type":"object","required":["addr"],"properties":{"addr":{"type":"string"}}}`)},
		{Name: "preset", Help: "Switch the window to a layout preset", When: when, Run: s.preset,
			Args: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string","description":"One of the presets field"}}}`)},
		{Name: "save-preset", Help: "Save the window's current layout as a named preset", When: when, Run: s.savePreset,
			Args: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)},
		{Name: "delete-preset", Help: "Delete a saved layout preset", When: when, Run: s.deletePreset,
			Args: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`)},
		{Name: "save-layout", Help: "Remember the window's current layout; the window calls this as the layout changes", When: when, Run: s.saveLayout,
			Args: json.RawMessage(`{"type":"object","required":["layout"],"properties":{"layout":{"type":"object"}}}`)},
		{Name: "notify", Help: "Show a notification in the window", When: when, Run: s.notify,
			Args: json.RawMessage(`{"type":"object","required":["text"],"properties":{"text":{"type":"string"},"level":{"enum":["info","success","warning","error"]}}}`)},
	}
}

func (s *Service) publish(event string, data any) {
	if s.o.Publish != nil {
		s.o.Publish(event, data)
	}
}

func (s *Service) window(verb string) func(context.Context, tree.Node, json.RawMessage, tree.Actor) (tree.Result, error) {
	return func(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
		cmd, err := decode[map[string]any](args)
		if err != nil {
			return tree.Result{}, err
		}
		cmd["verb"] = verb
		return s.command(cmd)
	}
}

func (s *Service) command(cmd map[string]any) (tree.Result, error) {
	s.publish("ui", cmd)
	return tree.Result{Message: fmt.Sprintf("sent %s to the window", cmd["verb"])}, nil
}

func (s *Service) preset(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct{ Name string }](args)
	if err != nil {
		return tree.Result{}, err
	}
	cmd := map[string]any{"verb": "preset", "name": a.Name}
	if !slices.Contains(BuiltinPresets, a.Name) {
		st, err := s.load()
		if err != nil {
			return tree.Result{}, err
		}
		layout, ok := st.Presets[a.Name]
		if !ok {
			return tree.Result{}, fmt.Errorf("no preset %q", a.Name)
		}
		cmd["layout"] = layout
	}
	return s.command(cmd)
}

func (s *Service) savePreset(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct{ Name string }](args)
	if err != nil {
		return tree.Result{}, err
	}
	if !namePattern.MatchString(a.Name) || slices.Contains(BuiltinPresets, a.Name) {
		return tree.Result{}, fmt.Errorf("preset name %q must be letters, digits, spaces, _ or -, and not a built-in", a.Name)
	}
	err = s.update(func(st *state) error {
		if st.Layout == nil {
			return errors.New("the window has not saved a layout yet")
		}
		if st.Presets == nil {
			st.Presets = map[string]json.RawMessage{}
		}
		st.Presets[a.Name] = st.Layout
		return nil
	})
	return tree.Result{Message: "saved preset " + a.Name}, err
}

func (s *Service) deletePreset(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct{ Name string }](args)
	if err != nil {
		return tree.Result{}, err
	}
	err = s.update(func(st *state) error {
		if _, ok := st.Presets[a.Name]; !ok {
			return fmt.Errorf("no saved preset %q", a.Name)
		}
		delete(st.Presets, a.Name)
		return nil
	})
	return tree.Result{Message: "deleted preset " + a.Name}, err
}

func (s *Service) saveLayout(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct{ Layout json.RawMessage }](args)
	if err != nil {
		return tree.Result{}, err
	}
	return tree.Result{}, s.update(func(st *state) error {
		st.Layout = a.Layout
		return nil
	})
}

func (s *Service) notify(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct{ Text, Level string }](args)
	if err != nil {
		return tree.Result{}, err
	}
	if a.Level == "" {
		a.Level = "info"
	}
	s.publish("toast", map[string]string{"level": a.Level, "text": a.Text})
	return tree.Result{Message: "notified"}, nil
}
