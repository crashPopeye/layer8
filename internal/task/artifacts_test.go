package task

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/store"
)

func bareTask(t *testing.T, svc *Service) store.Task {
	t.Helper()
	if err := svc.store.InsertProject(store.Project{ID: "p1", Name: "layer8", Path: "/w/layer8", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	task := store.Task{ID: "t1", ProjectID: "p1", Slug: "smoke", Title: "Smoke", Status: "active", CreatedAt: 1}
	if err := svc.store.InsertTask(task); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestArtifactPathsStayInside(t *testing.T) {
	svc := service(t)
	bareTask(t, svc)
	for _, p := range []string{"../t2/task.md", "/etc/passwd", "a/../../x", ""} {
		if _, err := svc.ReadArtifact("t1", p); !errors.Is(err, ErrInvalid) {
			t.Errorf("read %q: err = %v", p, err)
		}
		if _, err := svc.WriteArtifact("t1", p, []byte("x")); !errors.Is(err, ErrInvalid) {
			t.Errorf("write %q: err = %v", p, err)
		}
	}
	if _, err := svc.WriteArtifact("t1", "parent/03-prd.md", []byte("x")); !errors.Is(err, ErrInvalid) {
		t.Errorf("write under parent/: err = %v", err)
	}
	if _, err := svc.WriteArtifact("t1", "notes/a.md", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if got, err := svc.ReadArtifact("smoke", "notes/a.md"); err != nil || string(got) != "hello" {
		t.Fatalf("read back = %q, %v", got, err)
	}
}

func TestArtifactsNewestFirstWithType(t *testing.T) {
	svc := service(t)
	task := bareTask(t, svc)
	dir := svc.ArtifactsDir(task.ID)
	files := map[string]string{
		"task.md":                    "# Smoke\n",
		"02-research-smoke.md":       "---\ndate: 2026-11-24\ntype: research\n---\n# Research\n",
		"03-prd-smoke.md":            "---\ntype: \"design-prd\"\n---\n# PRD\n",
		"mockup.html":                "<!doctype html>",
		".hidden.md":                 "---\ntype: secret\n---\n",
		filepath.Join("sub", "x.md"): "plain",
	}
	base := time.Now().Add(-time.Hour)
	order := []string{"task.md", "02-research-smoke.md", filepath.Join("sub", "x.md"), "03-prd-smoke.md", "mockup.html", ".hidden.md"}
	for i, name := range order {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(files[name]), 0o644); err != nil {
			t.Fatal(err)
		}
		mtime := base.Add(time.Duration(i) * time.Minute)
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "parent")); err != nil {
		t.Fatal(err)
	}

	list, err := svc.Artifacts("t1")
	if err != nil {
		t.Fatal(err)
	}
	var got [][2]string
	for _, a := range list {
		got = append(got, [2]string{a.Path, a.Type})
	}
	want := [][2]string{
		{"mockup.html", "html"},
		{"03-prd-smoke.md", "design-prd"},
		{"sub/x.md", "md"},
		{"02-research-smoke.md", "research"},
		{"task.md", "task"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("artifacts = %v\nwant %v", got, want)
	}
}

func TestSendComments(t *testing.T) {
	svc := service(t)
	bareTask(t, svc)
	first, err := svc.AddComment("t1", CommentInput{Path: "03-prd.md", Line: 42, Quote: "Sessions survive restarts.", Body: "Say how long a resume takes."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddComment("t1", CommentInput{Path: "03-prd.md", Line: 7, Body: "Name the owner."}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddComment("t1", CommentInput{Path: "03-prd.md", Body: " "}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty body err = %v", err)
	}

	var sentTo, message string
	send := func(_ context.Context, sessionID, text string) error {
		sentTo, message = sessionID, text
		return nil
	}
	n, err := svc.SendComments(context.Background(), "t1", SendInput{SessionID: "s1", IDs: []string{first.ID}}, send)
	if err != nil || n != 1 {
		t.Fatalf("send one = %d, %v", n, err)
	}
	want := "Please address these review comments:\n\n.layer8/task/03-prd.md:42\n> Sessions survive restarts.\nSay how long a resume takes.\n"
	if sentTo != "s1" || message != want {
		t.Fatalf("sent to %q:\n%q\nwant\n%q", sentTo, message, want)
	}

	n, err = svc.SendComments(context.Background(), "t1", SendInput{SessionID: "s2", Resolve: true}, send)
	if err != nil || n != 1 {
		t.Fatalf("send all = %d, %v", n, err)
	}
	if want := "Please address these review comments:\n\n.layer8/task/03-prd.md:7\nName the owner.\n"; message != want {
		t.Fatalf("second message:\n%q", message)
	}
	comments, _ := svc.Comments("t1")
	statuses := map[int]string{}
	for _, c := range comments {
		statuses[c.Line] = c.Status
	}
	if statuses[42] != "sent" || statuses[7] != "resolved" {
		t.Fatalf("statuses = %v", statuses)
	}
	if _, err := svc.SendComments(context.Background(), "t1", SendInput{SessionID: "s2"}, send); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nothing open: err = %v", err)
	}
}
