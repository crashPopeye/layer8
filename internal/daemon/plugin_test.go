package daemon

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tree"
	"github.com/crashPopeye/layer8/internal/workflows"
	"github.com/crashPopeye/layer8/plugin"
)

// pending are actions the skills use that other work items add to the tree.
var pending = map[string]string{
	"open-panel": "WI-10",
}

var actionRef = regexp.MustCompile(`"action"\s*:\s*"([a-z0-9-]+)"`)

func TestSkillsUseActionsTheTreeHas(t *testing.T) {
	home := t.TempDir()
	db, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tr, _ := build(t.Context(), config.Config{Home: home, Addr: "127.0.0.1:0"}, db, "tok", "test", func() {})
	actions := map[string]bool{}
	for _, kind := range tr.Kinds() {
		for _, a := range tr.Actions(kind) {
			actions[a.Name] = true
		}
	}

	skills, used := map[string]bool{}, map[string]bool{}
	err = fs.WalkDir(plugin.Files, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		skills[strings.Split(p, "/")[1]] = true
		data, err := fs.ReadFile(plugin.Files, p)
		for _, m := range actionRef.FindAllStringSubmatch(string(data), -1) {
			used[m[1]] = true
			if !actions[m[1]] && pending[m[1]] == "" {
				t.Errorf("%s uses action %q, which no kind declares", p, m[1])
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !used["advance"] || !used["create-subtask"] {
		t.Fatalf("found no advance or create-subtask in the skills: %v", used)
	}

	app, err := tr.Get(t.Context(), tree.Actor{Kind: "user"}, "app", 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range app.Children {
		if c.Kind != "workflow" {
			continue
		}
		n, err := tr.Get(t.Context(), tree.Actor{Kind: "user"}, c.Addr, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, st := range n.Fields["steps"].([]workflows.Step) {
			if name, ok := strings.CutPrefix(st.Skill, "layer8:"); !ok || !skills[name] {
				t.Errorf("%s step %s uses skill %q, which the plugin lacks", c.Addr, st.Name, st.Skill)
			}
		}
	}
}
