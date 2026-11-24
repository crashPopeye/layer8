package session

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/workflow"
)

const (
	parentGuard = "parent-guard"
	refresh     = "refresh"
	route       = "route"
)

// hooks are the callbacks every session registers at initialize.
var hooks = []claude.HookMatcher{
	{Event: "PreToolUse", Matcher: "Write|Edit|MultiEdit|NotebookEdit", ID: parentGuard},
	{Event: "PostToolUse", Matcher: "Write|Edit|MultiEdit|Bash", ID: refresh},
	{Event: "PostToolUse", Matcher: "Skill", ID: route},
}

// RefreshWindow is how long tool calls are gathered into one artifacts and changes refresh.
const RefreshWindow = 500 * time.Millisecond

// onHook answers a hook callback of a session of taskID whose artifacts live in artifactsDir.
func (m *Manager) onHook(taskID, artifactsDir string, call claude.HookCall) claude.HookResult {
	switch call.CallbackID {
	case parentGuard:
		return guardParent(artifactsDir, call)
	case refresh:
		m.refresh.touch(taskID)
	case route:
		return m.routeSkill(taskID, call)
	}
	return claude.HookResult{}
}

// routeSkill tells the agent which skill comes after the one it just loaded in its task's workflow.
func (m *Manager) routeSkill(taskID string, call claude.HookCall) claude.HookResult {
	var in struct {
		Skill string `json:"skill"`
	}
	json.Unmarshal(call.Input.ToolInput, &in)
	t, err := m.store.Task(taskID)
	if err != nil {
		return claude.HookResult{}
	}
	next, ok := workflow.AfterSkill(t.Workflow, in.Skill)
	if !ok {
		return claude.HookResult{}
	}
	return claude.HookResult{AdditionalContext: fmt.Sprintf("Next in %s: %s", t.Workflow, next.Skill)}
}

// guardParent denies writes into the parent task's artifacts.
func guardParent(artifactsDir string, call claude.HookCall) claude.HookResult {
	var in struct {
		FilePath     string `json:"file_path"`
		NotebookPath string `json:"notebook_path"`
	}
	json.Unmarshal(call.Input.ToolInput, &in)
	path := in.FilePath + in.NotebookPath
	if !filepath.IsAbs(path) && call.Input.Cwd != "" {
		path = filepath.Join(call.Input.Cwd, path)
	}
	path = filepath.Clean(path)
	if strings.Contains(path, "/.layer8/task/parent/") || strings.HasPrefix(path, filepath.Join(artifactsDir, "parent")+"/") {
		return claude.HookResult{Deny: "parent artifacts are read-only; use l8 artifact write --task <parent>"}
	}
	return claude.HookResult{}
}

// coalescer calls fire once per key at the end of a window, however often the key is touched during it.
type coalescer struct {
	window time.Duration
	fire   func(key string)

	mu      sync.Mutex
	pending map[string]bool
}

func newCoalescer(window time.Duration, fire func(key string)) *coalescer {
	return &coalescer{window: window, fire: fire, pending: map[string]bool{}}
}

func (c *coalescer) touch(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending[key] {
		return
	}
	c.pending[key] = true
	time.AfterFunc(c.window, func() {
		c.mu.Lock()
		delete(c.pending, key)
		c.mu.Unlock()
		c.fire(key)
	})
}
