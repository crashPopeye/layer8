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

type cachedCaps struct {
	caps claude.Capabilities
	at   time.Time
}

// Capabilities lists the models and commands a harness offers in a task's worktree.
func (a *App) Capabilities(ctx context.Context, harness, taskID string) (claude.Capabilities, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return claude.Capabilities{}, err
	}
	key := harness + "\x00" + t.WorktreePath
	if c, ok := a.caps.Load(key); ok && time.Since(c.(cachedCaps).at) < 10*time.Minute {
		return c.(cachedCaps).caps, nil
	}
	var caps claude.Capabilities
	switch harness {
	case "claude":
		caps, err = claude.Probe(ctx, "", t.WorktreePath)
	case "codex":
		caps, err = codex.Probe(ctx, "", t.WorktreePath)
	default:
		return caps, fmt.Errorf("unknown harness %q", harness)
	}
	if err != nil {
		return caps, err
	}
	a.caps.Store(key, cachedCaps{caps, time.Now()})
	return caps, nil
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
