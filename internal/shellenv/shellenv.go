// Package shellenv gives a Finder-launched app the PATH the user has in their terminal.
//
// Apps started from Finder or the Dock inherit launchd's minimal PATH
// (/usr/bin:/bin:/usr/sbin:/sbin), so claude, git from Homebrew, gh and tmux
// would not be found. We ask the user's login shell once at startup.
package shellenv

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const marker = "__LAYER8_PATH__"

// Fix merges the login shell's PATH and common install locations into this process's PATH.
func Fix() {
	parts := []string{}
	if p := loginShellPath(); p != "" {
		parts = append(parts, filepath.SplitList(p)...)
	}
	parts = append(parts, filepath.SplitList(os.Getenv("PATH"))...)
	if home, err := os.UserHomeDir(); err == nil {
		parts = append(parts,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".claude", "local"),
			filepath.Join(home, "go", "bin"),
		)
	}
	parts = append(parts, "/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin")
	os.Setenv("PATH", strings.Join(dedupe(parts), string(os.PathListSeparator)))
}

func loginShellPath() string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// -i loads .zshrc/.bashrc (where most PATH edits live); markers fence off any rc-file output.
	out, err := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "printf '%s%s%s' "+marker+" \"$PATH\" "+marker).Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	return Extract(string(out))
}

// Extract returns the PATH between the markers, or "" if they are missing.
func Extract(out string) string {
	start := strings.Index(out, marker)
	if start < 0 {
		return ""
	}
	rest := out[start+len(marker):]
	end := strings.Index(rest, marker)
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, p := range in {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}
