package workflow

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/crashPopeye/layer8/plugin"
)

func TestDone(t *testing.T) {
	prdPhase, _ := Find("prd_tdd", "prd")
	implement, _ := Find("prd_tdd", "implement")
	prPhase, _ := Find("prd_tdd", "pr")
	cases := []struct {
		name  string
		phase Phase
		e     Evidence
		want  bool
	}{
		{"artifact present", prdPhase, Evidence{ArtifactTypes: []string{"task", "research", "design-prd"}}, true},
		{"artifact missing", prdPhase, Evidence{ArtifactTypes: []string{"task", "research"}, Changed: true, HasPR: true}, false},
		{"non-empty diff", implement, Evidence{Changed: true}, true},
		{"empty diff", implement, Evidence{ArtifactTypes: []string{"structure-outline"}, HasPR: true}, false},
		{"has a PR", prPhase, Evidence{HasPR: true}, true},
		{"no PR", prPhase, Evidence{Changed: true}, false},
	}
	for _, c := range cases {
		if got := c.phase.Done(c.e); got != c.want {
			t.Errorf("%s: Done = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNext(t *testing.T) {
	cases := []struct {
		workflow string
		chain    []string
	}{
		{"rpi", []string{"research-questions", "research", "design-discussion", "outline", "plan", "implement", "pr"}},
		{"prd_tdd", []string{"research-questions", "research", "prd", "tdd", "outline", "implement", "pr"}},
		{"outline_only", []string{"research", "outline", "implement", "pr"}},
		{"oneshot", []string{"implement", "pr"}},
	}
	for _, c := range cases {
		var chain []string
		for p, ok := Next(c.workflow, ""); ok; p, ok = Next(c.workflow, p.Name) {
			chain = append(chain, p.Name)
		}
		if !slices.Equal(chain, c.chain) {
			t.Errorf("%s: chain = %v, want %v", c.workflow, chain, c.chain)
		}
	}
	if _, ok := Next("freeform", ""); ok {
		t.Error("freeform has a phase")
	}
	if _, ok := Next("prd_tdd", "nope"); ok {
		t.Error("an unknown phase has a next one")
	}
	if p, _ := Find("rpi", "implement"); p.Skill != "/l8:implement-plan" {
		t.Errorf("rpi implements with %q", p.Skill)
	}
	if p, _ := Find("oneshot", "implement"); p.Skill != "" {
		t.Errorf("oneshot implements with %q", p.Skill)
	}
}

func TestAfterSkill(t *testing.T) {
	cases := []struct{ workflow, skill, want string }{
		{"prd_tdd", "l8:create-research", "/l8:create-prd"},
		{"rpi", "l8:create-research", "/l8:create-design-discussion"},
		{"prd_tdd", "/l8:create-prd", "/l8:create-tdd"},
		{"prd_tdd", "l8:iterate-prd", "/l8:create-tdd"},
		{"prd_tdd", "l8:implement-outline", "/l8:describe-pr"},
		{"rpi", "l8:iterate-implementation", "/l8:describe-pr"},
		{"prd_tdd", "l8:show-me", ""},
		{"prd_tdd", "l8:describe-pr", ""},
		{"freeform", "l8:create-prd", ""},
	}
	for _, c := range cases {
		p, _ := AfterSkill(c.workflow, c.skill)
		if p.Skill != c.want {
			t.Errorf("AfterSkill(%s, %s) = %q, want %q", c.workflow, c.skill, p.Skill, c.want)
		}
	}
}

func TestEverySkillIsBundled(t *testing.T) {
	for workflow, phases := range Table {
		for _, p := range phases {
			if p.Skill == "" {
				continue
			}
			name := strings.TrimPrefix(p.Skill, "/l8:")
			if _, err := fs.Stat(plugin.FS, "skills/"+name+"/SKILL.md"); err != nil {
				t.Errorf("%s phase %s runs %s, which the l8 plugin lacks: %v", workflow, p.Name, p.Skill, err)
			}
		}
	}
}
