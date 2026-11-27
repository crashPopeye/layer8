package plugin

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var wantSkills = []string{"address-comments", "describe-pr", "fix-ci", "implement", "layer8", "make-panel", "plan", "prd",
	"research", "research-questions", "review", "review-comments", "tdd"}

// frontmatter returns the YAML header of a SKILL.md.
func frontmatter(t *testing.T, data []byte) map[string]any {
	t.Helper()
	rest, ok := strings.CutPrefix(string(data), "---\n")
	head, _, ok2 := strings.Cut(rest, "\n---\n")
	if !ok || !ok2 {
		t.Fatal("no frontmatter")
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(head), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestManifestAndSkills(t *testing.T) {
	var manifest struct{ Name, Version, Description string }
	data, err := fs.ReadFile(Files, ".claude-plugin/plugin.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &manifest); err != nil || manifest.Name != "layer8" || manifest.Description == "" {
		t.Fatalf("plugin.json = %+v, %v", manifest, err)
	}
	dirs, _ := fs.ReadDir(Files, "skills")
	var names []string
	for _, d := range dirs {
		names = append(names, d.Name())
		data, err := fs.ReadFile(Files, "skills/"+d.Name()+"/SKILL.md")
		if err != nil {
			t.Fatal(err)
		}
		fm := frontmatter(t, data)
		if fm["name"] != d.Name() {
			t.Errorf("%s: name = %v", d.Name(), fm["name"])
		}
		if desc, _ := fm["description"].(string); len(desc) < 40 {
			t.Errorf("%s: description too short: %q", d.Name(), desc)
		}
		for k := range fm {
			if !slices.Contains([]string{"name", "description", "argument-hint"}, k) {
				t.Errorf("%s: unexpected frontmatter key %q", d.Name(), k)
			}
		}
	}
	if !slices.Equal(names, wantSkills) {
		t.Fatalf("skills = %v, want %v", names, wantSkills)
	}
}

func TestInstallAndMirrorCodex(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "plugin")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "stale.txt"), nil, 0o644)
	if err := Install(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "stale.txt")); err == nil {
		t.Fatal("Install kept a stale file")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatal(err)
	}

	skills := t.TempDir()
	os.MkdirAll(filepath.Join(skills, "layer8-gone"), 0o755)
	os.MkdirAll(filepath.Join(skills, "mine"), 0o755)
	if err := MirrorCodex(os.DirFS(dir), skills); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(skills)
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	want := []string{"mine"}
	for _, s := range wantSkills {
		want = append(want, CodexName(s))
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("codex skills = %v, want %v", got, want)
	}
	data, _ := os.ReadFile(filepath.Join(skills, "layer8-plan", "SKILL.md"))
	if fm := frontmatter(t, data); fm["name"] != "layer8-plan" {
		t.Fatalf("mirrored name = %v", fm["name"])
	}
}
