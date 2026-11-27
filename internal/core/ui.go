package core

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Focus returns what the user last looked at in the window.
func (a *App) Focus() Focus {
	a.focusMu.Lock()
	defer a.focusMu.Unlock()
	return a.focus
}

func (a *App) SetFocus(f Focus) {
	a.focusMu.Lock()
	a.focus = f
	a.focusMu.Unlock()
}

// toWindow asks the open window to do something: show a node, notify, switch theme.
func (a *App) toWindow(verb string, n *Node, in Args) (any, error) {
	ev := map[string]any{"verb": verb, "ref": n.Ref, "args": in}
	if n.task != nil {
		ev["taskId"] = n.task.ID
	}
	if n.slice != nil {
		ev["sliceId"] = n.slice.ID
	}
	if n.session != nil {
		ev["sessionId"] = n.session.ID
	}
	a.bus.emit("layer8:ui", ev)
	return "sent to the window", nil
}

func (a *App) uiFile() string     { return filepath.Join(a.Paths.Home, "ui.json") }
func (a *App) pluginsDir() string { return filepath.Join(a.Paths.Home, "plugins") }

// UIPrefs is the window's saved layout and theme, opaque to the daemon, plus the
// user's plugin files.
type UIPrefs struct {
	Prefs   json.RawMessage `json:"prefs"`
	Plugins []string        `json:"plugins"`
}

func (a *App) UIPrefs() (UIPrefs, error) {
	out := UIPrefs{Prefs: json.RawMessage("null"), Plugins: []string{}}
	if b, err := os.ReadFile(a.uiFile()); err == nil && json.Valid(b) {
		out.Prefs = b
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return out, err
	}
	entries, err := os.ReadDir(a.pluginsDir())
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return out, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
			out.Plugins = append(out.Plugins, e.Name())
		}
	}
	return out, nil
}

func (a *App) SaveUIPrefs(b []byte) error {
	if !json.Valid(b) {
		return bad("prefs must be JSON")
	}
	return os.WriteFile(a.uiFile(), b, 0o644)
}

// servePlugin sends a plugin file from ~/.layer8/plugins as a JavaScript module.
func (a *App) servePlugin(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name != filepath.Base(name) || !strings.HasSuffix(name, ".js") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, filepath.Join(a.pluginsDir(), name))
}
