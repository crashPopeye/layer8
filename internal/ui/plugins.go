package ui

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/tree"
)

//go:embed sdk.md
var sdkDoc string

var pluginFile = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*\.js$`)

type report struct {
	stamp    string
	Error    string   `json:"error,omitempty"`
	Panels   []string `json:"panels,omitempty"`
	Commands []string `json:"commands,omitempty"`
}

// Plugin is one file of the plugins folder, with what the window reported after loading it.
type Plugin struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	URL    string `json:"url"`
	Stamp  string `json:"stamp"`
	Status string `json:"status"`
	report
}

func (s *Service) stamps() map[string]string {
	out := map[string]string{}
	entries, _ := os.ReadDir(s.path("plugins"))
	for _, e := range entries {
		if info, err := e.Info(); err == nil && pluginFile.MatchString(e.Name()) && info.Mode().IsRegular() {
			out[strings.TrimSuffix(e.Name(), ".js")] = fmt.Sprintf("%d/%d", info.ModTime().UnixNano(), info.Size())
		}
	}
	return out
}

// Plugins lists the plugin files, sorted by name.
func (s *Service) Plugins() []Plugin {
	stamps := s.stamps()
	s.mu.Lock()
	defer s.mu.Unlock()
	list := []Plugin{}
	for _, name := range slices.Sorted(maps.Keys(stamps)) {
		p := Plugin{Name: name, File: s.path("plugins", name+".js"), URL: "/api/plugins/" + name + ".js", Stamp: stamps[name], Status: "pending"}
		if r, ok := s.reports[name]; ok && r.stamp == p.Stamp {
			p.report, p.Status = r, "loaded"
			if r.Error != "" {
				p.Status = "failed"
			}
		}
		list = append(list, p)
	}
	return list
}

func (s *Service) pluginFields() (map[string]any, error) {
	return map[string]any{"dir": s.path("plugins"), "plugins": s.Plugins(), "sdk": sdkDoc}, nil
}

func (s *Service) pluginActions() []tree.Action {
	when := on("plugins")
	return []tree.Action{
		{Name: "reload", Help: "Reload every plugin in the window", When: when, Run: s.window("reload-plugins")},
		{Name: "report", Help: "Record what a plugin registered or why it failed; the window calls this after loading it", When: when, Run: s.report,
			Args: json.RawMessage(`{"type":"object","required":["plugin"],"properties":{
				"plugin":{"type":"string"},"error":{"type":"string"},
				"panels":{"type":"array","items":{"type":"string"}},"commands":{"type":"array","items":{"type":"string"}}}}`)},
	}
}

func (s *Service) report(_ context.Context, _ tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	a, err := decode[struct {
		Plugin string
		report
	}](args)
	if err != nil {
		return tree.Result{}, err
	}
	stamp, ok := s.stamps()[a.Plugin]
	if !ok {
		return tree.Result{}, fmt.Errorf("no plugin %q", a.Plugin)
	}
	a.report.stamp = stamp
	s.mu.Lock()
	s.reports[a.Plugin] = a.report
	s.mu.Unlock()
	return tree.Result{Message: "recorded"}, nil
}

// ServePlugin serves GET /api/plugins/{file}: the plugin's JavaScript source.
func (s *Service) ServePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !pluginFile.MatchString(name) {
		http.Error(w, "bad plugin name", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(filepath.Join(s.path("plugins"), name))
	if err != nil {
		http.Error(w, "no plugin "+name, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(data)
}

// Watch publishes a change to ui:plugins whenever a plugin file is added, changed or removed, until ctx ends.
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
		if now := s.stamps(); !maps.Equal(now, seen) {
			seen = now
			s.tree.Changed("ui:plugins")
		}
	}
}
