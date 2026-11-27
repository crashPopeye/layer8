---
name: layer8
description: How to read and drive the layer8 desktop app from a session. Use whenever you need to know which task you work in, find its artifacts folder, create sub-tasks, start sessions, open panels, or apply any other layer8 action.
---

# Driving layer8

layer8 is the user's desktop app for agent work. Everything in it is a **node** with an **address**, and you reach it through the `layer8` MCP server:

| To | Use |
|---|---|
| Read a node | the MCP resource `layer8://<kind>/<path>` on server `layer8`, e.g. `layer8://here`, `layer8://app`, `layer8://task/eng-482/pr1` |
| Act on nodes | the `do` tool of server `layer8`, with `{"ops":[{"target":…,"action":…,"args":{…}}]}` |

Addresses: `app`, `project:<name>`, `task:<root>/<child>`, `session:<id8>`, `artifact:<root>/<file>`, `workflow:<name>`, `ui:layout`, and `here`, which is the task or sub-task this session runs in.

## Read first, then act

1. Read `layer8://here`. Its `fields` give `branch`, `base`, `worktree`, `artifacts` (the shared artifacts folder, also in `$LAYER8_ARTIFACTS`), `parent`, and `workflow` when a workflow is running. Its `children` are sub-tasks, sessions and artifacts.
2. Its `actions` list **only what is valid right now**, each with a JSON Schema for `args`. Never guess an action: if it is not listed, it is not available.
3. Put several ops in one `do` call. Each op runs on its own and returns its own `ok`/`error` line, so check every result.

Example: plan two stacked sub-tasks and start a session in the first one.

```json
{"ops":[
  {"target":"here","action":"create-subtask","args":{"name":"pr1-store"}},
  {"target":"here","action":"create-subtask","args":{"name":"pr2-api","base":"pr1-store"}},
  {"target":"task:eng-482/pr1-store","action":"start-session","args":{"skill":"layer8:implement"}}
]}
```

## Artifacts

All tasks in a tree share one artifacts folder. Name new files `NN-<type>-<slug>.md`, where `NN` is one more than the highest two-digit prefix already in the folder (`01` when empty), `<type>` is what the file is (`research`, `plan`, `review`…), and `<slug>` is a few words in kebab case. Write them with your normal file tools.

## Workflows

When `here` has a `workflow` field, a workflow started this session for one of its steps. A workflow skill ends by telling the app its step is finished:

```json
{"ops":[{"target":"here","action":"advance"}]}
```

Skip that call when `advance` is not in the actions of `here`. Never answer approval prompts through `do`; only the user can.
