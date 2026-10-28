Write an implementation plan for this task. Do not change any code.

1. Read `{{artifacts}}/ticket.md`, `{{artifacts}}/research.md` and any other artifact in `{{artifacts}}`. If research is missing or thin, investigate the code yourself first.
2. If a decision is genuinely the user's to make, ask before writing the plan.
3. Split the work into pull requests that can each be reviewed on their own: small enough to review in one sitting, each leaving main working. Prefer a stack where each PR builds on the previous one; mark PRs that don't depend on each other so they can run in parallel.
4. Write `{{artifacts}}/plan.md` with:
   - Goal and non-goals.
   - One section per PR, headed `## PR <n>: <title>` with: the changes file by file, the tests to add, and how to verify it (commands to run, manual checks).
   - At the very end, this machine-readable block, which layer8 uses to create one branch and one fresh session per PR:

```yaml
prs:
  - id: pr1
    title: "<short title>"
    depends_on: []
  - id: pr2
    title: "<short title>"
    depends_on: [pr1]
```

{{extra}}
