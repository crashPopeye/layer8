// Package diff parses `git diff` output into files, hunks and numbered lines.
package diff

import (
	"strconv"
	"strings"
)

type LineKind string

const (
	Context LineKind = "ctx"
	Add     LineKind = "add"
	Del     LineKind = "del"
)

type Line struct {
	Kind LineKind `json:"kind"`
	Old  int      `json:"old,omitempty"` // 0 for added lines
	New  int      `json:"new,omitempty"` // 0 for deleted lines
	Text string   `json:"text"`
}

type Hunk struct {
	Header string `json:"header"`
	Lines  []Line `json:"lines"`
}

type File struct {
	OldPath string `json:"oldPath"`
	NewPath string `json:"newPath"`
	Status  string `json:"status"` // added, deleted, renamed, modified
	Binary  bool   `json:"binary"`
	Adds    int    `json:"adds"`
	Dels    int    `json:"dels"`
	Hunks   []Hunk `json:"hunks"`
}

// Path is the path a reviewer refers to: the new one unless the file was deleted.
func (f File) Path() string {
	if f.Status == "deleted" {
		return f.OldPath
	}
	return f.NewPath
}

// Parse reads output of `git diff --no-color --no-ext-diff` (default prefixes a/ b/).
func Parse(out string) []File {
	var files []File
	var f *File
	var h *Hunk
	oldN, newN := 0, 0
	flush := func() {
		if f != nil {
			files = append(files, *f)
		}
	}
	for _, raw := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(raw, "diff --git "):
			flush()
			a, b := splitGitPaths(strings.TrimPrefix(raw, "diff --git "))
			f = &File{OldPath: a, NewPath: b, Status: "modified"}
			h = nil
		case f == nil:
			continue
		case h == nil && strings.HasPrefix(raw, "new file mode"):
			f.Status = "added"
		case h == nil && strings.HasPrefix(raw, "deleted file mode"):
			f.Status = "deleted"
		case h == nil && strings.HasPrefix(raw, "rename from "):
			f.Status, f.OldPath = "renamed", unquote(strings.TrimPrefix(raw, "rename from "))
		case h == nil && strings.HasPrefix(raw, "rename to "):
			f.NewPath = unquote(strings.TrimPrefix(raw, "rename to "))
		case h == nil && strings.HasPrefix(raw, "Binary files "):
			f.Binary = true
		case h == nil && (strings.HasPrefix(raw, "--- ") || strings.HasPrefix(raw, "+++ ")):
			// Paths already known from the diff --git line; /dev/null is covered by the mode lines.
		case strings.HasPrefix(raw, "@@"):
			f.Hunks = append(f.Hunks, Hunk{Header: raw})
			h = &f.Hunks[len(f.Hunks)-1]
			oldN, newN = parseHunkHeader(raw)
		case h == nil:
			continue
		case strings.HasPrefix(raw, "+"):
			h.Lines = append(h.Lines, Line{Kind: Add, New: newN, Text: raw[1:]})
			newN++
			f.Adds++
		case strings.HasPrefix(raw, "-"):
			h.Lines = append(h.Lines, Line{Kind: Del, Old: oldN, Text: raw[1:]})
			oldN++
			f.Dels++
		case strings.HasPrefix(raw, " "):
			h.Lines = append(h.Lines, Line{Kind: Context, Old: oldN, New: newN, Text: raw[1:]})
			oldN++
			newN++
		case raw == "":
			// Trailing newline of the whole output, or an empty context line from tools
			// that strip the leading space; neither advances counters reliably, so skip.
		default:
			// "\ No newline at end of file"
		}
	}
	flush()
	return files
}

// parseHunkHeader reads "@@ -a,b +c,d @@" and returns the starting line numbers.
func parseHunkHeader(h string) (int, int) {
	parts := strings.Fields(h)
	if len(parts) < 3 {
		return 0, 0
	}
	start := func(s string) int {
		s = strings.TrimLeft(s, "-+")
		if i := strings.IndexByte(s, ','); i >= 0 {
			s = s[:i]
		}
		n, _ := strconv.Atoi(s)
		return n
	}
	return start(parts[1]), start(parts[2])
}

// splitGitPaths splits `a/x b/y` (or quoted forms) into x and y.
func splitGitPaths(s string) (string, string) {
	if strings.HasPrefix(s, `"`) {
		if end := closingQuote(s); end > 0 {
			a := unquote(s[:end+1])
			b := unquote(strings.TrimSpace(s[end+1:]))
			return strings.TrimPrefix(a, "a/"), strings.TrimPrefix(b, "b/")
		}
	}
	// Unquoted: paths are identical except for the prefix unless renamed; split on " b/".
	if i := strings.Index(s, " b/"); i >= 0 {
		return strings.TrimPrefix(s[:i], "a/"), s[i+3:]
	}
	return s, s
}

func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '"' {
			return i
		}
	}
	return -1
}

func unquote(s string) string {
	if strings.HasPrefix(s, `"`) {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}
