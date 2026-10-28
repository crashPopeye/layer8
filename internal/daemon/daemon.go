// Package daemon runs layer8's background service and manages its lifecycle:
// a launchd LaunchAgent on macOS keeps it alive across app restarts and updates.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/core"
	"github.com/crashPopeye/layer8/internal/server"
	"github.com/crashPopeye/layer8/internal/shellenv"
)

// Serve runs the daemon until SIGTERM/SIGINT, a /api/shutdown request, or (once no
// agent is working) its own executable being replaced by an update.
func Serve(version string, ui fs.FS) error {
	shellenv.Fix()
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	if f, err := os.OpenFile(filepath.Join(paths.Logs, "layer8d.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
	token, err := config.Token(paths)
	if err != nil {
		return err
	}
	hub := server.NewHub()
	app, err := core.New(core.Options{Paths: paths, Version: version, Emit: hub.Emit})
	if err != nil {
		return err
	}
	port := app.Config.Get().Port
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		app.Shutdown()
		return fmt.Errorf("listen on 127.0.0.1:%d (is another layer8d running, or change the port in ~/.layer8/config.json): %w", port, err)
	}

	stop := make(chan string, 1)
	requestStop := func(reason string) {
		select {
		case stop <- reason:
		default:
		}
	}
	api := app.Handler(core.Hooks{
		OpenURL:  OpenURL,
		Shutdown: func() { requestStop("shutdown requested") },
	})
	srv := &http.Server{
		Handler:           server.New(token, port, api, ui, hub).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			requestStop("server error: " + err.Error())
		}
	}()
	app.Run()
	log.Printf("layer8d %s listening on 127.0.0.1:%d (pid %d)", version, port, os.Getpid())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGINT)
	go watchForUpdate(app, requestStop)

	var reason string
	select {
	case s := <-sigs:
		reason = "signal " + s.String()
	case reason = <-stop:
	}
	log.Printf("layer8d stopping: %s", reason)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx) // SSE streams are cut; clients reconnect to the next daemon
	app.Shutdown()
	return nil
}

// watchForUpdate exits once the binary on disk changed (the app was updated) and no
// agent is working; launchd then starts the new version.
func watchForUpdate(app *core.App, requestStop func(string)) {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	orig, err := os.Stat(exe)
	if err != nil {
		return
	}
	for range time.Tick(30 * time.Second) {
		now, err := os.Stat(exe)
		if err != nil || os.SameFile(orig, now) && now.ModTime().Equal(orig.ModTime()) {
			continue // unchanged, or mid-replacement
		}
		if running, queued := app.Sessions.Counts(); running+queued == 0 {
			requestStop("a new version was installed")
			return
		}
	}
}

// OpenURL opens an http(s) link in the default browser.
func OpenURL(u string) error {
	cmd := "xdg-open"
	if runtime.GOOS == "darwin" {
		cmd = "/usr/bin/open"
	}
	return exec.Command(cmd, u).Start()
}
