package diff

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseRealGitDiff(t *testing.T) {
	dir := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\nthree\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "gone.txt"), []byte("bye\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "old name.txt"), []byte("same content here\nline2\nline3\n"), 0o644)
	git("add", "-A")
	git("commit", "-qm", "init")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n2\nthree\nfour\n"), 0o644)
	os.Remove(filepath.Join(dir, "gone.txt"))
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("hello\n"), 0o644)
	git("mv", "old name.txt", "new name.txt")
	git("add", "-A")
	_ = context.Background()

	files := Parse(git("diff", "--cached", "--no-color", "--no-ext-diff", "-M"))
	byPath := map[string]File{}
	for _, f := range files {
		byPath[f.Path()] = f
	}
	a := byPath["a.txt"]
	if a.Status != "modified" || a.Adds != 2 || a.Dels != 1 {
		t.Fatalf("a.txt %+v", a)
	}
	var added []Line
	for _, l := range a.Hunks[0].Lines {
		if l.Kind == Add {
			added = append(added, l)
		}
	}
	if added[0].New != 2 || added[0].Text != "2" || added[1].New != 4 {
		t.Fatalf("line numbers %+v", added)
	}
	if byPath["gone.txt"].Status != "deleted" || byPath["new.txt"].Status != "added" {
		t.Fatalf("statuses %+v", byPath)
	}
	r := byPath["new name.txt"]
	if r.Status != "renamed" || r.OldPath != "old name.txt" {
		t.Fatalf("rename %+v", r)
	}
}

func TestHunkHeader(t *testing.T) {
	o, n := parseHunkHeader("@@ -10,3 +12 @@ func x()")
	if o != 10 || n != 12 {
		t.Fatal(o, n)
	}
}
