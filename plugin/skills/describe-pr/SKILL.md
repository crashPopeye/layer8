---
name: describe-pr
description: Write the pull request description for the current task or sub-task from its diff and artifacts, then open the PR through layer8 (stacked on the parent's branch for sub-tasks).
argument-hint: "[notes for reviewers]"
---

# Describe and open the PR

## 1. Orient

Read `layer8://here` (MCP server `layer8`): `title`, `branch`, `base`, `artifacts` (also `$LAYER8_ARTIFACTS`), `pr`. If `pr` already shows an open pull request, update its description with `gh pr edit` instead of opening another.

Collect `git log <base>..HEAD`, `git diff <base>...HEAD --stat` and the parts of the diff that matter. Read the plan section for this task and any deviations noted by the implement session.

## 2. Write the description

File: `pr-description.md` in the artifacts folder for a root task, `pr-description-<sub-task name>.md` for a sub-task (they share the folder). Content:

- **Title** on the first line as `# <title>`: imperative, under 70 characters.
- **What and why**: 2-4 sentences.
- **Changes**: grouped bullets, the reviewer's map of the diff.
- **Decisions**: choices a reviewer might question, with the reason.
- **How to verify**: commands and manual steps.
- **Stack** (sub-tasks): which PR this builds on and what comes next.

No AI attribution lines.

## 3. Open the PR

Commit anything left uncommitted that belongs to the change. Then pass the title and the body (the file without its title line) explicitly:

```json
{"ops":[{"target":"here","action":"open-pr","args":{"title":"Rate-limit uploads per API key","body":"## What and why\n…"}}]}
```

`open-pr` pushes the branch and targets `base`, so stacked PRs land on their parent. If `open-pr` is not listed on `here`, fall back to `git push -u origin HEAD` and `gh pr create --base <base> --title … --body-file …`.

## 4. Finish

Give the user the PR link. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
