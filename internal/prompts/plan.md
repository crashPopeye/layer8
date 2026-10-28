Write an implementation plan for this task. Do not change any code.

1. Read `{{.Artifacts}}/ticket.md`, `{{.Artifacts}}/research.md` and any other artifact in `{{.Artifacts}}`. If research is missing or thin, investigate the code yourself first.
2. If a decision is genuinely the user's to make, ask before writing the plan.
3. Split the work into pull requests that can each be reviewed on their own: small enough to review in one sitting, each leaving the default branch working. Prefer a stack, where each PR builds on the one before it. PRs that don't depend on each other can run in parallel.
4. Write `{{.Artifacts}}/plan.md` with:
   - Goal and non-goals.
   - One section per PR, headed `## <id>: <title>` (for example `## pr1: Upload session table`), with the changes file by file, the tests to add, and how to verify it (commands to run, manual checks).
   - At the very end, this machine-readable block. layer8 uses it to create one stacked branch and one fresh session per PR. Each PR is stacked on the last entry of its `depends_on`; ids use letters, digits, `-` and `_` only.

```yaml
prs:
  - id: pr1
    title: "<short title>"
    depends_on: []
  - id: pr2
    title: "<short title>"
    depends_on: [pr1]
```

{{.Extra}}
