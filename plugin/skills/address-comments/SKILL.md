---
name: address-comments
description: Work through the unresolved review comments on the current task's pull request - change the code where the reviewer is right, answer where they aren't - then push and reply.
argument-hint: "[reviewer or thread to focus on]"
---

# Address PR comments

## 1. Orient

Read `layer8://here` (MCP server `layer8`): `worktree`, `branch` and `pr` (number, URL, review threads). If `refresh-pr` is listed, call it first so the threads are current:

```json
{"ops":[{"target":"here","action":"refresh-pr"}]}
```

Without thread data in `pr`, list the unresolved threads with `gh api graphql` (`pullRequest.reviewThreads` where `isResolved` is false) or `gh pr view --comments`.

## 2. Triage

Make a table for yourself: `thread | file:line | ask | verdict (change / answer / ask the user)`. Group threads that ask for the same thing. Anything that changes product behaviour or contradicts the plan goes to the user before you touch the code.

## 3. Change

Make the agreed changes in small commits, each mentioning what it addresses. Run the tests. Push with the task's action, or `git push` when it isn't listed:

```json
{"ops":[{"target":"here","action":"push"}]}
```

## 4. Reply

Reply on each thread with `gh`: what changed (with the commit), or why not. Resolve only the threads you fully addressed; leave disagreements open for the reviewer.

## 5. Finish

Give the user the triage table. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
