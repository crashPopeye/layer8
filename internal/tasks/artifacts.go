package tasks

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/tree"
)

func (s *Service) artifactsDir(root string) string {
	return filepath.Join(s.o.Home, "tasks", root)
}

// artifactPath splits artifact:<root>/<path> after checking that root is a root task and path stays inside its folder.
func (s *Service) artifactPath(ctx context.Context, a tree.Addr) (root, rel string, err error) {
	root, rel, _ = strings.Cut(a.Path(), "/")
	if !filepath.IsLocal(rel) || a.Kind() != "artifact" {
		return "", "", fmt.Errorf("%w: %s is not an artifact address", tree.ErrNotFound, a)
	}
	var n int
	s.db.QueryRowContext(ctx, `SELECT count(*) FROM tasks WHERE slug = ? AND parent_id IS NULL`, root).Scan(&n)
	if n == 0 || strings.Contains(root, "--") {
		return "", "", fmt.Errorf("%w: no task %s for %s", tree.ErrNotFound, root, a)
	}
	return root, path.Clean(rel), nil
}

func (s *Service) openRoot(root string) (*os.Root, error) {
	dir := s.artifactsDir(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenRoot(dir)
}

func (s *Service) getArtifact(ctx context.Context, a tree.Addr) (tree.Node, error) {
	root, rel, err := s.artifactPath(ctx, a)
	if err != nil {
		return tree.Node{}, err
	}
	r, err := s.openRoot(root)
	if err != nil {
		return tree.Node{}, err
	}
	defer r.Close()
	fields := map[string]any{"task": addrOf(root), "path": filepath.Join(s.artifactsDir(root), rel), "exists": false}
	info, err := r.Stat(rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return tree.Node{}, err
	case info.IsDir():
		return tree.Node{}, fmt.Errorf("%w: %s is a folder", tree.ErrNotFound, a)
	default:
		fields["exists"] = true
		fields["size"] = info.Size()
		fields["modified"] = info.ModTime().UTC().Format(time.RFC3339)
		fields["type"] = cmp.Or(mediaType(rel), "application/octet-stream")
		fields["url"] = "/api/artifact?addr=" + url.QueryEscape(string(a))
	}
	return tree.Node{Title: rel, Fields: fields}, nil
}

func (s *Service) writeArtifact(ctx context.Context, n tree.Node, args json.RawMessage, _ tree.Actor) (tree.Result, error) {
	var a struct{ Content string }
	json.Unmarshal(args, &a)
	root, rel, err := s.artifactPath(ctx, n.Addr)
	if err != nil {
		return tree.Result{}, err
	}
	r, err := s.openRoot(root)
	if err != nil {
		return tree.Result{}, err
	}
	defer r.Close()
	if err := r.MkdirAll(path.Dir(rel), 0o755); err != nil {
		return tree.Result{}, err
	}
	if err := r.WriteFile(rel, []byte(a.Content), 0o644); err != nil {
		return tree.Result{}, err
	}
	s.artifactsChanged(ctx, root)
	return tree.Result{Message: fmt.Sprintf("wrote %d bytes to %s", len(a.Content), n.Addr)}, nil
}

func (s *Service) deleteArtifact(ctx context.Context, n tree.Node, _ json.RawMessage, _ tree.Actor) (tree.Result, error) {
	root, rel, err := s.artifactPath(ctx, n.Addr)
	if err != nil {
		return tree.Result{}, err
	}
	r, err := s.openRoot(root)
	if err != nil {
		return tree.Result{}, err
	}
	defer r.Close()
	if err := r.Remove(rel); err != nil {
		return tree.Result{}, err
	}
	s.artifactsChanged(ctx, root)
	return tree.Result{Message: "deleted " + string(n.Addr)}, nil
}

// listArtifacts returns the files in a tree's artifacts folder, skipping dot files.
func (s *Service) listArtifacts(root string) ([]tree.Summary, error) {
	list := []tree.Summary{}
	err := s.walkArtifacts(root, func(rel string, _ fs.FileInfo) {
		list = append(list, tree.Summary{Addr: tree.Addr("artifact:" + root + "/" + rel), Kind: "artifact", Title: rel})
	})
	return list, err
}

func (s *Service) walkArtifacts(root string, fn func(rel string, info fs.FileInfo)) error {
	err := fs.WalkDir(os.DirFS(s.artifactsDir(root)), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") && rel != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			fn(rel, info)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// artifactsChanged reports the tree of root as changed: every task in it and the artifacts listed.
func (s *Service) artifactsChanged(ctx context.Context, root string, artifacts ...tree.Addr) {
	list, _ := s.subtree(ctx, root)
	for _, t := range list {
		artifacts = append(artifacts, t.Addr())
	}
	s.tree.Changed(artifacts...)
}

// Watch reports artifacts created, changed or deleted on disk, polling every interval until ctx ends.
func (s *Service) Watch(ctx context.Context, every time.Duration) {
	seen := s.snapshot()
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		now := s.snapshot()
		changed := map[string][]tree.Addr{}
		for key, stamp := range now {
			if seen[key] != stamp {
				root, _, _ := strings.Cut(key, "/")
				changed[root] = append(changed[root], tree.Addr("artifact:"+key))
			}
		}
		for key := range seen {
			if _, ok := now[key]; !ok {
				root, _, _ := strings.Cut(key, "/")
				changed[root] = append(changed[root], tree.Addr("artifact:"+key))
			}
		}
		for root, addrs := range changed {
			s.artifactsChanged(ctx, root, addrs...)
		}
		seen = now
	}
}

// snapshot maps <root>/<path> of every artifact to its size and modification time.
func (s *Service) snapshot() map[string]string {
	out := map[string]string{}
	roots, _ := os.ReadDir(filepath.Join(s.o.Home, "tasks"))
	for _, r := range roots {
		if !r.IsDir() {
			continue
		}
		s.walkArtifacts(r.Name(), func(rel string, info fs.FileInfo) {
			out[r.Name()+"/"+rel] = fmt.Sprint(info.Size(), info.ModTime().UnixNano())
		})
	}
	return out
}

var mediaTypes = map[string]string{
	".md": "text/markdown", ".markdown": "text/markdown", ".mmd": "text/plain",
	".ts": "text/plain", ".tsx": "text/plain", ".mts": "text/plain",
}

// mediaType returns the media type for a file name without parameters, or "" when unknown.
func mediaType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if t, ok := mediaTypes[ext]; ok {
		return t
	}
	t, _, _ := mime.ParseMediaType(mime.TypeByExtension(ext))
	return t
}
