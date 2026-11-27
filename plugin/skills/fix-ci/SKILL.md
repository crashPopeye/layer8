---
name: fix-ci
description: Make the current task's pull request checks pass - read the failing jobs, reproduce locally, fix, push, and repeat until green or truly stuck.
argument-hint: "[check to focus on]"
---

# Fix CI

## 1. Orient

Read `layer8://here` (MCP server `layer8`): `worktree`, `branch`, and `pr`, which holds the PR number, its checks and the log tail of failing jobs. If `refresh-pr` is listed, call it first so the checks are current:

```json
{"ops":[{"target":"here","action":"refresh-pr"}]}
```

Without a `pr` field, use `gh pr checks` and `gh run view <run-id> --log-failed` from the worktree. With no PR at all, tell the user and stop.

## 2. Fix, one round at a time

1. Pick the first failing check. Read its log tail; find the real error, not the last line.
2. Reproduce it locally with the same command CI runs (read the workflow file under `.github/workflows/`).
3. Fix the cause, not the symptom: never skip, delete or loosen a test to get green unless the test itself is wrong, and say so when it is.
4. Run the check locally, then the rest of the suite.
5. Commit, then push with the task's action, or `git push` when it isn't listed:

```json
{"ops":[{"target":"here","action":"push"}]}
```

6. Wait for CI (`gh pr checks --watch`), then start the next round with what is still red.

Stop after 3 rounds without progress, or when the failure is outside the branch (flaky infrastructure, secrets, a broken base branch), and explain what you found.

## 3. Finish

Tell the user which checks you fixed and how. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
