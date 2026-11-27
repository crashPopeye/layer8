package harness

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestClean(t *testing.T) {
	got := clean([]string{"PATH=/usr/bin", "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CLAUDE_CODE_SSE_PORT=1", "NODE_OPTIONS=--inspect", "ANTHROPIC_API_KEY=k", "CLAUDE_CONFIG_DIR=/c", ""})
	want := []string{"PATH=/usr/bin", "ANTHROPIC_API_KEY=k", "CLAUDE_CONFIG_DIR=/c"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestShellEnvHasPath(t *testing.T) {
	env := ShellEnv()
	if !slices.ContainsFunc(env, func(kv string) bool { return len(kv) > 5 && kv[:5] == "PATH=" }) {
		t.Fatalf("no PATH in %q", env)
	}
	if slices.ContainsFunc(env, func(kv string) bool { return kv == "CLAUDECODE=1" }) {
		t.Fatal("CLAUDECODE leaked")
	}
}

func TestLookPath(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(a, "claude"), nil, 0o644)
	os.WriteFile(filepath.Join(b, "claude"), nil, 0o755)
	got, err := LookPath("claude", []string{"PATH=" + a + ":" + b})
	if err != nil || got != filepath.Join(b, "claude") {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := LookPath("claude", []string{"PATH=" + a}); err == nil {
		t.Fatal("found a non-executable file")
	}
	if got, _ := LookPath("/abs/claude", nil); got != "/abs/claude" {
		t.Fatalf("got %q", got)
	}
}
