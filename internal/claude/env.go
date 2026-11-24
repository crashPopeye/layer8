package claude

import (
	"slices"
	"strings"
)

// inheritedSessionVars tie a CLI to a parent Claude Code session, as when the
// daemon is started from inside one; spawned sessions must not see them.
var inheritedSessionVars = []string{
	"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_PID",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_REMOTE_SESSION_ID", "CLAUDE_CODE_SESSION_ATTENDED",
	"CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN", "CLAUDE_AFTER_LAST_COMPACT",
	"CLAUDE_ADDITIONAL_DIRECTORIES", "CLAUDE_CODE_ADDITIONAL_DIRECTORIES_CLAUDE_MD", "CLAUDE_CODE_TEE_SDK_STDOUT",
	"CLAUDE_CODE_INCLUDE_PARTIAL_MESSAGES",
	"NODE_OPTIONS", "DEBUG",
}

// CleanEnv drops the variables that would attach a spawned CLI to a parent session.
func CleanEnv(in []string) []string {
	return slices.DeleteFunc(slices.Clone(in), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains(inheritedSessionVars, name)
	})
}
