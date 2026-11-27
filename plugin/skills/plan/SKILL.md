---
name: plan
description: Turn the research and design into an implementation plan split into reviewable pull requests, and create one stacked layer8 sub-task per PR when there are several. Writes NN-plan-<slug>.md.
argument-hint: "[constraints on the split]"
---

# Plan

## 1. Orient

Read `layer8://here` (MCP server `layer8`): `slug`, `branch`, `artifacts` (also `$LAYER8_ARTIFACTS`) and its sub-tasks in `children`. Read every artifact: ticket, research, PRD, design. Verify against the code anything the plan depends on.

## 2. Split into pull requests

Each PR should be reviewable on its own: about 1,500 changed lines or less, passing tests, and leaving `main` working if merged alone. Order them so each builds on the previous one (store → service → API → UI → docs, typically). One PR is fine for small work; don't split for the sake of it.

## 3. Write `NN-plan-<slug>.md`

- **Overview**: the approach in 5 lines, and a table `PR | sub-task name | scope | depends on`.
- **One section per PR**, headed `## PR <n>: <sub-task name>`, with:
  - the files to change or add and what changes in each;
  - the contracts it introduces (signatures, schemas) as code blocks;
  - the tests to write, and the commands that verify it (`go test ./…`, `npm test`…);
  - **Done when**: observable checks.
- **Not doing**: what is deliberately left out.

Sub-task names are short kebab-case, like `pr1-store`, `pr2-middleware`. The implement skill finds its section by that name.

## 4. Create the sub-tasks

Only when the plan has two PRs or more, and only for those not already among the children of `here`. Stack each on the previous one with `base`, in one call:

```json
{"ops":[
  {"target":"here","action":"create-subtask","args":{"name":"pr1-store"}},
  {"target":"here","action":"create-subtask","args":{"name":"pr2-middleware","base":"pr1-store"}},
  {"target":"here","action":"create-subtask","args":{"name":"pr3-docs","base":"pr2-middleware"}}
]}
```

Independent PRs (docs that don't need the code, for example) omit `base`, so they stack on the task's own branch and can be implemented in parallel. Check every result line; fix and retry failed ops.

## 5. Finish

Show the user the PR table. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
