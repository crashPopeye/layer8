---
name: implement
description: Implement the plan for the current task or sub-task in its worktree, verify it with the project's tests, and commit. Reads the plan from the shared artifacts folder.
argument-hint: "[extra instructions]"
---

# Implement

## 1. Orient

Read `layer8://here` (MCP server `layer8`): `worktree` (you run in it), `branch`, `base`, `parent`, and `artifacts` (also `$LAYER8_ARTIFACTS`). Read the newest `*-plan-*.md` there, plus the design and research it relies on.

**Find your part of the plan.** The last segment of the `here` address is the sub-task name (`task:eng-482/pr2-middleware` → `pr2-middleware`); implement only the `## PR …: pr2-middleware` section. A root task with no sub-tasks implements the whole plan. With no plan, implement what the ticket and the user ask, and keep the change small.

## 2. Build

- Follow the codebase's conventions; read neighbouring code before writing.
- Work in small steps: change, run the narrowest relevant tests, continue.
- Write the tests the plan names. A fix starts with a failing test.
- When the plan is wrong or the code disagrees with it, choose the sensible fix, keep going, and note the deviation for the PR description.
- Stay inside your PR's scope. Work that belongs to a later sub-task stays there.

## 3. Verify

Run the full checks the plan lists (format, lint, tests, typecheck). Don't finish with failing checks; if one fails for reasons outside your scope, say so explicitly.

## 4. Commit

Commit on the task's branch with clear messages (what and why). Don't push and don't open the PR: that is `describe-pr`'s job.

## 5. Finish

Tell the user what was built, what was verified, and any deviation from the plan. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
