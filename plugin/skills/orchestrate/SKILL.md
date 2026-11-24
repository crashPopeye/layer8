---
name: orchestrate
description: Drive the layer8 app from a session with the l8 CLI - create and list tasks, start, read, message and stop sessions. Use when the user asks to start a task or a session, check on another session, or hand work to a parallel agent.
---

# Drive layer8 with `l8`

You are running inside a layer8 session. The `<layer8_session>` block in your system prompt names your session and task. `l8` talks to the local layer8 daemon; it is on your `PATH`.

`current` means this task (`--task current`) or this session (`l8 session read current`). It resolves from `L8_TASK_ID` and `L8_SESSION_ID`, which layer8 sets for you.

## Tasks

A task is a git worktree on its own branch plus an artifacts folder, linked into the worktree at `.layer8/task/`. Every session of a task shares that folder.

| Command | Effect |
|---|---|
| `l8 task create --title T [--workflow freeform] [--prompt P] [--autonomous]` | New task with its own worktree and branch `l8/<slug>`. `--prompt` is written to its `task.md` |
| `l8 task list [--project P] [--status active]` | Tasks, newest first: slug, status, workflow, branch, title |
| `l8 task show <id\|slug\|current>` | Branch, worktree, artifacts folder and `task.md` |
| `l8 task done <id>` / `l8 task archive <id>` | Close a task. Its worktree stays |

Workflows: `freeform`, `oneshot`, `outline_only`, `prd_tdd`, `rpi`.

## Sessions

A session is one Claude Code conversation running in its task's worktree.

| Command | Effect |
|---|---|
| `l8 session start --task <id\|current> --label L --prompt "…" [--model M] [--permission-mode default]` | Start a session and send its first message |
| `l8 session list --task <id\|current>` | Sessions of a task: id, label, status, cost |
| `l8 session read <id\|current> [--last 20]` | The last messages of a session as plain text |
| `l8 session send <id> "…"` | Message a session. Queued if it is mid-turn |
| `l8 session stop <id>` | Close a session's process |

Statuses: `starting`, `running`, `waiting_approval` (a tool call needs the user), `ready_for_input`, `stopped`, `failed`.

## Example

Hand a side investigation to a parallel session in this task, then read its answer:

```bash
l8 session start --task current --label research --prompt "List every caller of store.Open and write the list to .layer8/task/callers.md"
l8 session list --task current
l8 session read <id> --last 5
```

## Rules

- Don't poll another session in a tight loop; check back with `l8 session read` when you need its result.
- Write shared notes and results to `.layer8/task/` so every session of the task can read them.
