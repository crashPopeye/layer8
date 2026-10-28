package daemon

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
)

const Label = "dev.layer8.daemon"

func agentPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", Label+".plist"), nil
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// AgentPlist describes the LaunchAgent: run `<exe> daemon` at login and restart it
// whenever it exits (that is how it switches to a new version after an update).
func AgentPlist(exe, logPath string) string {
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + Label + `</string>
  <key>ProgramArguments</key>
  <array><string>` + xmlEscape(exe) + `</string><string>daemon</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>` + xmlEscape(logPath) + `</string>
  <key>StandardErrorPath</key><string>` + xmlEscape(logPath) + `</string>
</dict>
</plist>
`
}

func launchctl(args ...string) (string, error) {
	out, err := exec.Command("/bin/launchctl", args...).CombinedOutput()
	return string(out), err
}

func domain() string { return "gui/" + strconv.Itoa(os.Getuid()) }

// installAgent writes the plist and (re)loads it when it changed or isn't loaded.
func installAgent(exe string, paths config.Paths) error {
	plistPath, err := agentPlistPath()
	if err != nil {
		return err
	}
	want := AgentPlist(exe, filepath.Join(paths.Logs, "layer8d.stdout.log"))
	have, _ := os.ReadFile(plistPath)
	_, loadedErr := launchctl("print", domain()+"/"+Label)
	loaded := loadedErr == nil
	if string(have) == want && loaded {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plistPath, []byte(want), 0o644); err != nil {
		return err
	}
	if loaded {
		// bootout waits for the old daemon to exit (it gets SIGTERM and shuts down cleanly).
		if out, err := launchctl("bootout", domain()+"/"+Label); err != nil {
			return fmt.Errorf("launchctl bootout: %v: %s", err, out)
		}
	}
	if out, err := launchctl("bootstrap", domain(), plistPath); err != nil {
		return fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
	}
	return nil
}

// UninstallAgent stops the daemon and removes the LaunchAgent.
func UninstallAgent() error {
	plistPath, err := agentPlistPath()
	if err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		_, _ = launchctl("bootout", domain()+"/"+Label)
	}
	if err := os.Remove(plistPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Ensure makes sure the daemon from this executable is running and returns how to
// reach it. On macOS it is managed by launchd; elsewhere (development) it is started
// as a detached process.
func Ensure(ctx context.Context, version string) (Info, error) {
	paths, err := config.ResolvePaths()
	if err != nil {
		return Info{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return Info{}, err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	info, err := LocalInfo()
	if err != nil {
		return info, err
	}
	c := NewClient(info)

	running, verr := c.Version(ctx)
	if runtime.GOOS == "darwin" {
		if err := installAgent(exe, paths); err != nil {
			return info, err
		}
	} else if verr != nil {
		if err := startDetached(exe, paths); err != nil {
			return info, err
		}
	}
	running, err = waitUp(ctx, c, 15*time.Second)
	if err != nil {
		return info, err
	}
	if running != version && version != "dev" {
		// An older daemon (from before an update): restart onto this version if idle.
		// When agents are busy it switches by itself once they finish.
		err := c.Do(ctx, http.MethodPost, "/api/shutdown", map[string]bool{"force": false}, nil)
		if err == nil {
			waitDown(ctx, c, running, 10*time.Second)
			if runtime.GOOS != "darwin" {
				_ = startDetached(exe, paths)
			}
			if _, err := waitUp(ctx, c, 15*time.Second); err != nil {
				return info, err
			}
		}
	}
	return info, nil
}

func waitUp(ctx context.Context, c *Client, d time.Duration) (string, error) {
	deadline := time.Now().Add(d)
	for {
		v, err := c.Version(ctx)
		if err == nil {
			return v, nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("layer8d did not start; see ~/.layer8/logs/layer8d.log (%v)", err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// waitDown waits until the daemon running oldVersion is gone (or replaced).
func waitDown(ctx context.Context, c *Client, oldVersion string, d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		v, err := c.Version(ctx)
		if err != nil || v != oldVersion {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func startDetached(exe string, paths config.Paths) error {
	logf, err := os.OpenFile(filepath.Join(paths.Logs, "layer8d.stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(exe, "daemon")
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
