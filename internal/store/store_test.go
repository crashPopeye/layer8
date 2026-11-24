package store

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func schema(t *testing.T, s *Store) (int, []string) {
	t.Helper()
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	rows, err := s.db.Query(`SELECT sql FROM sqlite_master WHERE sql IS NOT NULL ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ddl []string
	for rows.Next() {
		var sql string
		if err := rows.Scan(&sql); err != nil {
			t.Fatal(err)
		}
		ddl = append(ddl, sql)
	}
	return version, ddl
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "layer8.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	v1, ddl1 := schema(t, s)
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	v2, ddl2 := schema(t, s)
	if v1 != len(migrations) || v2 != v1 || !reflect.DeepEqual(ddl1, ddl2) {
		t.Fatalf("second open changed the schema: v%d → v%d\n%v\n%v", v1, v2, ddl1, ddl2)
	}
}

func TestRoundTrip(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	p := Project{ID: "p1", Name: "layer8", Path: "/w/layer8", BaseBranch: "main", Instructions: "Use Go.", CreatedAt: 1}
	if err := s.InsertProject(p); err != nil {
		t.Fatal(err)
	}
	projects, err := s.Projects()
	if err != nil || !reflect.DeepEqual(projects, []Project{p}) {
		t.Fatalf("projects = %+v, %v", projects, err)
	}

	older := Task{ID: "t1", ProjectID: "p1", Slug: "smoke", Title: "Smoke", Workflow: "oneshot", Status: "active",
		Branch: "l8/smoke", BaseRef: "origin/main", Worktree: "/ws/smoke", Autonomous: true, CreatedAt: 10}
	newer := Task{ID: "t2", ProjectID: "p1", ParentID: "t1", Slug: "child", Title: "Child", Status: "done", CreatedAt: 20}
	for _, task := range []Task{older, newer} {
		if err := s.InsertTask(task); err != nil {
			t.Fatal(err)
		}
	}

	all, err := s.ListTasks(TaskFilter{ProjectID: "p1"})
	if err != nil || !reflect.DeepEqual(all, []Task{newer, older}) {
		t.Fatalf("tasks = %+v, %v", all, err)
	}
	active, err := s.ListTasks(TaskFilter{Status: "active"})
	if err != nil || !reflect.DeepEqual(active, []Task{older}) {
		t.Fatalf("active = %+v, %v", active, err)
	}
	bySlug, err := s.Task("smoke")
	if err != nil || bySlug != older {
		t.Fatalf("by slug = %+v, %v", bySlug, err)
	}
	if err := s.SetTaskStatus("t1", "archived"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Task("t1"); got.Status != "archived" {
		t.Fatalf("status = %q", got.Status)
	}
	if _, err := s.Task("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing task err = %v", err)
	}
}
