package window

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crashPopeye/layer8/internal/client"
)

func TestWatcherBadgesAndNotifies(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/attention", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"waiting":2}`)) })
	mux.HandleFunc("GET /api/tasks/t1", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"id":"t1","title":"Smoke task"}`)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var badge string
	var notes [][2]string
	focused := true
	w := watcher{
		client:  client.New(srv.URL, "tok"),
		badge:   func(label string) { badge = label },
		focused: func() bool { return focused },
		notify:  func(title, body string) { notes = append(notes, [2]string{title, body}) },
	}
	attention := client.Event{Kind: "attention", ID: "s1", TaskID: "t1", Status: "waiting_approval", Label: "research"}
	w.handle(attention)
	if badge != "2" || len(notes) != 0 {
		t.Fatalf("focused: badge %q, notes %v", badge, notes)
	}
	focused = false
	w.handle(attention)
	if len(notes) != 1 || notes[0] != [2]string{"Smoke task", "research is waiting for your approval"} {
		t.Fatalf("unfocused notes = %v", notes)
	}
	if badgeLabel(0) != "" {
		t.Fatal("empty badge should have no label")
	}
}
