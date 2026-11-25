package task

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestMatchFiles(t *testing.T) {
	files := []string{"internal/store/store.go", "cmd/l8/main.go", "internal/session/store_test.go", "README.md", "ui/js/stores.js", "docs/restore.md"}
	got := matchFiles(files, "Store", 10)
	want := []string{"ui/js/stores.js", "internal/store/store.go", "internal/session/store_test.go", "docs/restore.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("matchFiles = %v, want %v", got, want)
	}
	if got := matchFiles(files, "", 2); len(got) != 2 {
		t.Fatalf("empty query, limit 2 = %v", got)
	}
}

func TestFilesListsTrackedAndUntracked(t *testing.T) {
	ctx := context.Background()
	svc := service(t)
	p, err := svc.AddProject(ctx, repo(t))
	if err != nil {
		t.Fatal(err)
	}
	task, err := svc.Create(ctx, CreateInput{ProjectID: p.ID, Title: "Files"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(task.Worktree, "notes.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Files(ctx, task.Slug, ".md")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"notes.md", "README.md"}) {
		t.Fatalf("Files = %v", got)
	}
}
