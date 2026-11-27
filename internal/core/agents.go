package core

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/claude"
	"github.com/crashPopeye/layer8/internal/codex"
)

type capsEntry struct {
	mu      sync.Mutex
	caps    *claude.Capabilities
	fetched time.Time
}

// Capabilities lists the models, commands and skills a harness offers in a task's
// worktree. The first call asks the harness; later ones answer from the cache at once
// and refresh it in the background at most once a minute, so new skills still show up.
func (a *App) Capabilities(ctx context.Context, harness, taskID string) (claude.Capabilities, error) {
	t, err := a.Store.Task(taskID)
	if err != nil {
		return claude.Capabilities{}, err
	}
	probe := map[string]func(context.Context, string, string) (claude.Capabilities, error){"claude": claude.Probe, "codex": codex.Probe}[harness]
	if probe == nil {
		return claude.Capabilities{}, fmt.Errorf("unknown harness %q", harness)
	}
	v, _ := a.caps.LoadOrStore(harness+"\x00"+t.WorktreePath, &capsEntry{})
	e := v.(*capsEntry)
	fetch := func(ctx context.Context) (claude.Capabilities, error) {
		c, err := probe(ctx, "", t.WorktreePath)
		if err == nil {
			e.caps, e.fetched = &c, time.Now()
		}
		return c, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.caps == nil {
		return fetch(ctx)
	}
	if time.Since(e.fetched) > time.Minute {
		e.fetched = time.Now()
		go func() {
			ctx, cancel := context.WithTimeout(a.ctx, 30*time.Second)
			defer cancel()
			e.mu.Lock()
			defer e.mu.Unlock()
			_, _ = fetch(ctx)
		}()
	}
	return *e.caps, nil
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
