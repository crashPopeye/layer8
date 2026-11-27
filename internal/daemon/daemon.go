// Package daemon wires the daemon together, serves it, and installs it as a launchd agent.
package daemon

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/crashPopeye/layer8/internal/comments"
	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/diff"
	"github.com/crashPopeye/layer8/internal/github"
	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/harness/claude"
	"github.com/crashPopeye/layer8/internal/harness/codex"
	"github.com/crashPopeye/layer8/internal/mcp"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/server"
	"github.com/crashPopeye/layer8/internal/sessions"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
	"github.com/crashPopeye/layer8/internal/ui"
	"github.com/crashPopeye/layer8/internal/update"
	"github.com/crashPopeye/layer8/internal/workflows"
	"github.com/crashPopeye/layer8/plugin"
)

// Build assembles the tree, its kinds and the HTTP handler, and watches artifact, workflow and plugin folders until ctx ends.
func Build(ctx context.Context, cfg config.Config, db *sql.DB, token, version string) http.Handler {
	_, h := build(ctx, cfg, db, token, version, func() {})
	return h
}

func build(ctx context.Context, cfg config.Config, db *sql.DB, token, version string, stop func()) (*tree.Tree, http.Handler) {
	t := tree.New(db)
	hub := server.NewHub()
	t.OnChange = func(addrs []tree.Addr) { hub.Publish("node", server.NodeChange{Addrs: addrs}) }
	projects.Register(t, db, version)
	ts := tasks.Register(t, db, tasks.Options{Home: cfg.Home, Apps: cfg.Apps})
	go ts.Watch(ctx, 2*time.Second)
	var plugins []string
	if cfg.PluginDir != "" {
		plugins = []string{cfg.PluginDir}
	}
	ss := sessions.Register(t, db, ts, sessions.Options{
		Harnesses: map[string]harness.Harness{"claude": claude.Harness{PluginDirs: plugins}, "codex": codex.Harness{}},
		Max:       cfg.MaxSessions, MCPURL: cfg.URL() + "/mcp", Token: token, AutoApproveDo: cfg.AutoApproveDo,
		Terminal: cfg.Apps.Terminal, Publish: hub.Publish,
	})
	diff.Register(t)
	comments.Register(t, db)
	ghToken := github.GHToken(cfg.GitHubToken)
	gh := github.Register(t, db, &github.Client{API: cfg.GitHubAPI, HTTP: &http.Client{Timeout: 30 * time.Second}, Token: ghToken})
	go gh.Poll(ctx, 90*time.Second)
	wf := workflows.Register(t, db, workflows.Options{Dir: cfg.Path("workflows")})
	go wf.Watch(ctx, 2*time.Second)
	uis := ui.Register(t, ui.Options{Home: cfg.Home, Publish: hub.Publish})
	go uis.Watch(ctx, time.Second)
	up := &update.Proxy{API: cfg.GitHubAPI, Repo: cfg.UpdateRepo, Base: cfg.URL(), HTTP: &http.Client{}, Token: ghToken}
	restart := &restarter{busy: ss.Busy, stop: stop, every: 5 * time.Second}
	return t, server.New(server.Options{
		Tree: t, Hub: hub, Token: token, Version: version, MCP: mcp.New(t, hub, version),
		Routes: map[string]http.Handler{
			"GET /api/artifact":              http.HandlerFunc(ts.ServeArtifact),
			"GET /api/file":                  http.HandlerFunc(ts.ServeFile),
			"GET /api/files":                 http.HandlerFunc(ts.ServeFiles),
			"GET /api/diff":                  diff.Handler(t),
			"GET /api/sessions/{id8}/events": http.HandlerFunc(ss.ServeEvents),
			"GET /api/capabilities":          http.HandlerFunc(ss.ServeCapabilities),
			"GET /api/plugins/{file}":        http.HandlerFunc(uis.ServePlugin),
			"GET /api/update/latest.json":    http.HandlerFunc(up.ServeManifest),
			"GET /api/update/asset/{id}":     http.HandlerFunc(up.ServeAsset),
			"POST /api/restart":              restart,
		},
	})
}

// Serve runs the daemon until SIGINT or SIGTERM.
func Serve(cfg config.Config, version string) error {
	token, err := cfg.Token()
	if err != nil {
		return err
	}
	if cfg.PluginDir == "" {
		cfg.PluginDir = cfg.Path("plugin")
		if err := plugin.Install(cfg.PluginDir); err != nil {
			return err
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if err := plugin.MirrorCodex(os.DirFS(cfg.PluginDir), filepath.Join(home, ".agents", "skills")); err != nil {
			slog.Warn("codex skills not installed", "err", err)
		}
	}
	db, err := store.Open(cfg.Path("layer8.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, restart := context.WithCancel(ctx)
	defer restart()
	_, handler := build(ctx, cfg, db, token, version, restart)
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("layer8d serving", "url", cfg.URL(), "home", cfg.Home, "version", version)
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
