// Package core ties layer8 together: workspaces, tasks, PR slices, sessions,
// GitHub tracking and background sync. The UI talks to it through api.go.
package core

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/prompts"
	"github.com/crashPopeye/layer8/internal/sessions"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/updater"
)

// Emitter delivers a named event to the UI (Wails events in the app, a no-op in tests).
type Emitter func(name string, data any)

type App struct {
	Paths    config.Paths
	Config   *config.Store
	Store    *store.Store
	Sessions *sessions.Manager
	Updater  *updater.Updater
	Version  string

	bus   *bus
	locks sync.Map // worktree path -> *sync.Mutex

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	pollMu   sync.Mutex
	syncMu   sync.Mutex
	lastSync time.Time
}

type Options struct {
	Paths   config.Paths
	Version string
	Start   sessions.StartFunc // nil = the real claude CLI
	Emit    Emitter
}

func New(o Options) (*App, error) {
	cfg, err := config.Load(o.Paths)
	if err != nil {
		return nil, err
	}
	st, err := store.Open(o.Paths.DB)
	if err != nil {
		return nil, err
	}
	if err := st.RecoverSessions(); err != nil {
		return nil, err
	}
	start := o.Start
	if start == nil {
		start = sessions.RealStart
	}
	emit := o.Emit
	if emit == nil {
		emit = func(string, any) {}
	}
	a := &App{Paths: o.Paths, Config: cfg, Store: st, Version: o.Version, bus: newBus(emit)}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.Sessions = sessions.New(st, a.bus, start, func() int { return a.Config.Get().MaxConcurrentSessions }, a.launch)
	a.Sessions.OnTurnEnd = a.onTurnEnd
	a.Updater = updater.New(o.Version, func() (string, error) { return a.githubToken() })
	return a, nil
}

// Run starts the background loops: GitHub polling, sync with the default branch, update checks.
func (a *App) Run() {
	a.loop("github", 90*time.Second, 5*time.Second, func() { a.PollGitHub(a.ctx) })
	a.loop("sync", time.Minute, 20*time.Second, func() {
		mins := a.Config.Get().SyncIntervalMinutes
		if mins > 0 && time.Since(a.lastSync) >= time.Duration(mins)*time.Minute {
			a.SyncAll(a.ctx)
		}
	})
	a.loop("update", 6*time.Hour, 30*time.Second, func() {
		if a.Config.Get().AutoUpdate {
			if _, err := a.Updater.Check(a.ctx); err != nil {
				log.Printf("update check: %v", err)
			}
			a.bus.Changed()
		}
	})
}

func (a *App) loop(name string, every, first time.Duration, fn func()) {
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		t := time.NewTimer(first)
		defer t.Stop()
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-t.C:
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("%s loop panic: %v", name, r)
						}
					}()
					fn()
				}()
				t.Reset(every)
			}
		}
	}()
}

// Shutdown stops loops and agent processes. Sessions are resumable after restart.
func (a *App) Shutdown() {
	a.cancel()
	a.Sessions.Shutdown()
	a.wg.Wait()
	a.bus.stop()
	a.Store.Close()
}

// lock serialises git operations on one worktree.
func (a *App) lock(path string) func() {
	m, _ := a.locks.LoadOrStore(path, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// launch tells the session manager how to start claude for a session.
func (a *App) launch(s store.Session) (sessions.Launch, error) {
	t, err := a.Store.Task(s.TaskID)
	if err != nil {
		return sessions.Launch{}, err
	}
	v := prompts.Vars{Worktree: t.WorktreePath, Branch: t.Branch, Base: t.BaseRef, Artifacts: t.ArtifactsPath}
	if t.Linear != nil {
		v.Ticket = t.Linear.Identifier + " (" + t.Linear.URL + ")"
	}
	cwd := t.WorktreePath
	if s.SliceID != "" {
		x, err := a.Store.Slice(s.SliceID)
		if err != nil {
			return sessions.Launch{}, err
		}
		cwd = x.WorktreePath
		v.Worktree, v.Branch, v.Slice = x.WorktreePath, x.Branch, x.Key+" ("+x.Title+")"
		v.Base = a.parentBranch(t, x)
	}
	return sessions.Launch{
		Cwd:                cwd,
		AddDirs:            []string{t.ArtifactsPath},
		AppendSystemPrompt: prompts.Context(v),
		Model:              a.Config.Get().Model,
	}, nil
}

// ---- UI event bus

type bus struct {
	emit    Emitter
	mu      sync.Mutex
	changed bool
	events  []store.Event
	stopCh  chan struct{}
	done    chan struct{}
}

func newBus(emit Emitter) *bus {
	b := &bus{emit: emit, stopCh: make(chan struct{}), done: make(chan struct{})}
	go b.run()
	return b
}

func (b *bus) Changed() {
	b.mu.Lock()
	b.changed = true
	b.mu.Unlock()
}

func (b *bus) Event(e store.Event) {
	b.mu.Lock()
	b.events = append(b.events, e)
	b.mu.Unlock()
}

// run batches: one "state" ping and one "events" batch at most every 120ms, so a
// chatty agent never floods the webview's event queue.
func (b *bus) run() {
	defer close(b.done)
	t := time.NewTicker(120 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-t.C:
			b.mu.Lock()
			changed, events := b.changed, b.events
			b.changed, b.events = false, nil
			b.mu.Unlock()
			if len(events) > 0 {
				for i := range events {
					events[i] = decorate(events[i])
				}
				b.emit("layer8:events", events)
			}
			if changed {
				b.emit("layer8:state", nil)
			}
		}
	}
}

func (b *bus) stop() {
	close(b.stopCh)
	<-b.done
}
