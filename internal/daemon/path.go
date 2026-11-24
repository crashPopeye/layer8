package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

const pathMarker = "__LAYER8_PATH__"

// fixPath prepends the login shell's PATH and the usual install folders to this
// process's PATH, so a daemon started by launchd finds claude, git and gh.
func fixPath() {
	parts := filepath.SplitList(loginShellPath())
	parts = append(parts, filepath.SplitList(os.Getenv("PATH"))...)
	if home, err := os.UserHomeDir(); err == nil {
		parts = append(parts, filepath.Join(home, ".local", "bin"), filepath.Join(home, ".claude", "local"))
	}
	parts = append(parts, "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	var path []string
	for _, p := range parts {
		if p != "" && !slices.Contains(path, p) {
			path = append(path, p)
		}
	}
	os.Setenv("PATH", strings.Join(path, string(os.PathListSeparator)))
}

// loginShellPath asks the user's interactive login shell for its PATH.
func loginShellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "printf '%s%s%s' "+pathMarker+" \"$PATH\" "+pathMarker).Output()
	return between(string(out), pathMarker)
}

// between returns the text between the first two occurrences of marker, or "".
func between(s, marker string) string {
	_, rest, ok := strings.Cut(s, marker)
	if !ok {
		return ""
	}
	inner, _, ok := strings.Cut(rest, marker)
	if !ok {
		return ""
	}
	return strings.TrimSpace(inner)
}
