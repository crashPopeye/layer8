package plan

import (
	"strings"
	"testing"
)

const doc = "# Plan\n\n```yaml\nnot: this\n```\n\n## PR 1\n\n```yaml\nprs:\n  - id: pr2\n    title: \"Service\"\n    depends_on: [pr1]\n  - id: pr1\n    title: Schema\n    depends_on: []\n  - id: pr3\n    title: UI\n    depends_on: [pr1, pr2]\n```\n"

func TestParseOrdersByDependency(t *testing.T) {
	prs, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range prs {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != "pr1,pr2,pr3" {
		t.Fatalf("order %v", ids)
	}
	if prs[2].Parent() != "pr2" || prs[0].Parent() != "" {
		t.Fatalf("parents %q %q", prs[2].Parent(), prs[0].Parent())
	}
}

func TestParseErrors(t *testing.T) {
	cases := map[string]string{
		"no block":   "# nothing",
		"unknown":    "```yaml\nprs:\n  - id: a\n    title: A\n    depends_on: [b]\n```",
		"cycle":      "```yaml\nprs:\n  - id: a\n    title: A\n    depends_on: [b]\n  - id: b\n    title: B\n    depends_on: [a]\n```",
		"duplicate":  "```yaml\nprs:\n  - id: a\n    title: A\n  - id: a\n    title: B\n```",
		"bad id":     "```yaml\nprs:\n  - id: \"a b\"\n    title: A\n```",
		"no title":   "```yaml\nprs:\n  - id: a\n```",
		"empty list": "```yaml\nprs: []\n```",
	}
	for name, md := range cases {
		if _, err := Parse(md); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
