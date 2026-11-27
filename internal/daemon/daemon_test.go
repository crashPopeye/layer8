package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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
	ts := httptest.NewServer(Build(t.Context(), cfg, db, token, "v1"))
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

func TestEnsureLetsABusyDaemonOfAnotherVersionFinish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("LAYER8_HOME", filepath.Join(home, ".layer8"))
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	token, _ := cfg.Token()
	var restarts atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"version":"v1"}`)) })
	mux.Handle("POST /api/restart", &restarter{busy: func() bool { restarts.Add(1); return true }, stop: func() {}, every: time.Hour})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	cfg.Addr = ts.Listener.Addr().String()

	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)
	agents := filepath.Join(home, "Library", "LaunchAgents")
	os.MkdirAll(agents, 0o755)
	os.WriteFile(filepath.Join(agents, Label+".plist"), []byte(Plist(exe, cfg)), 0o644)

	got, err := Ensure(cfg, "v2")
	if err != nil || got != token {
		t.Fatalf("Ensure = %q, %v", got, err)
	}
	if restarts.Load() == 0 {
		t.Fatal("Ensure did not ask the old daemon to restart")
	}
}

func TestRestarterStopsOnceIdle(t *testing.T) {
	var mu sync.Mutex
	busy := true
	stopped := make(chan struct{})
	rs := &restarter{
		busy:  func() bool { mu.Lock(); defer mu.Unlock(); return busy },
		stop:  func() { close(stopped) },
		every: time.Millisecond,
	}
	for range 2 {
		rec := httptest.NewRecorder()
		rs.ServeHTTP(rec, httptest.NewRequest("POST", "/api/restart", nil))
		if rec.Code != http.StatusAccepted || strings.TrimSpace(rec.Body.String()) != `{"busy":true}` {
			t.Fatalf("restart = %d %s", rec.Code, rec.Body)
		}
	}
	select {
	case <-stopped:
		t.Fatal("stopped while busy")
	case <-time.After(20 * time.Millisecond):
	}
	mu.Lock()
	busy = false
	mu.Unlock()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("not stopped once idle")
	}
}
