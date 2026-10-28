// Package prompts renders the workflow prompts and the per-session context.
package prompts

import (
	"embed"
	"fmt"
	"strings"
	"text/template"
)

//go:embed *.md
var files embed.FS

var tmpl = template.Must(template.New("").Option("missingkey=error").ParseFS(files, "*.md"))

// Vars are available to every template; unused ones are ignored.
type Vars struct {
	Worktree, Branch, Base, Artifacts, Ticket string
	Slice, SliceID, SliceTitle, ParentID      string
	PRNumber                                  int
	PRURL                                     string
	OldBase                                   string
	Extra                                     string
}

// Kinds that have a template. "chat" sessions start from the user's own text.
var Kinds = []string{"research", "plan", "implement", "slice", "fix_ci", "review", "conflict", "comments"}

func Render(name string, v Vars) (string, error) {
	var b strings.Builder
	if err := tmpl.ExecuteTemplate(&b, name+".md", v); err != nil {
		return "", fmt.Errorf("prompt %s: %w", name, err)
	}
	return strings.TrimSpace(b.String()) + "\n", nil
}

// Context is appended to Claude Code's system prompt for every session.
func Context(v Vars) string {
	s, err := Render("context", v)
	if err != nil {
		panic(err) // the template is embedded; a failure here is a programming error
	}
	return s
}
