package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/daemon"
	"github.com/crashPopeye/layer8/internal/store"
)

func TestCLIGetAndDo(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := httptest.NewServer(daemon.Build(t.Context(), config.Config{Home: t.TempDir()}, db, "tok", "test"))
	defer ts.Close()
	c := client{url: ts.URL, token: "tok"}
	repo := filepath.Join(t.TempDir(), "api-gateway")
	if out, err := exec.Command("git", "init", "-q", "-b", "main", repo).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}

	run := func(f func([]string, *bytes.Buffer) error, args ...string) (string, error) {
		var out bytes.Buffer
		err := f(args, &out)
		return out.String(), err
	}
	get := func(args []string, w *bytes.Buffer) error { return c.get(args, w) }
	do := func(args []string, w *bytes.Buffer) error { return c.do(args, w) }

	out, err := run(do, "app", "add-project", "path="+repo, "name=gw")
	if err != nil || out != "ok project:gw: added project gw\n" {
		t.Fatalf("do add-project = %q, %v", out, err)
	}

	out, err = run(get)
	if err != nil || !strings.Contains(out, "app  layer8") || !strings.Contains(out, "  project:gw  gw") || !strings.Contains(out, "add-project {path, name?}") {
		t.Fatalf("get app = %q, %v", out, err)
	}

	out, err = run(get, "project:gw", "--json")
	var node struct {
		Addr   string
		Fields map[string]string
	}
	if err != nil || json.Unmarshal([]byte(out), &node) != nil || node.Addr != "project:gw" || node.Fields["defaultBranch"] != "main" {
		t.Fatalf("get --json = %q, %v", out, err)
	}

	out, err = run(do, "--json", `[{"target":"project:gw","action":"rename","args":{"name":"gateway"}},{"target":"project:gw","action":"remove"}]`)
	if err == nil || !strings.Contains(out, "ok project:gateway: renamed gw to gateway") || !strings.Contains(out, "error project:gw: not found") {
		t.Fatalf("do --json = %q, %v", out, err)
	}

	if _, err := run(get, "project:ghost"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("get ghost err = %v", err)
	}
	if _, err := run(do, "project:gateway", "rename", "name:=42"); err == nil {
		t.Fatal("rename with a number should fail validation")
	}

	var actor string
	db.QueryRow(`SELECT DISTINCT actor_kind FROM actions_log`).Scan(&actor)
	if actor != "cli" {
		t.Fatalf("logged actor = %q", actor)
	}
}

func TestParseOps(t *testing.T) {
	ops, err := parseOps([]string{"task:x", "start-session", "skill=research", "n:=3", "flags:=[\"a\"]", "q=a:=b"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(ops[0].Args); got != `{"flags":["a"],"n":3,"q":"a:=b","skill":"research"}` {
		t.Fatalf("args = %s", got)
	}
	if _, err := parseOps([]string{"app", "x", "bare"}); err == nil {
		t.Fatal("bare argument should fail")
	}
	if _, err := parseOps([]string{"app", "x", "n:={"}); err == nil {
		t.Fatal("invalid JSON should fail")
	}
}
