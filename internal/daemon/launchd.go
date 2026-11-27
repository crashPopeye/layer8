package daemon

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
)

// Label is the launchd label of the daemon.
const Label = "dev.layer8.daemon"

// Plist returns the LaunchAgent property list that runs exe serve with cfg's home.
func Plist(exe string, cfg config.Config) string {
	x := func(s string) string {
		var b bytes.Buffer
		xml.EscapeText(&b, []byte(s))
		return b.String()
	}
	logPath := cfg.Path("logs", "daemon.log")
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key><array><string>%s</string><string>serve</string></array>
	<key>EnvironmentVariables</key><dict><key>LAYER8_HOME</key><string>%s</string></dict>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardOutPath</key><string>%s</string>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, Label, x(exe), x(cfg.Home), x(logPath), x(logPath))
}

// Ensure installs or refreshes the LaunchAgent, waits until the daemon of this version answers, and returns its token.
func Ensure(cfg config.Config, version string) (token string, err error) {
	if token, err = cfg.Token(); err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, "Library", "LaunchAgents", Label+".plist")
	plist := Plist(exe, cfg)
	old, _ := os.ReadFile(path)
	service := fmt.Sprintf("gui/%d/%s", os.Getuid(), Label)
	running := runningVersion(cfg.URL())

	switch {
	case string(old) != plist || running == "":
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.MkdirAll(cfg.Path("logs"), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
			return "", err
		}
		exec.Command("launchctl", "bootout", service).Run()
		if out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), path).CombinedOutput(); err != nil {
			return "", fmt.Errorf("launchctl bootstrap: %v: %s", err, out)
		}
	case running != version:
		if out, err := exec.Command("launchctl", "kickstart", "-k", service).CombinedOutput(); err != nil {
			return "", fmt.Errorf("launchctl kickstart: %v: %s", err, out)
		}
	}

	for deadline := time.Now().Add(10 * time.Second); runningVersion(cfg.URL()) != version; time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			return "", fmt.Errorf("daemon %s did not answer at %s; see %s", version, cfg.URL(), cfg.Path("logs", "daemon.log"))
		}
	}
	return token, nil
}

func runningVersion(url string) string {
	client := http.Client{Timeout: time.Second}
	resp, err := client.Get(url + "/api/version")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var v struct{ Version string }
	json.NewDecoder(resp.Body).Decode(&v)
	return v.Version
}
