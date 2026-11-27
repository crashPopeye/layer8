package codex

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
)

// TestRealCodex runs Probe and one approved command against the installed codex.
// It runs only with LAYER8_REAL_CODEX=1 and calls a model once.
func TestRealCodex(t *testing.T) {
	if os.Getenv("LAYER8_REAL_CODEX") == "" {
		t.Skip("set LAYER8_REAL_CODEX=1 to run against the installed codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	dir := t.TempDir()
	start := time.Now()
	c, err := Harness{}.Probe(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("probe in %s: %d skills, %d models, %d mcp servers", time.Since(start).Round(time.Millisecond), len(c.Commands), len(c.Models), len(c.MCPServers))
	for _, m := range c.Models {
		t.Logf("model %s (%s) efforts=%v", m.Value, m.DisplayName, m.Efforts)
	}
	for _, s := range c.MCPServers {
		t.Logf("mcp %s: %s", s.Name, s.Status)
	}

	rec := newRecorder(func(harness.PermissionRequest) harness.Decision { return harness.Decision{Allow: true} })
	p, err := Harness{}.Start(ctx, harness.StartOptions{Cwd: dir, Model: os.Getenv("LAYER8_REAL_CODEX_MODEL"), Effort: "low",
		AddDirs: []string{t.TempDir()}, AppendSystemPrompt: "End every reply with the word LAYER8."}, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(5 * time.Second)
	p.Send(harness.UserMessage{Text: "Run the shell command `mkdir -p sub && echo hi > sub/hello.txt`, then reply in one short sentence."})
	for _, e := range rec.until(t, harness.EventResult, 2*time.Minute) {
		if e.Partial {
			continue
		}
		t.Logf("%s %v", e.Type, e.Data)
	}
	select {
	case req := <-rec.perms:
		t.Logf("approval asked for %s %s", req.Tool, req.Input)
	default:
		t.Log("no approval asked")
	}
	if b, err := os.ReadFile(dir + "/sub/hello.txt"); err != nil || strings.TrimSpace(string(b)) != "hi" {
		t.Fatalf("sub/hello.txt = %q %v", b, err)
	}
}
