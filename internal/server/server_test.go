package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func newTestServer(t *testing.T) (*httptest.Server, *Hub) {
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"ok":true}`)) })
	hub := NewHub()
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>")}}
	ts := httptest.NewServer(New("secret", 7878, api, ui, hub).Handler())
	t.Cleanup(ts.Close)
	return ts, hub
}

func do(t *testing.T, ts *httptest.Server, path string, hdr map[string]string) int {
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	for k, v := range hdr {
		if k == "Host" {
			req.Host = v
		} else {
			req.Header.Set(k, v)
		}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res.StatusCode
}

func TestAccessRules(t *testing.T) {
	ts, _ := newTestServer(t)
	auth := map[string]string{"Authorization": "Bearer secret"}
	cases := []struct {
		name string
		path string
		hdr  map[string]string
		want int
	}{
		{"ui is public", "/", nil, 200},
		{"api needs token", "/api/state", nil, 401},
		{"wrong token", "/api/state", map[string]string{"Authorization": "Bearer nope"}, 401},
		{"token ok", "/api/state", auth, 200},
		{"query token (EventSource)", "/api/state?token=secret", nil, 200},
		{"tauri origin", "/api/state", map[string]string{"Authorization": "Bearer secret", "Origin": "tauri://localhost"}, 200},
		{"own origin", "/api/state", map[string]string{"Authorization": "Bearer secret", "Origin": "http://127.0.0.1:7878"}, 200},
		{"foreign origin", "/api/state", map[string]string{"Authorization": "Bearer secret", "Origin": "https://evil.example"}, 403},
		{"dns rebinding host", "/api/state", map[string]string{"Authorization": "Bearer secret", "Host": "evil.example:7878"}, 403},
	}
	for _, c := range cases {
		if got := do(t, ts, c.path, c.hdr); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestEventsStream(t *testing.T) {
	ts, hub := newTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events?token=secret", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatal(ct)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Scan() // ": connected"
	go func() {
		time.Sleep(50 * time.Millisecond)
		hub.Emit("layer8:state", map[string]int{"n": 1})
	}()
	var got []string
	for sc.Scan() && len(got) < 2 {
		if line := sc.Text(); line != "" {
			got = append(got, line)
		}
	}
	if strings.Join(got, "|") != `event: layer8:state|data: {"n":1}` {
		t.Fatalf("got %q", got)
	}
}
