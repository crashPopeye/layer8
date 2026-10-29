package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/codex"
)

// Capabilities lists the models and commands a harness offers in a task's worktree.
// It asks the harness each time, so skills added since show up.
func (a *App) Capabilities(ctx context.Context, harness, taskID string) (claude.Capabilities, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return claude.Capabilities{}, err
	}
	switch harness {
	case "claude":
		return claude.Probe(ctx, "", t.WorktreePath)
	case "codex":
		return codex.Probe(ctx, "", t.WorktreePath)
	default:
		return claude.Capabilities{}, fmt.Errorf("unknown harness %q", harness)
	}
}

// Attach saves a file in the task's artifacts, where agents can read it, and returns its path.
func (a *App) Attach(taskID, name string, r io.Reader) (string, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(t.ArtifactsPath, "attachments")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name = filepath.Base(name)
	if name == "." || name == string(filepath.Separator) {
		name = "file"
	}
	path := filepath.Join(dir, time.Now().Format("150405.000-")+name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	_, err = io.Copy(f, io.LimitReader(r, 50<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return path, err
}
