package ui_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crashPopeye/layer8/internal/projects"
	"github.com/crashPopeye/layer8/internal/store"
	"github.com/crashPopeye/layer8/internal/tree"
	"github.com/crashPopeye/layer8/internal/ui"
)

type event struct {
	name string
	data string
}

type env struct {
	t      *testing.T
	tree   *tree.Tree
	svc    *ui.Service
	home   string
	mu     sync.Mutex
	events []event
}

func newEnv(t *testing.T) *env {
	home := t.TempDir()
	db, err := store.Open(filepath.Join(home, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, tree: tree.New(db), home: home}
	projects.Register(e.tree, db, "test")
	e.svc = ui.Register(e.tree, ui.Options{Home: home, Publish: func(name string, data any) {
		b, _ := json.Marshal(data)
		e.mu.Lock()
		e.events = append(e.events, event{name, string(b)})
		e.mu.Unlock()
	}})
	return e
}

func (e *env) do(target tree.Addr, action string, args string) tree.Result {
	e.t.Helper()
	return e.tree.Do(e.t.Context(), tree.Actor{Kind: "user"}, []tree.Op{{Target: target, Action: action, Args: json.RawMessage(args)}})[0]
}

func (e *env) ok(target tree.Addr, action string, args string) tree.Result {
	e.t.Helper()
	r := e.do(target, action, args)
	if !r.OK {
		e.t.Fatalf("%s %s: %s", target, action, r.Message)
	}
	return r
}

func (e *env) fails(target tree.Addr, action, args, want string) {
	e.t.Helper()
	if r := e.do(target, action, args); r.OK || !strings.Contains(r.Message, want) {
		e.t.Fatalf("%s %s %s = %+v, want an error containing %q", target, action, args, r, want)
	}
}

func (e *env) fields(addr tree.Addr) map[string]any {
	e.t.Helper()
	n, err := e.tree.Get(e.t.Context(), tree.Actor{Kind: "user"}, addr, 0)
	if err != nil {
		e.t.Fatal(err)
	}
	var f map[string]any
	b, _ := json.Marshal(n.Fields)
	json.Unmarshal(b, &f)
	return f
}

func (e *env) last() event {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.events) == 0 {
		e.t.Fatal("no event published")
	}
	return e.events[len(e.events)-1]
}

func TestAppListsTheUINodes(t *testing.T) {
	e := newEnv(t)
	n, err := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, "app", 1)
	if err != nil {
		t.Fatal(err)
	}
	var got []tree.Addr
	for _, c := range n.Children {
		got = append(got, c.Addr)
	}
	if !slices.Equal(got, []tree.Addr{"ui:layout", "ui:theme", "ui:plugins"}) {
		t.Fatalf("app children = %v", got)
	}
	if _, err := e.tree.Get(t.Context(), tree.Actor{Kind: "user"}, "ui:nope", 0); err == nil {
		t.Fatal("ui:nope must not exist")
	}
}

func TestLayoutPresets(t *testing.T) {
	e := newEnv(t)
	e.fails("ui:layout", "save-preset", `{"name":"Mine"}`, "not saved a layout")
	e.ok("ui:layout", "save-layout", `{"layout":{"grid":1}}`)
	e.fails("ui:layout", "save-preset", `{"name":"Focus"}`, "built-in")
	e.fails("ui:layout", "save-preset", `{"name":"../x"}`, "letters")
	e.ok("ui:layout", "save-preset", `{"name":"Mine"}`)

	f := e.fields("ui:layout")
	if got, _ := json.Marshal(f["presets"]); string(got) != `["Default","Focus","Review","Wide diff","Mine"]` {
		t.Errorf("presets = %s", got)
	}
	if got, _ := json.Marshal(f["layout"]); string(got) != `{"grid":1}` {
		t.Errorf("layout = %s", got)
	}

	e.ok("ui:layout", "preset", `{"name":"Mine"}`)
	if ev := e.last(); ev.name != "ui" || ev.data != `{"layout":{"grid":1},"name":"Mine","verb":"preset"}` {
		t.Errorf("preset event = %+v", ev)
	}
	e.ok("ui:layout", "preset", `{"name":"Review"}`)
	if ev := e.last(); ev.data != `{"name":"Review","verb":"preset"}` {
		t.Errorf("built-in preset event = %+v", ev)
	}
	e.fails("ui:layout", "preset", `{"name":"Nope"}`, "no preset")
	e.ok("ui:layout", "delete-preset", `{"name":"Mine"}`)
	e.fails("ui:layout", "delete-preset", `{"name":"Mine"}`, "no saved preset")

	data, err := os.ReadFile(filepath.Join(e.home, "ui.json"))
	if err != nil || !strings.Contains(string(data), `"grid": 1`) {
		t.Fatalf("ui.json = %s, %v", data, err)
	}
}

func TestWindowCommands(t *testing.T) {
	e := newEnv(t)
	e.ok("ui:layout", "open-panel", `{"panel":"open-prs","position":"right"}`)
	if ev := e.last(); ev.name != "ui" || ev.data != `{"panel":"open-prs","position":"right","verb":"open-panel"}` {
		t.Errorf("open-panel event = %+v", ev)
	}
	e.fails("ui:layout", "open-panel", `{"panel":"x","position":"top"}`, "must be one of")
	e.ok("ui:layout", "focus", `{"addr":"task:eng-1"}`)
	if ev := e.last(); ev.data != `{"addr":"task:eng-1","verb":"focus"}` {
		t.Errorf("focus event = %+v", ev)
	}
	e.ok("ui:layout", "notify", `{"text":"done"}`)
	if ev := e.last(); ev.name != "toast" || ev.data != `{"level":"info","text":"done"}` {
		t.Errorf("notify event = %+v", ev)
	}
	e.ok("ui:plugins", "reload", `{}`)
	if ev := e.last(); ev.data != `{"verb":"reload-plugins"}` {
		t.Errorf("reload event = %+v", ev)
	}
	e.fails("ui:theme", "open-panel", `{"panel":"x"}`, "not available")
}

func TestThemes(t *testing.T) {
	e := newEnv(t)
	f := e.fields("ui:theme")
	if f["theme"] != "layer8" || f["mode"] != "dark" || f["accent"] != "" {
		t.Fatalf("defaults = %v %v %v", f["theme"], f["mode"], f["accent"])
	}
	if got, _ := json.Marshal(f["builtin"]); string(got) != `["ember","forest","graphite","layer8","ocean"]` {
		t.Errorf("builtin = %s", got)
	}

	e.fails("ui:theme", "set", `{"theme":"nope"}`, "no theme")
	e.fails("ui:theme", "set", `{"mode":"dim"}`, "must be one of")
	e.ok("ui:theme", "set", `{"theme":"ocean","mode":"system"}`)
	e.fails("ui:theme", "set-accent", `{"color":"red"}`, "hex")
	e.ok("ui:theme", "set-accent", `{"color":"#ff0066"}`)

	e.fails("ui:theme", "save", `{"name":"ocean","tokens":{}}`, "built-in")
	e.fails("ui:theme", "save", `{"name":"mine","tokens":{"dark":{"primary":"red;}"}}}`, "not a CSS token")
	e.fails("ui:theme", "save", `{"name":"mine","tokens":{"dusk":{}}}`, "unknown field")
	e.ok("ui:theme", "save", `{"name":"mine","tokens":{"shared":{"radius":"0"},"dark":{"primary":"#123456"}}}`)
	os.WriteFile(filepath.Join(e.home, "themes", "broken.json"), []byte("{"), 0o644)

	f = e.fields("ui:theme")
	if f["theme"] != "mine" || f["mode"] != "system" || f["accent"] != "#ff0066" {
		t.Errorf("after save = %v %v %v", f["theme"], f["mode"], f["accent"])
	}
	if got, _ := json.Marshal(f["themes"].(map[string]any)["mine"]); string(got) != `{"dark":{"primary":"#123456"},"shared":{"radius":"0"}}` {
		t.Errorf("mine = %s", got)
	}
	if _, ok := f["errors"].(map[string]any)["broken"]; !ok {
		t.Errorf("errors = %v", f["errors"])
	}
}

func TestPlugins(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(e.home, "plugins")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "open-prs.js"), []byte("export default () => {}"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)

	plugins := func() []ui.Plugin { return e.svc.Plugins() }
	if p := plugins(); len(p) != 1 || p[0].Name != "open-prs" || p[0].Status != "pending" || p[0].URL != "/api/plugins/open-prs.js" {
		t.Fatalf("plugins = %+v", p)
	}
	e.fails("ui:plugins", "report", `{"plugin":"ghost"}`, "no plugin")
	e.ok("ui:plugins", "report", `{"plugin":"open-prs","panels":["open-prs"]}`)
	if p := plugins()[0]; p.Status != "loaded" || !slices.Equal(p.Panels, []string{"open-prs"}) {
		t.Fatalf("after report = %+v", p)
	}
	if sdk, _ := e.fields("ui:plugins")["sdk"].(string); !strings.Contains(sdk, "registerPanel") {
		t.Errorf("sdk field = %q", sdk)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	changed := make(chan []tree.Addr, 4)
	e.tree.OnChange = func(addrs []tree.Addr) { changed <- addrs }
	go e.svc.Watch(ctx, 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	os.WriteFile(filepath.Join(dir, "open-prs.js"), []byte("export default function () { throw 1 }"), 0o644)
	select {
	case addrs := <-changed:
		if !slices.Equal(addrs, []tree.Addr{"ui:plugins"}) {
			t.Errorf("changed = %v", addrs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change published after editing the plugin")
	}
	if p := plugins()[0]; p.Status != "pending" || p.Error != "" {
		t.Errorf("an edited plugin must drop its old report: %+v", p)
	}
	e.ok("ui:plugins", "report", `{"plugin":"open-prs","error":"boom"}`)
	if p := plugins()[0]; p.Status != "failed" || p.Error != "boom" {
		t.Errorf("failed report = %+v", p)
	}
}

func TestServePlugin(t *testing.T) {
	e := newEnv(t)
	os.MkdirAll(filepath.Join(e.home, "plugins"), 0o755)
	os.WriteFile(filepath.Join(e.home, "plugins", "a.js"), []byte("export default 1"), 0o644)
	os.WriteFile(filepath.Join(e.home, "secret.js"), []byte("nope"), 0o644)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/plugins/{file}", e.svc.ServePlugin)

	get := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	if w := get("/api/plugins/a.js"); w.Code != 200 || w.Body.String() != "export default 1" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/javascript") {
		t.Errorf("a.js = %d %q %q", w.Code, w.Body, w.Header())
	}
	if w := get("/api/plugins/b.js"); w.Code != 404 {
		t.Errorf("missing plugin = %d", w.Code)
	}
	for _, bad := range []string{"/api/plugins/..%2Fsecret.js", "/api/plugins/a.txt"} {
		if w := get(bad); w.Code == 200 {
			t.Errorf("%s served %q", bad, w.Body)
		}
	}
}
