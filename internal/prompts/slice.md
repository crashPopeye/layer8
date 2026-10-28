Implement pull request `{{.SliceID}}` ("{{.SliceTitle}}") from `{{.Artifacts}}/plan.md`.

1. Read the plan section for `{{.SliceID}}`, `{{.Artifacts}}/research.md`{{if .ParentID}}, and `{{.Artifacts}}/handoff-{{.ParentID}}.md`, written by the session that implemented `{{.ParentID}}`, which this branch is stacked on{{end}}.
2. Implement that section only. Other PRs of the plan are handled by other sessions on other branches. Follow the patterns described in the research.
3. Run the checks the plan lists for this PR and fix what fails.
4. Commit in small logical commits with clear messages. Do not push.
5. Finish by writing `{{.Artifacts}}/handoff-{{.SliceID}}.md`: what changed, decisions made, and anything the sessions for later PRs must know.

{{.Extra}}
