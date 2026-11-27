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

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/diff"
	"github.com/crashPopeye/layer8/internal/github"
	"github.com/crashPopeye/layer8/internal/harness"
	"github.com/crashPopeye/layer8/internal/harness/claude"
	"github.com/crashPopeye/layer8/internal/harness/codex"
	"github.com/crashPopeye/layer8/internal/linear"
	"github.com/crashPopeye/layer8/internal/mcp"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/server"
	"github.com/crashPopeye/layer8/internal/sessions"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
	"github.com/crashPopeye/layer8/internal/workflows"
	"github.com/crashPopeye/layer8/plugin"
)

// Build assembles the tree, its kinds and the HTTP handler, and watches artifact and workflow folders until ctx ends.
func Build(ctx context.Context, cfg config.Config, db *sql.DB, token, version string) http.Handler {
	_, h := build(ctx, cfg, db, token, version)
	return h
}

func build(ctx context.Context, cfg config.Config, db *sql.DB, token, version string) (*tree.Tree, http.Handler) {
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
	linearCallback := linear.Register(t, linear.New(cfg.Path("linear.json"), cfg.LinearClientID), cfg.URL()+linearCallbackPath)
	diff.Register(t)
	gh := github.Register(t, db, &github.Client{API: "https://api.github.com", HTTP: &http.Client{Timeout: 30 * time.Second}, Token: github.GHToken(cfg.GitHubToken)})
	go gh.Poll(ctx, 90*time.Second)
	wf := workflows.Register(t, db, workflows.Options{Dir: cfg.Path("workflows")})
	go wf.Watch(ctx, 2*time.Second)
	return t, server.New(server.Options{
		Tree: t, Hub: hub, Token: token, Version: version, MCP: mcp.New(t, hub, version),
		Routes: map[string]http.Handler{
			"GET /api/artifact":              http.HandlerFunc(ts.ServeArtifact),
			"GET /api/file":                  http.HandlerFunc(ts.ServeFile),
			"GET /api/diff":                  diff.Handler(t),
			"GET /api/sessions/{id8}/events": http.HandlerFunc(ss.ServeEvents),
			"GET /api/capabilities":          http.HandlerFunc(ss.ServeCapabilities),
		},
		Public: map[string]http.Handler{"GET " + linearCallbackPath: linearCallback},
	})
}

const linearCallbackPath = "/api/linear/callback"

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
	srv := &http.Server{
		Handler:           Build(ctx, cfg, db, token, version),
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
