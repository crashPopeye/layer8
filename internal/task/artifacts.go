package task

import (
	"bufio"
	"bytes"
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/crashPopeye/layer8/internal/store"
)

// Artifact is one file of a task's artifacts folder.
type Artifact struct {
	Path    string `json:"path"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"`
}

// Artifacts lists a task's artifacts, newest first, each labelled by its frontmatter type.
// The parent link and dotfiles are left out.
func (s *Service) Artifacts(taskRef string) ([]Artifact, error) {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return nil, err
	}
	root := s.ArtifactsDir(t.ID)
	artifacts := []Artifact{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			return skip(d)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		artifacts = append(artifacts, Artifact{
			Path:    filepath.ToSlash(rel),
			Type:    artifactType(p),
			Size:    info.Size(),
			ModTime: info.ModTime().UnixMilli(),
		})
		return nil
	})
	if os.IsNotExist(err) {
		return artifacts, nil
	}
	slices.SortStableFunc(artifacts, func(a, b Artifact) int {
		return cmp.Or(cmp.Compare(b.ModTime, a.ModTime), strings.Compare(a.Path, b.Path))
	})
	return artifacts, err
}

func skip(d fs.DirEntry) error {
	if d.IsDir() {
		return filepath.SkipDir
	}
	return nil
}

// ReadArtifact returns the content of the artifact at rel; paths under parent/ read the parent's.
func (s *Service) ReadArtifact(taskRef, rel string) ([]byte, error) {
	p, _, err := s.artifactPath(taskRef, rel)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("%w: no artifact %s", store.ErrNotFound, rel)
	}
	return data, err
}

// WriteArtifact creates or replaces the artifact at rel and announces the change.
func (s *Service) WriteArtifact(taskRef, rel string, data []byte) (Artifact, error) {
	p, taskID, err := s.artifactPath(taskRef, rel)
	if err != nil {
		return Artifact{}, err
	}
	if first, _, _ := strings.Cut(path.Clean(rel), "/"); first == "parent" {
		return Artifact{}, fmt.Errorf("%w: parent artifacts are read-only; write them with --task <parent>", ErrInvalid)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return Artifact{}, err
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return Artifact{}, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return Artifact{}, err
	}
	s.notify("artifacts", taskID)
	return Artifact{Path: path.Clean(rel), Type: artifactType(p), Size: info.Size(), ModTime: info.ModTime().UnixMilli()}, nil
}

// artifactPath resolves rel inside the task's artifacts folder and rejects paths that leave it.
func (s *Service) artifactPath(taskRef, rel string) (string, string, error) {
	local := filepath.FromSlash(rel)
	if !filepath.IsLocal(local) {
		return "", "", fmt.Errorf("%w: artifact path %q must stay inside the artifacts folder", ErrInvalid, rel)
	}
	t, err := s.store.Task(taskRef)
	if err != nil {
		return "", "", err
	}
	return filepath.Join(s.ArtifactsDir(t.ID), local), t.ID, nil
}

var namedTypes = map[string]string{"task.md": "task", "ticket.md": "ticket"}

// artifactType is the frontmatter "type" of a Markdown file, else what its name or extension says.
func artifactType(p string) string {
	if t := frontmatterType(p); t != "" {
		return t
	}
	if t, ok := namedTypes[filepath.Base(p)]; ok {
		return t
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(p)), ".")
}

func frontmatterType(p string) string {
	if !strings.EqualFold(filepath.Ext(p), ".md") {
		return ""
	}
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	if !lines.Scan() || lines.Text() != "---" {
		return ""
	}
	for lines.Scan() && lines.Text() != "---" {
		if v, ok := bytes.CutPrefix(lines.Bytes(), []byte("type:")); ok {
			return strings.Trim(strings.TrimSpace(string(v)), `"'`)
		}
	}
	return ""
}
