package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/crashPopeye/layer8/internal/github"
)

// Release repository, set at build time with -ldflags -X.
var (
	ReleaseOwner = "crashPopeye"
	ReleaseRepo  = "layer8"
)

const manifestName = "latest.json"

// The repository is private, so the app's updater can't download from GitHub
// directly. It asks the daemon instead: the daemon fetches the latest release
// with the user's GitHub token and serves the manifest with download URLs that
// point back at itself. Tauri verifies every download against the signing key
// built into the app, so this proxy can't introduce an unsigned update.

type updateCache struct {
	mu      sync.Mutex
	assets  map[int64]bool // asset ids of the latest release seen
	fetched time.Time
}

var updates = &updateCache{assets: map[int64]bool{}}

func (a *App) updateManifest(ctx context.Context, base string) ([]byte, error) {
	gh, err := a.github()
	if err != nil {
		return nil, err
	}
	rel, err := gh.LatestRelease(ctx, ReleaseOwner, ReleaseRepo)
	if err != nil {
		return nil, err
	}
	byName := map[string]github.Asset{}
	updates.mu.Lock()
	updates.assets = map[int64]bool{}
	for _, as := range rel.Assets {
		byName[as.Name] = as
		updates.assets[as.ID] = true
	}
	updates.fetched = time.Now()
	updates.mu.Unlock()
	m, ok := byName[manifestName]
	if !ok {
		return nil, fmt.Errorf("release %s has no %s (it was published without update signing)", rel.TagName, manifestName)
	}
	var buf bytes.Buffer
	if err := gh.DownloadAsset(ctx, ReleaseOwner, ReleaseRepo, m.ID, &buf); err != nil {
		return nil, err
	}
	return RewriteManifest(buf.Bytes(), base, byName)
}

// RewriteManifest points each platform's download URL at the daemon's asset proxy,
// matching assets by file name. Everything else (version, signatures) is kept as is.
func RewriteManifest(raw []byte, base string, assets map[string]github.Asset) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("bad %s: %w", manifestName, err)
	}
	platforms, ok := m["platforms"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s has no platforms", manifestName)
	}
	for key, v := range platforms {
		p, ok := v.(map[string]any)
		if !ok {
			continue
		}
		raw, _ := p["url"].(string)
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("platform %s: bad url", key)
		}
		name, _ := url.PathUnescape(path.Base(u.Path))
		as, ok := assets[name]
		if !ok {
			return nil, fmt.Errorf("platform %s: release has no asset %q", key, name)
		}
		p["url"] = fmt.Sprintf("%s/api/update/asset/%d", base, as.ID)
	}
	return json.Marshal(m)
}

func (a *App) serveUpdateAsset(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	updates.mu.Lock()
	known := err == nil && updates.assets[id]
	updates.mu.Unlock()
	if !known {
		http.Error(w, "unknown asset", http.StatusNotFound)
		return
	}
	gh, err := a.github()
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	if err := gh.DownloadAsset(r.Context(), ReleaseOwner, ReleaseRepo, id, w); err != nil {
		// Headers may already be sent; the updater's signature check rejects a truncated file.
		var apiErr *github.APIError
		if errors.As(err, &apiErr) {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
	}
}
