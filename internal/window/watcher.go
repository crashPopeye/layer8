package window

import (
	"context"
	"strconv"
	"time"

	"github.com/crashPopeye/layer8/internal/client"
)

// watcher keeps the menu-bar badge and notifications in step with the daemon's events.
type watcher struct {
	client  *client.Client
	badge   func(label string)
	focused func() bool
	notify  func(title, body string)
}

// run follows the daemon's event stream until ctx is done, reconnecting after a daemon restart.
func (w watcher) run(ctx context.Context) {
	for ctx.Err() == nil {
		w.refresh()
		w.client.Subscribe(ctx, w.handle)
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}

func (w watcher) handle(e client.Event) {
	switch e.Kind {
	case "session", "approval":
		w.refresh()
	case "attention":
		w.refresh()
		if w.notify != nil && !w.focused() {
			w.notify(w.message(e))
		}
	}
}

func (w watcher) refresh() {
	if n, err := w.client.Attention(); err == nil {
		w.badge(badgeLabel(n))
	}
}

// message is the notification for a session that needs the user.
func (w watcher) message(e client.Event) (title, body string) {
	title = "layer8"
	if t, err := w.client.Task(e.TaskID); err == nil {
		title = t.Title
	}
	if e.Status == "waiting_approval" {
		return title, e.Label + " is waiting for your approval"
	}
	return title, e.Label + " finished its turn"
}

func badgeLabel(waiting int) string {
	if waiting == 0 {
		return ""
	}
	return strconv.Itoa(waiting)
}
