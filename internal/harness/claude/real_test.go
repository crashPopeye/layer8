package claude

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/harness"
)

// TestRealClaude runs Probe and one approved tool call against the installed claude.
// It runs only with LAYER8_REAL_CLAUDE=1 and calls a model once.
func TestRealClaude(t *testing.T) {
	if os.Getenv("LAYER8_REAL_CLAUDE") == "" {
		t.Skip("set LAYER8_REAL_CLAUDE=1 to run against the installed claude")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	start := time.Now()
	c, err := Harness{}.Probe(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("probe in %s: %d commands, %d models, %d agents, %d mcp servers", time.Since(start).Round(time.Millisecond), len(c.Commands), len(c.Models), len(c.Agents), len(c.MCPServers))
	plugins := map[string]int{}
	for _, cmd := range c.Commands {
		plugins[cmd.Plugin]++
	}
	t.Logf("commands per plugin: %v", plugins)
	for _, m := range c.Models {
		t.Logf("model %s (%s) efforts=%v", m.Value, m.DisplayName, m.Efforts)
	}
	for _, s := range c.MCPServers {
		t.Logf("mcp %s: %s (%s)", s.Name, s.Status, s.Source)
	}

	rec := newRecorder(func(harness.PermissionRequest) harness.Decision { return harness.Decision{Allow: true} })
	p, err := Harness{}.Start(ctx, harness.StartOptions{Cwd: dir, Model: "haiku", AppendSystemPrompt: "End every reply with the word LAYER8."}, rec)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(5 * time.Second)
	p.Send(harness.UserMessage{Text: "Use the Write tool to create hello.txt containing hi, then reply in one short sentence."})
	for _, e := range rec.until(t, harness.EventResult) {
		if e.Partial {
			continue
		}
		switch e.Type {
		case harness.EventSystem:
			t.Logf("system %v", e.Data["subtype"])
		case harness.EventResult:
			t.Logf("result cost=%v contextTokens=%v session=%v", e.Data["costUsd"], e.Data["contextTokens"], e.Data["sessionId"])
		default:
			t.Logf("%s %v", e.Type, e.Data)
		}
	}
	req := <-rec.perms
	t.Logf("approval asked for %s %s", req.Tool, req.Input)
	if b, err := os.ReadFile(dir + "/hello.txt"); err != nil || string(b) != "hi" && string(b) != "hi\n" {
		t.Fatalf("hello.txt = %q %v", b, err)
	}
}
