// Package config reads and writes layer8's user settings, config.json in its home.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config is what the user sets once, such as their Linear API key.
type Config struct {
	LinearAPIKey string `json:"linearApiKey,omitempty"`
}

func path(home string) string { return filepath.Join(home, "config.json") }

// Load reads home/config.json; a missing file is an empty Config.
func Load(home string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path(home))
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

// Save replaces home/config.json with c, readable by the user only.
func Save(home string, c Config) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path(home) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path(home))
}
