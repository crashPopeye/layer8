// Package tmux keeps one detached tmux session per worktree, so shells and dev
// servers survive closing the terminal window.
package tmux

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
)

var unsafeName = regexp.MustCompile(`[.:\s]`)

// Name derives a session name; tmux treats '.' and ':' as target separators.
func Name(slug string) string {
	return unsafeName.ReplaceAllString("l8-"+slug, "-")
}

func Available() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

func run(ctx context.Context, args ...string) error {
	return exec.CommandContext(ctx, "tmux", args...).Run()
}

func HasSession(ctx context.Context, name string) bool {
	return run(ctx, "has-session", "-t", "="+name) == nil
}

// Ensure starts a detached session in cwd running the user's own shell, with env set.
func Ensure(ctx context.Context, name, cwd string, env map[string]string) error {
	if HasSession(ctx, name) {
		return nil
	}
	args := []string{"new-session", "-d", "-s", name, "-c", cwd}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	return run(ctx, args...)
}

func NewWindow(ctx context.Context, name, cwd, title, command string) error {
	return run(ctx, "new-window", "-t", "="+name+":", "-c", cwd, "-n", title, command)
}

func Kill(ctx context.Context, name string) error {
	if !HasSession(ctx, name) {
		return nil
	}
	return run(ctx, "kill-session", "-t", "="+name)
}

// AttachCommand attaches to (or recreates) the session; run it inside a terminal.
func AttachCommand(name, cwd string) string {
	return "tmux new-session -A -s " + Quote(name) + " -c " + Quote(cwd)
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_\-./=:@%+,]+$`)

// Quote quotes s for a POSIX shell.
func Quote(s string) string {
	if s != "" && shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
