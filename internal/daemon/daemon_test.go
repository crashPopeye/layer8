package daemon

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/store"
)

func TestPlist(t *testing.T) {
	cfg := config.Config{Home: "/Users/a&b/.layer8"}
	p := Plist("/Applications/layer8.app/Contents/MacOS/layer8d", cfg)
	for _, want := range []string{
		"<string>dev.layer8.daemon</string>",
		"<array><string>/Applications/layer8.app/Contents/MacOS/layer8d</string><string>serve</string></array>",
		"<key>LAYER8_HOME</key><string>/Users/a&amp;b/.layer8</string>",
		"<string>/Users/a&amp;b/.layer8/logs/daemon.log</string>",
		"<key>KeepAlive</key><true/>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("plist missing %q:\n%s", want, p)
		}
	}
}

func TestEnsureLeavesAHealthyDaemonAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LAYER8_HOME", filepath.Join(home, ".layer8"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	token, _ := cfg.Token()
	db, err := store.Open(cfg.Path("layer8.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts := httptest.NewServer(Build(db, token, "v1"))
	defer ts.Close()
	cfg.Addr = ts.Listener.Addr().String()

	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	agents := filepath.Join(home, "Library", "LaunchAgents")
	os.MkdirAll(agents, 0o755)
	os.WriteFile(filepath.Join(agents, Label+".plist"), []byte(Plist(exe, cfg)), 0o644)

	got, err := Ensure(cfg, "v1")
	if err != nil || got != token {
		t.Fatalf("Ensure = %q, %v", got, err)
	}
}
