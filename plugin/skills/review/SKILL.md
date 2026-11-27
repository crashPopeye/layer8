---
name: review
description: Review the current task's changes against its base branch and the plan - correctness first, then scope, tests and simplicity - and write the findings to NN-review-<slug>.md.
argument-hint: "[what to focus on]"
---

# Review

## 1. Orient

Read `layer8://here` (MCP server `layer8`): `worktree`, `branch`, `base`, `artifacts` (also `$LAYER8_ARTIFACTS`). Read the plan section for this task (see the `implement` skill for how to find it) and get the change with `git diff <base>...HEAD` plus uncommitted work (`git status`).

## 2. Review

In this order, and report only what you can defend with a file and line:

1. **Correctness**: logic errors, unhandled errors, races, data loss, security issues at trust boundaries. Run the tests; read the code paths they skip.
2. **Plan**: anything planned that is missing, anything built that wasn't planned.
3. **Tests**: does each behaviour change have a test that would fail without it?
4. **Simplicity**: code that could be deleted or replaced by something the codebase already has.

## 3. Write `NN-review-<slug>.md`

A table `severity (blocker / should / nit) | file:line | finding | suggested fix`, then a 3-line verdict. Don't edit the code; the user decides what to fix.

## 4. Finish

Give the user the verdict and the blockers. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
