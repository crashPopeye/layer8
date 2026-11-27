package update_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crashPopeye/layer8/internal/update"
)

func fakeGitHub(t *testing.T, released bool) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/o/r/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer gh-token" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		if !released {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v0.2.0","assets":[{"id":11,"name":"latest.json"},{"id":12,"name":"layer8.app.tar.gz"},{"id":13,"name":"layer8_0.2.0_universal.dmg"}]}`))
	})
	mux.HandleFunc("GET /repos/o/r/releases/assets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/octet-stream" {
			http.Error(w, "json metadata", http.StatusTeapot)
			return
		}
		http.Redirect(w, r, "/storage/"+r.PathValue("id"), http.StatusFound)
	})
	mux.HandleFunc("GET /storage/11", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"version":"0.2.0","notes":"n","platforms":{
			"darwin-aarch64":{"signature":"sig","url":"https://github.com/o/r/releases/download/v0.2.0/layer8.app.tar.gz"},
			"darwin-x86_64":{"signature":"sig","url":"https://github.com/o/r/releases/download/v0.2.0/layer8.app.tar.gz"}}}`))
	})
	mux.HandleFunc("GET /storage/12", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("bundle")) })
	mux.HandleFunc("GET /storage/13", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("dmg")) })
	gh := httptest.NewServer(mux)
	t.Cleanup(gh.Close)
	return gh
}

func proxy(gh *httptest.Server) http.Handler {
	p := &update.Proxy{API: gh.URL, Repo: "o/r", Base: "http://127.0.0.1:7979", HTTP: gh.Client(),
		Token: func(context.Context) (string, error) { return "gh-token", nil }}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/update/latest.json", p.ServeManifest)
	mux.HandleFunc("GET /api/update/asset/{id}", p.ServeAsset)
	return mux
}

func get(h http.Handler, path string) (int, string) {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	body, _ := io.ReadAll(rec.Body)
	return rec.Code, string(body)
}

func TestManifestRewritesAssetsToTheDaemon(t *testing.T) {
	h := proxy(fakeGitHub(t, true))

	if code, _ := get(h, "/api/update/asset/12"); code != http.StatusNotFound {
		t.Fatalf("asset before any manifest: %d, want 404", code)
	}
	code, body := get(h, "/api/update/latest.json")
	if code != http.StatusOK {
		t.Fatalf("manifest: %d %s", code, body)
	}
	var m struct {
		Version   string
		Platforms map[string]struct{ URL, Signature string }
	}
	json.Unmarshal([]byte(body), &m)
	for _, key := range []string{"darwin-aarch64", "darwin-x86_64"} {
		if got := m.Platforms[key]; got.URL != "http://127.0.0.1:7979/api/update/asset/12" || got.Signature != "sig" {
			t.Errorf("%s = %+v", key, got)
		}
	}
	if code, body := get(h, "/api/update/asset/12"); code != http.StatusOK || body != "bundle" {
		t.Errorf("listed asset: %d %q", code, body)
	}
	if code, _ := get(h, "/api/update/asset/13"); code != http.StatusNotFound {
		t.Errorf("unlisted asset: %d, want 404", code)
	}
}

func TestNoReleaseIsNoUpdate(t *testing.T) {
	if code, body := get(proxy(fakeGitHub(t, false)), "/api/update/latest.json"); code != http.StatusNoContent {
		t.Fatalf("no release: %d %s, want 204", code, body)
	}
}
