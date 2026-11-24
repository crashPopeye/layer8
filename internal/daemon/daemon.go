// Package daemon wires layer8's state, services and HTTP API together.
package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/api"
	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/gitx"
	"github.com/crashPopeye/layer8/internal/session"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/task"
	"github.com/crashPopeye/layer8/plugin"
	"github.com/crashPopeye/layer8/ui"
)

// Run serves the API on addr with all state under home until ctx is done.
func Run(ctx context.Context, home, addr string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	token, err := ensureToken(client.TokenPath(home))
	if err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(home, "layer8.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	pluginDir, err := plugin.Materialize(filepath.Join(home, "plugin"))
	if err != nil {
		return err
	}

	hub := api.NewHub()
	tasks := task.NewService(st, gitx.Git{}, home, hub.Notify)
	sessions := session.NewManager(st, hub.Publish, session.Claude, session.Config{
		PluginDir:     pluginDir,
		ArtifactsRoot: filepath.Join(home, "artifacts"),
		LogDir:        filepath.Join(home, "logs"),
		Env:           sessionEnv(home, addr),
	})
	srv := &http.Server{
		Handler:     api.New(st, hub, tasks, sessions, token, ui.FS),
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	log.Printf("layer8 %s on http://%s, state in %s", api.Version, addr, home)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// ensureToken returns the API token stored at path, creating it on first run.
func ensureToken(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(b))) > 0 {
		return strings.TrimSpace(string(b)), nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)
	return token, os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// sessionEnv points every session's l8 at this daemon, with the l8 next to this binary first on PATH.
func sessionEnv(home, addr string) []string {
	env := []string{"LAYER8_HOME=" + home, "LAYER8_ADDR=" + addr}
	if exe, err := os.Executable(); err == nil {
		env = append(env, "PATH="+filepath.Dir(exe)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return env
}
