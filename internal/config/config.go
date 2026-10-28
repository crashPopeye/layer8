// Package config holds layer8's on-disk settings and paths.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type TerminalApp string

const (
	TerminalApple   TerminalApp = "terminal"
	TerminalIterm   TerminalApp = "iterm"
	TerminalGhostty TerminalApp = "ghostty"
	TerminalWezterm TerminalApp = "wezterm"
	TerminalKitty   TerminalApp = "kitty"
	TerminalCustom  TerminalApp = "custom"
)

type EditorApp string

const (
	EditorCode   EditorApp = "code"
	EditorCursor EditorApp = "cursor"
	EditorZed    EditorApp = "zed"
	EditorIdea   EditorApp = "idea"
	EditorCustom EditorApp = "custom"
)

// Config is persisted as ~/.layer8/config.json. Secrets live here too, with mode 0600.
type Config struct {
	// Port the daemon listens on, on 127.0.0.1 only. Changing it needs a daemon restart.
	Port int `json:"port"`

	LinearAPIKey string `json:"linearApiKey"`
	// Empty means: ask the gh CLI (`gh auth token`).
	GitHubToken string `json:"githubToken"`

	Terminal        TerminalApp `json:"terminal"`
	TerminalCommand string      `json:"terminalCommand"` // custom: template with {cwd} and {cmd}
	Editor          EditorApp   `json:"editor"`
	EditorCommand   string      `json:"editorCommand"` // custom: template with {path}
	UseTmux         bool        `json:"useTmux"`

	MaxConcurrentSessions int    `json:"maxConcurrentSessions"`
	WorktreeRoot          string `json:"worktreeRoot"`
	// Claude model alias or ID passed as --model; empty uses Claude Code's default.
	Model string `json:"model"`
	// PermissionMode for agents: "acceptEdits" (file edits go through, commands ask),
	// "default" (ask for everything) or "settings" (use the user's Claude Code settings).
	PermissionMode string `json:"permissionMode"`

	SyncIntervalMinutes   int  `json:"syncIntervalMinutes"` // 0 disables background sync
	PushAfterSync         bool `json:"pushAfterSync"`       // force-with-lease branches that already have an upstream
	AutoStartSlices       bool `json:"autoStartSlices"`     // start a slice's session when its parent finishes
	CreateLinearSubIssues bool `json:"createLinearSubIssues"`
	DraftPRs              bool `json:"draftPRs"`
}

// Paths are derived from the home directory; LAYER8_HOME overrides it (tests, multiple profiles).
type Paths struct {
	Home, Config, DB, Tasks, Logs, Token, LinearToken string
}

func ResolvePaths() (Paths, error) {
	home := os.Getenv("LAYER8_HOME")
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return Paths{}, err
		}
		home = filepath.Join(h, ".layer8")
	}
	p := Paths{
		Home:        home,
		Config:      filepath.Join(home, "config.json"),
		DB:          filepath.Join(home, "layer8.db"),
		Tasks:       filepath.Join(home, "tasks"),
		Logs:        filepath.Join(home, "logs"),
		Token:       filepath.Join(home, "token"),
		LinearToken: filepath.Join(home, "linear-token.json"),
	}
	for _, d := range []string{p.Home, p.Tasks, p.Logs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return Paths{}, err
		}
	}
	return p, nil
}

func Defaults(p Paths) Config {
	return Config{
		Port:                  7878,
		Terminal:              TerminalApple,
		Editor:                EditorCode,
		UseTmux:               true,
		MaxConcurrentSessions: 6,
		WorktreeRoot:          filepath.Join(p.Home, "worktrees"),
		PermissionMode:        "acceptEdits",
		SyncIntervalMinutes:   10,
		PushAfterSync:         true,
		AutoStartSlices:       true,
		CreateLinearSubIssues: true,
		DraftPRs:              true,
	}
}

// Store guards the config file; reads return copies so callers can't race on fields.
type Store struct {
	mu   sync.RWMutex
	path string
	cfg  Config
}

func Load(p Paths) (*Store, error) {
	cfg := Defaults(p)
	b, err := os.ReadFile(p.Config)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p.Config, err)
		}
	}
	if cfg.LinearAPIKey == "" {
		cfg.LinearAPIKey = os.Getenv("LINEAR_API_KEY")
	}
	return &Store{path: p.Config, cfg: normalize(cfg, p)}, nil
}

func normalize(c Config, p Paths) Config {
	d := Defaults(p)
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = d.Port
	}
	if c.MaxConcurrentSessions < 1 {
		c.MaxConcurrentSessions = d.MaxConcurrentSessions
	}
	if c.MaxConcurrentSessions > 32 {
		c.MaxConcurrentSessions = 32
	}
	if c.WorktreeRoot == "" {
		c.WorktreeRoot = d.WorktreeRoot
	}
	if c.Terminal == "" {
		c.Terminal = d.Terminal
	}
	if c.Editor == "" {
		c.Editor = d.Editor
	}
	switch c.PermissionMode {
	case "default", "acceptEdits", "auto", "bypassPermissions", "settings":
	default:
		c.PermissionMode = d.PermissionMode
	}
	if c.SyncIntervalMinutes < 0 {
		c.SyncIntervalMinutes = 0
	}
	return c
}

func (s *Store) Get() Config {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

func (s *Store) Save(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c = normalize(c, Paths{Home: filepath.Dir(s.path)})
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return err
	}
	s.cfg = c
	return nil
}

// Token returns the daemon's API token, creating it (mode 0600) on first use.
// Only processes running as this user can read it.
func Token(p Paths) (string, error) {
	if b, err := os.ReadFile(p.Token); err == nil {
		if tok := strings.TrimSpace(string(b)); len(tok) >= 32 {
			return tok, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	// Write to a temp file and rename, so a concurrent reader never sees a partial token.
	tmp, err := os.CreateTemp(p.Home, ".token-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.WriteString(tok); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	// Link fails if another process created the token first; then use theirs.
	if err := os.Link(tmp.Name(), p.Token); err != nil {
		b, rerr := os.ReadFile(p.Token)
		if rerr != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)), nil
	}
	return tok, nil
}
