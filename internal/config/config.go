// Package config locates the layer8 home folder and reads its settings.
package config

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Config is the content of <home>/config.json plus the home folder itself.
type Config struct {
	Home        string `json:"-"`
	Addr        string `json:"addr"`
	MaxSessions int    `json:"maxSessions"`
	Apps        Apps   `json:"apps"`
}

// Apps names the macOS applications a task's "open" action uses.
type Apps struct {
	Terminal string `json:"terminal"`
	Editor   string `json:"editor"`
}

// Load reads the config from LAYER8_HOME (default ~/.layer8), creating the folder if needed.
func Load() (Config, error) {
	home := os.Getenv("LAYER8_HOME")
	if home == "" {
		user, err := os.UserHomeDir()
		if err != nil {
			return Config{}, err
		}
		home = filepath.Join(user, ".layer8")
	}
	home, err := filepath.Abs(home)
	if err != nil {
		return Config{}, err
	}
	if err := os.MkdirAll(home, 0o700); err != nil {
		return Config{}, err
	}
	c := Config{Home: home, Addr: "127.0.0.1:7979", MaxSessions: 6, Apps: Apps{Terminal: "Terminal", Editor: "Visual Studio Code"}}
	data, err := os.ReadFile(c.Path("config.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, err
	}
	return c, nil
}

// Path joins elems onto the home folder.
func (c Config) Path(elem ...string) string {
	return filepath.Join(append([]string{c.Home}, elem...)...)
}

// URL is the daemon's base URL.
func (c Config) URL() string {
	return "http://" + c.Addr
}

// Token returns the API token, creating it on first use.
func (c Config) Token() (string, error) {
	data, err := os.ReadFile(c.Path("token"))
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	token := rand.Text()
	return token, os.WriteFile(c.Path("token"), []byte(token+"\n"), 0o600)
}
