package daemon

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/client"
)

// Label is the LaunchAgent that keeps the daemon running.
const Label = "dev.layer8.daemon"

// Agent installs and refreshes the daemon's LaunchAgent.
type Agent struct {
	Exe       string
	Home      string
	Addr      string
	PlistPath string
	Launchctl func(args ...string) ([]byte, error)
	Version   func() (string, error)
}

// NewAgent returns the LaunchAgent that runs this executable as the daemon for home and addr.
func NewAgent(home, addr string) (Agent, error) {
	exe, err := os.Executable()
	if err != nil {
		return Agent{}, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return Agent{}, err
	}
	return Agent{
		Exe:       exe,
		Home:      home,
		Addr:      addr,
		PlistPath: filepath.Join(userHome, "Library", "LaunchAgents", Label+".plist"),
		Launchctl: func(args ...string) ([]byte, error) { return exec.Command("/bin/launchctl", args...).CombinedOutput() },
		Version: func() (string, error) {
			token, err := os.ReadFile(client.TokenPath(home))
			if err != nil {
				return "", err
			}
			return client.New("http://"+addr, strings.TrimSpace(string(token))).Version()
		},
	}, nil
}

// Plist renders the LaunchAgent: `<exe> daemon` at login, restarted whenever it exits.
func (a Agent) Plist() string {
	log := xmlEscape(filepath.Join(a.Home, "logs", "daemon.log"))
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + Label + `</string>
  <key>ProgramArguments</key>
  <array><string>` + xmlEscape(a.Exe) + `</string><string>daemon</string></array>
  <key>EnvironmentVariables</key>
  <dict>
    <key>LAYER8_HOME</key><string>` + xmlEscape(a.Home) + `</string>
    <key>LAYER8_ADDR</key><string>` + xmlEscape(a.Addr) + `</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ThrottleInterval</key><integer>5</integer>
  <key>ProcessType</key><string>Interactive</string>
  <key>StandardOutPath</key><string>` + log + `</string>
  <key>StandardErrorPath</key><string>` + log + `</string>
</dict>
</plist>
`
}

// Ensure makes the daemon answering on Addr the one at version want. Otherwise
// it rewrites the plist for this executable, replaces whatever runs under Label
// (the old layer8 included) and waits for the new daemon to answer. A "dev"
// build never installs the LaunchAgent; its daemon runs from `make dev`.
func (a Agent) Ensure(ctx context.Context, want string) error {
	if v, err := a.Version(); err == nil && v == want {
		return nil
	}
	if want == "dev" {
		return fmt.Errorf("no dev daemon answers on %s; start one with make dev", a.Addr)
	}
	if err := os.MkdirAll(filepath.Join(a.Home, "logs"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.PlistPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(a.PlistPath, []byte(a.Plist()), 0o644); err != nil {
		return err
	}
	domain := "gui/" + strconv.Itoa(os.Getuid())
	a.Launchctl("bootout", domain+"/"+Label)
	if err := retry(ctx, 10, func() error { return a.launchctl("bootstrap", domain, a.PlistPath) }); err != nil {
		return err
	}
	if err := a.launchctl("kickstart", domain+"/"+Label); err != nil {
		return err
	}
	return retry(ctx, 75, func() error {
		v, err := a.Version()
		if err == nil && v != want {
			err = fmt.Errorf("daemon on %s is version %s, not %s", a.Addr, v, want)
		}
		return err
	})
}

func (a Agent) launchctl(args ...string) error {
	if out, err := a.Launchctl(args...); err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", args[0], err, bytes.TrimSpace(out))
	}
	return nil
}

// retry calls fn up to attempts times, 200 ms apart, until it succeeds.
func retry(ctx context.Context, attempts int, fn func() error) error {
	var err error
	for range attempts {
		if err = fn(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return err
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
