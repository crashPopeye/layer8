// Package update serves the app's update feed from the latest GitHub release of a private repository through the daemon.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

// Proxy serves the release's latest.json with its asset URLs pointing back at the daemon, and streams those assets.
type Proxy struct {
	API   string
	Repo  string
	Base  string
	HTTP  *http.Client
	Token func(ctx context.Context) (string, error)

	mu      sync.Mutex
	allowed map[int64]bool
}

type asset struct {
	ID   int64
	Name string
}

// ServeManifest answers GET /api/update/latest.json, or 204 when the repository has no release.
func (p *Proxy) ServeManifest(w http.ResponseWriter, r *http.Request) {
	manifest, err := p.manifest(r.Context())
	if err == errNoRelease {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manifest)
}

// ServeAsset answers GET /api/update/asset/{id} for the assets the last served manifest listed.
func (p *Proxy) ServeAsset(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	p.mu.Lock()
	ok := p.allowed[id]
	p.mu.Unlock()
	if !ok {
		http.Error(w, "unknown update asset", http.StatusNotFound)
		return
	}
	res, err := p.download(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer res.Body.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	if res.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(res.ContentLength, 10))
	}
	io.Copy(w, res.Body)
}

var errNoRelease = fmt.Errorf("no release")

func (p *Proxy) manifest(ctx context.Context) (map[string]any, error) {
	res, err := p.get(ctx, "/repos/"+p.Repo+"/releases/latest", "application/vnd.github+json")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, errNoRelease
	}
	var release struct{ Assets []asset }
	if err := json.NewDecoder(res.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("latest release: %w", err)
	}
	byName := map[string]int64{}
	for _, a := range release.Assets {
		byName[a.Name] = a.ID
	}
	id, ok := byName["latest.json"]
	if !ok {
		return nil, fmt.Errorf("the latest release of %s has no latest.json", p.Repo)
	}
	body, err := p.download(ctx, id)
	if err != nil {
		return nil, err
	}
	defer body.Body.Close()
	var manifest map[string]any
	if err := json.NewDecoder(body.Body).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("latest.json: %w", err)
	}
	platforms, _ := manifest["platforms"].(map[string]any)
	allowed := map[int64]bool{}
	for name, v := range platforms {
		platform, _ := v.(map[string]any)
		url, _ := platform["url"].(string)
		id, ok := byName[path.Base(url)]
		if !ok {
			return nil, fmt.Errorf("latest.json platform %s points at %q, which is not an asset of the release", name, url)
		}
		platform["url"] = fmt.Sprintf("%s/api/update/asset/%d", p.Base, id)
		allowed[id] = true
	}
	p.mu.Lock()
	p.allowed = allowed
	p.mu.Unlock()
	return manifest, nil
}

func (p *Proxy) download(ctx context.Context, id int64) (*http.Response, error) {
	res, err := p.get(ctx, fmt.Sprintf("/repos/%s/releases/assets/%d", p.Repo, id), "application/octet-stream")
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close()
		return nil, fmt.Errorf("release asset %d: %s", id, res.Status)
	}
	return res, nil
}

func (p *Proxy) get(ctx context.Context, path, accept string) (*http.Response, error) {
	token, err := p.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimSuffix(p.API, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := p.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotFound {
		res.Body.Close()
		return nil, fmt.Errorf("github GET %s: %s", path, res.Status)
	}
	return res, nil
}
