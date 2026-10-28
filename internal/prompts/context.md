# layer8 task context

You are working on one task inside layer8, a workbench that runs several agent sessions in parallel. This session's files:

- Working tree: `{{.Worktree}}`{{if .Branch}} on branch `{{.Branch}}` (stacked on `{{.Base}}`){{end}}
- Task artifacts: `{{.Artifacts}}`. Research, plans and handoffs live here, not in the repository. Read what is already there before you start, and write your outputs here.
{{- if .Ticket}}
- Linear ticket: {{.Ticket}}, copied to `{{.Artifacts}}/ticket.md`.
{{- end}}
{{- if .Slice}}
- This session works on one pull request of the plan: `{{.Slice}}`. Stay inside its scope; other PRs are handled by other sessions.
{{- end}}

Every session starts with a fresh context. Anything the next session needs must be written to an artifact, not left in this conversation.

layer8 pushes branches and opens pull requests itself. Commit your work, but do not push, rebase, or switch branches unless the user asks.
