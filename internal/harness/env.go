package harness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const envMarker = "__LAYER8_ENV__"

var shellEnv = sync.OnceValue(func() []string { return clean(loginEnv()) })

// ShellEnv returns the user's login-shell environment, without the variables of an
// enclosing Claude Code session, for spawning agent CLIs.
func ShellEnv() []string { return shellEnv() }

// LookPath finds bin on the PATH of env.
func LookPath(bin string, env []string) (string, error) {
	if strings.Contains(bin, "/") {
		return bin, nil
	}
	for _, kv := range env {
		path, ok := strings.CutPrefix(kv, "PATH=")
		if !ok {
			continue
		}
		for _, dir := range filepath.SplitList(path) {
			p := filepath.Join(dir, bin)
			if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
				return p, nil
			}
		}
	}
	return "", fmt.Errorf("%s not found on the login-shell PATH", bin)
}

func loginEnv() []string {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-ilc", "printf "+envMarker+"; env -0")
	cmd.Env = []string{"HOME=" + os.Getenv("HOME"), "USER=" + os.Getenv("USER"), "LOGNAME=" + os.Getenv("LOGNAME"),
		"SHELL=" + shell, "TMPDIR=" + os.Getenv("TMPDIR"), "LANG=" + os.Getenv("LANG"), "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	out, err := cmd.Output()
	_, after, ok := bytes.Cut(out, []byte(envMarker))
	if err != nil || !ok {
		return os.Environ()
	}
	return strings.Split(strings.TrimRight(string(after), "\x00"), "\x00")
}

func clean(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == "" || k == "CLAUDECODE" || k == "NODE_OPTIONS" || strings.HasPrefix(k, "CLAUDE_CODE_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}
