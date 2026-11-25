package task

import (
	"cmp"
	"context"
	"path"
	"slices"
	"strings"
)

// MaxFiles is how many paths Files returns at most.
const MaxFiles = 50

// Files lists up to MaxFiles paths of the task's worktree matching q, best matches first.
func (s *Service) Files(ctx context.Context, taskRef, q string) ([]string, error) {
	t, err := s.store.Task(taskRef)
	if err != nil {
		return nil, err
	}
	files, err := s.git.Files(ctx, t.Worktree)
	if err != nil {
		return nil, err
	}
	return matchFiles(files, q, MaxFiles), nil
}

// matchFiles keeps the paths containing q, case-insensitively, ranked by where
// q falls: at the start of the file name, elsewhere in it, then in a directory;
// shorter paths first within a rank.
func matchFiles(files []string, q string, limit int) []string {
	q = strings.ToLower(q)
	type hit struct {
		path string
		rank int
	}
	var hits []hit
	for _, f := range files {
		lower := strings.ToLower(f)
		base := path.Base(lower)
		switch {
		case strings.HasPrefix(base, q):
			hits = append(hits, hit{f, 0})
		case strings.Contains(base, q):
			hits = append(hits, hit{f, 1})
		case strings.Contains(lower, q):
			hits = append(hits, hit{f, 2})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(len(a.path), len(b.path)))
	})
	out := make([]string, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.path)
	}
	return out
}
