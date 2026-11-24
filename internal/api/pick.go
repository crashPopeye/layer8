package api

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// pickFolder shows the native macOS folder chooser and returns the chosen path,
// or "" when the user cancels.
func pickFolder(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "/usr/bin/osascript",
		"-e", "activate",
		"-e", `POSIX path of (choose folder with prompt "Add a project to layer8")`).Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) && strings.Contains(string(exit.Stderr), "-128") {
		return "", nil
	}
	return strings.TrimSuffix(strings.TrimSpace(string(out)), "/"), err
}
