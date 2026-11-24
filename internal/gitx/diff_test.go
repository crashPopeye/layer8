package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestChanges(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init", "-q", "-b", "main")
	write(t, filepath.Join(dir, "old.go"), "package a\n\nfunc A() int { return 1 }\n\nfunc B() int { return 2 }\n")
	write(t, filepath.Join(dir, "edit.txt"), "one\ntwo\nthree\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-qm", "base")
	git(t, dir, "switch", "-q", "-c", "l8/work")

	git(t, dir, "mv", "old.go", "new.go")
	write(t, filepath.Join(dir, "new.go"), "package a\n\nfunc A() int { return 1 }\n\nfunc B() int { return 3 }\n")
	git(t, dir, "commit", "-qam", "rename")
	write(t, filepath.Join(dir, "edit.txt"), "one\n2\nthree\nfour\n")
	write(t, filepath.Join(dir, "notes.md"), "a\nb\n")

	c, err := Git{}.Changes(context.Background(), dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.MergeBase) != 40 || c.Base != "main" {
		t.Fatalf("base = %q, merge-base = %q", c.Base, c.MergeBase)
	}
	got := map[string]File{}
	for _, f := range c.Files {
		got[f.NewPath] = f
	}
	if len(got) != 3 {
		t.Fatalf("files = %+v", c.Files)
	}
	want := []struct {
		path, old, status string
		adds, dels        int
		untracked         bool
	}{
		{"new.go", "old.go", "renamed", 1, 1, false},
		{"edit.txt", "edit.txt", "modified", 2, 1, false},
		{"notes.md", "notes.md", "added", 2, 0, true},
	}
	for _, w := range want {
		f := got[w.path]
		if f.OldPath != w.old || f.Status != w.status || f.Adds != w.adds || f.Dels != w.dels || f.Untracked != w.untracked {
			t.Errorf("%s = %+v", w.path, f)
		}
	}
	lines := got["edit.txt"].Hunks[0].Lines
	if l := lines[len(lines)-1]; l.Kind != Add || l.New != 4 || l.Text != "four" {
		t.Errorf("last line of edit.txt = %+v", l)
	}
	if l := got["notes.md"].Hunks[0].Lines[1]; l.Kind != Add || l.New != 2 || l.Text != "b" {
		t.Errorf("untracked line = %+v", l)
	}
}

func TestParseQuotedAndBinary(t *testing.T) {
	out := strings.Join([]string{
		`diff --git "a/sp ace\tx.txt" "b/sp ace\tx.txt"`,
		"deleted file mode 100644",
		"--- \"a/sp ace\\tx.txt\"",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-gone",
		`\ No newline at end of file`,
		"diff --git a/img.png b/img.png",
		"new file mode 100644",
		"Binary files /dev/null and b/img.png differ",
		"",
	}, "\n")
	files := Parse(out)
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if f := files[0]; f.OldPath != "sp ace\tx.txt" || f.Status != "deleted" || f.Dels != 1 || f.Hunks[0].Lines[0].Old != 1 {
		t.Errorf("quoted = %+v", f)
	}
	if f := files[1]; !f.Binary || f.Status != "added" {
		t.Errorf("binary = %+v", f)
	}
}
