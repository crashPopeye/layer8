package session

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/crashPopeye/layer8/internal/claude"
)

const parentGuard = "parent-guard"

// hooks are the callbacks every session registers at initialize.
var hooks = []claude.HookMatcher{
	{Event: "PreToolUse", Matcher: "Write|Edit|MultiEdit|NotebookEdit", ID: parentGuard},
}

// onHook answers a hook callback of a session whose artifacts live in artifactsDir.
func onHook(artifactsDir string, call claude.HookCall) claude.HookResult {
	if call.CallbackID != parentGuard {
		return claude.HookResult{}
	}
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
