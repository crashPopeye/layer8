package prompts

import (
	"strings"
	"testing"
)

func TestAllTemplatesRender(t *testing.T) {
	v := Vars{Worktree: "/w", Branch: "b", Base: "origin/main", Artifacts: "/a", Ticket: "ENG-1",
		Slice: "pr2", SliceID: "pr2", SliceTitle: "Service", ParentID: "pr1", PRNumber: 7, PRURL: "u", OldBase: "abc", Extra: "X"}
	for _, k := range append(Kinds, "context") {
		out, err := Render(k, v)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		if strings.Contains(out, "{{") || strings.Contains(out, "<no value>") {
			t.Fatalf("%s left a placeholder:\n%s", k, out)
		}
	}
	out, _ := Render("slice", v)
	if !strings.Contains(out, "handoff-pr1.md") || !strings.Contains(out, "handoff-pr2.md") {
		t.Fatal(out)
	}
	root, _ := Render("slice", Vars{SliceID: "pr1", SliceTitle: "S", Artifacts: "/a"})
	if strings.Contains(root, "stacked on") {
		t.Fatal("root slice should not mention a parent")
	}
	ctx := Context(Vars{Worktree: "/w", Artifacts: "/a"})
	if strings.Contains(ctx, "Linear") || strings.Contains(ctx, "branch") && strings.Contains(ctx, "stacked on") {
		t.Fatal(ctx)
	}
}
