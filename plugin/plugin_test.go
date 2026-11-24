package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
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
