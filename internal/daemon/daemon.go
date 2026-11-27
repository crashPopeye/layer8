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
	"syscall"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/linear"
	"github.com/crashPopeye/layer8/internal/mcp"
	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/server"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tasks"
	"github.com/crashPopeye/layer8/internal/tree"
)

// Build assembles the tree, its kinds and the HTTP handler, and watches artifact folders until ctx ends.
func Build(ctx context.Context, cfg config.Config, db *sql.DB, token, version string) http.Handler {
	t := tree.New(db)
	hub := server.NewHub()
	t.OnChange = func(addrs []tree.Addr) { hub.Publish("node", server.NodeChange{Addrs: addrs}) }
	projects.Register(t, db, version)
	ts := tasks.Register(t, db, tasks.Options{Home: cfg.Home, Apps: cfg.Apps})
	go ts.Watch(ctx, 2*time.Second)
	linearCallback := linear.Register(t, linear.New(cfg.Path("linear.json"), cfg.LinearClientID), cfg.URL()+linearCallbackPath)
	return server.New(server.Options{
		Tree: t, Hub: hub, Token: token, Version: version, MCP: mcp.New(t, hub, version),
		Routes: map[string]http.Handler{
			"GET /api/artifact": http.HandlerFunc(ts.ServeArtifact),
			"GET /api/file":     http.HandlerFunc(ts.ServeFile),
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
