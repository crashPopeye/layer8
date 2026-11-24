// Package workflow is the static table of phase chains a task can follow, and
// the rules that say when a phase is done and which one comes next.
package workflow

import (
	"slices"
	"strings"
)

// Phase is one step of a workflow: the session that runs it is labelled Name,
// sends Skill as its first message, and writes an artifact of ArtifactType.
// A phase without an artifact type is done by its diff (implement) or its PR (pr).
type Phase struct {
	Name         string `json:"name"`
	Skill        string `json:"skill"`
	ArtifactType string `json:"artifactType"`
}

var (
	researchQuestions = Phase{"research-questions", "/l8:create-research-questions", "research-questions"}
	research          = Phase{"research", "/l8:create-research", "research"}
	designDiscussion  = Phase{"design-discussion", "/l8:create-design-discussion", "design-discussion"}
	prd               = Phase{"prd", "/l8:create-prd", "design-prd"}
	tdd               = Phase{"tdd", "/l8:create-tdd", "design-tdd"}
	outline           = Phase{"outline", "/l8:create-structure-outline", "structure-outline"}
	plan              = Phase{"plan", "/l8:create-plan", "plan"}
	implementOutline  = Phase{"implement", "/l8:implement-outline", ""}
	implementPlan     = Phase{"implement", "/l8:implement-plan", ""}
	implementPrompt   = Phase{"implement", "", ""}
	pr                = Phase{"pr", "/l8:describe-pr", ""}
)

// Table maps each workflow to its phases in order; freeform has no entry.
var Table = map[string][]Phase{
	"rpi":          {researchQuestions, research, designDiscussion, outline, plan, implementPlan, pr},
	"prd_tdd":      {researchQuestions, research, prd, tdd, outline, implementOutline, pr},
	"outline_only": {research, outline, implementOutline, pr},
	"oneshot":      {implementPrompt, pr},
}

// Evidence is what a task has produced so far.
type Evidence struct {
	ArtifactTypes []string
	Changed       bool
	HasPR         bool
}

// Done reports whether e shows that p's work exists.
func (p Phase) Done(e Evidence) bool {
	switch {
	case p.ArtifactType != "":
		return slices.Contains(e.ArtifactTypes, p.ArtifactType)
	case p.Name == "implement":
		return e.Changed
	case p.Name == "pr":
		return e.HasPR
	}
	return false
}

// First is the phase a workflow starts with.
func First(workflow string) (Phase, bool) {
	phases := Table[workflow]
	if len(phases) == 0 {
		return Phase{}, false
	}
	return phases[0], true
}

// Find returns the phase of workflow called name.
func Find(workflow, name string) (Phase, bool) {
	i := slices.IndexFunc(Table[workflow], func(p Phase) bool { return p.Name == name })
	if i < 0 {
		return Phase{}, false
	}
	return Table[workflow][i], true
}

// Next returns the phase after the one called name; an empty name means the first phase.
func Next(workflow, name string) (Phase, bool) {
	if name == "" {
		return First(workflow)
	}
	phases := Table[workflow]
	i := slices.IndexFunc(phases, func(p Phase) bool { return p.Name == name })
	if i < 0 || i+1 >= len(phases) {
		return Phase{}, false
	}
	return phases[i+1], true
}

// AfterSkill returns the phase that follows the one run by skill (a create-* or
// iterate-* skill, with or without the leading slash), so an agent can be told what comes next.
func AfterSkill(workflow, skill string) (Phase, bool) {
	name := strings.TrimPrefix(strings.TrimPrefix(skill, "/"), "l8:")
	if rest, ok := strings.CutPrefix(name, "iterate-"); ok {
		name = "create-" + rest
		if rest == "implementation" {
			name = "implement"
		}
	}
	for _, p := range Table[workflow] {
		own := strings.TrimPrefix(p.Skill, "/l8:")
		if own == name || (name == "implement" && p.Name == "implement") {
			return Next(workflow, p.Name)
		}
	}
	return Phase{}, false
}
