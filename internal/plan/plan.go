// Package plan reads the machine-readable PR list at the end of a plan.md.
package plan

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

type PR struct {
	ID        string   `yaml:"id" json:"id"`
	Title     string   `yaml:"title" json:"title"`
	DependsOn []string `yaml:"depends_on" json:"dependsOn"`
}

// Parent is the PR this one is stacked on: its last dependency, or "" for main.
// A branch can only have one base, so further dependencies are ordering constraints only.
func (p PR) Parent() string {
	if len(p.DependsOn) == 0 {
		return ""
	}
	return p.DependsOn[len(p.DependsOn)-1]
}

var fence = regexp.MustCompile("(?s)```ya?ml[^\\n]*\\n(.*?)\\n```")

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$`)

// Parse extracts the last ```yaml block with a top-level `prs:` key and returns the
// PRs in dependency order (parents before children).
func Parse(markdown string) ([]PR, error) {
	var doc struct {
		PRs []PR `yaml:"prs"`
	}
	found := false
	for _, m := range fence.FindAllStringSubmatch(markdown, -1) {
		var d struct {
			PRs []PR `yaml:"prs"`
		}
		if err := yaml.Unmarshal([]byte(m[1]), &d); err != nil || d.PRs == nil {
			continue
		}
		doc, found = d, true
	}
	if !found {
		return nil, errors.New("plan.md has no ```yaml block with a `prs:` list")
	}
	if len(doc.PRs) == 0 {
		return nil, errors.New("the `prs:` list is empty")
	}
	byID := map[string]PR{}
	for _, p := range doc.PRs {
		p.Title = strings.TrimSpace(p.Title)
		if !validID.MatchString(p.ID) {
			return nil, fmt.Errorf("invalid PR id %q (letters, digits, - and _ only)", p.ID)
		}
		if p.Title == "" {
			return nil, fmt.Errorf("PR %s has no title", p.ID)
		}
		if _, dup := byID[p.ID]; dup {
			return nil, fmt.Errorf("duplicate PR id %q", p.ID)
		}
		byID[p.ID] = p
	}
	for _, p := range doc.PRs {
		for _, d := range p.DependsOn {
			if _, ok := byID[d]; !ok {
				return nil, fmt.Errorf("PR %s depends on unknown PR %q", p.ID, d)
			}
			if d == p.ID {
				return nil, fmt.Errorf("PR %s depends on itself", p.ID)
			}
		}
	}
	// Kahn's algorithm, keeping the plan's order among PRs that are ready together.
	var out []PR
	done := map[string]bool{}
	for len(out) < len(doc.PRs) {
		progressed := false
		for _, p := range doc.PRs {
			if done[p.ID] {
				continue
			}
			ready := true
			for _, d := range p.DependsOn {
				if !done[d] {
					ready = false
					break
				}
			}
			if ready {
				p.Title = byID[p.ID].Title
				out = append(out, p)
				done[p.ID] = true
				progressed = true
			}
		}
		if !progressed {
			return nil, errors.New("the PR dependencies contain a cycle")
		}
	}
	return out, nil
}
