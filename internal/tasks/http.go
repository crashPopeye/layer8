package tasks

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/tree"
)

// ServeArtifact streams an artifact's raw content: GET /api/artifact?addr=artifact:<root>/<path>.
func (s *Service) ServeArtifact(w http.ResponseWriter, r *http.Request) {
	root, rel, err := s.artifactPath(r.Context(), tree.Addr(r.URL.Query().Get("addr")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	serveFrom(w, r, s.artifactsDir(root), rel)
}

// ServeFile streams a file of a task's worktree, or lists a folder as JSON: GET /api/file?task=<addr>&path=<path>.
func (s *Service) ServeFile(w http.ResponseWriter, r *http.Request) {
	t, err := s.Lookup(r.Context(), tree.Addr(r.URL.Query().Get("task")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	rel := r.URL.Query().Get("path")
	if rel == "" {
		rel = "."
	}
	serveFrom(w, r, t.Worktree, rel)
}

// entry is one line of a folder listing.
type entry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// serveFrom serves rel inside dir, refusing paths that leave dir, symlinks included.
func serveFrom(w http.ResponseWriter, r *http.Request, dir, rel string) {
	if rel != "." && !filepath.IsLocal(rel) {
		http.Error(w, "path escapes its folder", http.StatusBadRequest)
		return
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer root.Close()
	f, err := root.Open(rel)
	if errors.Is(err, fs.ErrNotExist) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		listFolder(w, f)
		return
	}
	if t := mediaType(rel); t != "" {
		if strings.HasPrefix(t, "text/") {
			t += "; charset=utf-8"
		}
		w.Header().Set("Content-Type", t)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox allow-scripts")
	http.ServeContent(w, r, rel, info.ModTime(), f)
}

func listFolder(w http.ResponseWriter, f *os.File) {
	dirents, err := f.ReadDir(-1)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	list := []entry{}
	for _, d := range dirents {
		e := entry{Name: d.Name(), Dir: d.IsDir()}
		if info, err := d.Info(); err == nil && !e.Dir {
			e.Size = info.Size()
		}
		list = append(list, e)
	}
	slices.SortFunc(list, func(a, b entry) int { return strings.Compare(a.Name, b.Name) })
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}
