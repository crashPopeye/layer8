package plugin

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestMaterialize(t *testing.T) {
	root := t.TempDir()
	dir, err := Materialize(root)
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct{ Name string }
	b, err := os.ReadFile(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(b, &manifest) != nil || manifest.Name != "l8" {
		t.Fatalf("plugin.json = %s, %v", b, err)
	}
	skill := filepath.Join(dir, "skills", "orchestrate", "SKILL.md")
	if err := os.WriteFile(skill, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if again, err := Materialize(root); err != nil || again != dir {
		t.Fatalf("second Materialize = %s, %v", again, err)
	}
	if b, _ := os.ReadFile(skill); string(b) != "edited" {
		t.Fatal("Materialize overwrote an in-place edit")
	}
}

func TestSkillsAreLayer8Only(t *testing.T) {
	forbidden := []string{"humanlayer ", ".humanlayer/tasks", "cloud.humanlayer.com"}
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := FS.ReadFile(path)
		for _, s := range forbidden {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("%s contains %q", path, s)
			}
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReferencesResolve(t *testing.T) {
	ref := regexp.MustCompile(`\bl8:([a-z-]+)`)
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		b, err := FS.ReadFile(path)
		for _, m := range ref.FindAllSubmatch(b, -1) {
			name := string(m[1])
			_, skill := fs.Stat(FS, "skills/"+name+"/SKILL.md")
			_, agent := fs.Stat(FS, "agents/"+name+".md")
			if skill != nil && agent != nil {
				t.Errorf("%s refers to l8:%s, which is neither a bundled skill nor agent", path, name)
			}
		}
		if dir, ok := strings.CutSuffix(path, "/SKILL.md"); ok && !bytes.Contains(b, []byte("\nname: "+filepath.Base(dir)+"\n")) {
			t.Errorf("%s is not named %s", path, filepath.Base(dir))
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
