---
name: orchestrate
description: Drive the layer8 app from a session with the l8 CLI - create tasks and sub-tasks and start their sessions, list and close tasks, start, read, message and stop sessions, read and write another task's artifacts. Use when the user asks to split work into sub-tasks, start a task or a session, check on another session, hand work to a parallel agent, or reach a parent's or sub-task's artifacts.
---

# Drive layer8 with `l8`

You are running inside a layer8 session. The `<layer8_session>` block in your system prompt names your session, your task and, for a sub-task, its parent. `l8` talks to the local layer8 daemon; it is on your `PATH`.

`current` means this task (`--task current`, `--parent current`) or this session (`l8 session read current`). It resolves from `L8_TASK_ID` and `L8_SESSION_ID`, which layer8 sets for you. Every command accepts `--json` and then prints its result as JSON, which is the way to read ids in a script.

## Tasks and sub-tasks

A task is a git worktree on its own branch plus an artifacts folder, linked into the worktree at `.layer8/task/`. Every session of a task shares that folder.

A sub-task is a full task with a parent. Its branch starts from the head of the parent's branch, so it builds on the parent's committed work. It inherits the parent's project, its project instructions and its `autonomous` flag. The parent's artifacts folder is linked read-only at `.layer8/task/parent/`.

| Command | Effect |
|---|---|
| `l8 task create --title T [--parent <id\|current>] [--workflow freeform] [--prompt P] [--start] [--auto-advance] [--autonomous] [--project P] [--adopt DIR]` | New task, or sub-task with `--parent`, on branch `l8/<slug>`. `--prompt` goes to its `task.md`; `--start` also starts its first session (below) |
| `l8 task list [--project P] [--parent <id\|current>] [--status active\|done\|archived]` | Tasks, newest first: slug, status, workflow, branch, title. `--parent current` lists this task's sub-tasks |
| `l8 task show <id\|slug\|current>` | Id, branch, base, worktree, artifacts folder, parent, workflow phases and `task.md` |
| `l8 task done <id\|current>` / `l8 task archive <id\|current>` | Close a task. Its worktree and branch stay |

## Workflows and phases

A workflow is a chain of phases. Each phase runs in its own session, labelled with the phase name, whose first message is the phase's skill pointed at `.layer8/task/`. A phase is done when its artifact (a Markdown file whose frontmatter `type` is listed below) is in `.layer8/task/`, when the worktree has changes (`implement`), or when the task has a PR (`pr`).

| Workflow | Phases |
|---|---|
| `rpi` | research-questions → research → design-discussion → outline → plan → implement → pr |
| `prd_tdd` | research-questions → research → prd → tdd → outline → implement → pr |
| `outline_only` | research → outline → implement → pr |
| `oneshot` | implement (the task's prompt) → pr |
| `freeform` | none: `--start` sends the prompt to a session labelled `session` |

| Phase | Skill | Artifact `type` |
|---|---|---|
| research-questions | `/l8:create-research-questions` | `research-questions` |
| research | `/l8:create-research` | `research` |
| design-discussion | `/l8:create-design-discussion` | `design-discussion` |
| prd | `/l8:create-prd` | `design-prd` |
| tdd | `/l8:create-tdd` | `design-tdd` |
| outline | `/l8:create-structure-outline` | `structure-outline` |
| plan | `/l8:create-plan` | `plan` |
| implement | `/l8:implement-outline` (`rpi`: `/l8:implement-plan`) | none |
| pr | `/l8:describe-pr` | none |

When a phase is done and its session goes idle, the app offers the next phase on the task's phase rail; with `--auto-advance` it starts it on its own.

| Command | Effect |
|---|---|
| `l8 phase next [--task <id\|current>]` | Start the next phase's session now, whether or not the current phase is done |

## Sessions

A session is one Claude Code conversation running in its task's worktree.

| Command | Effect |
|---|---|
| `l8 session start --task <id\|current> --label L --prompt "…" [--model M] [--effort E] [--permission-mode default\|acceptEdits\|auto\|bypassPermissions]` | Start a session and send its first message |
| `l8 session list --task <id\|current>` | Sessions of a task: id, label, status, cost |
| `l8 session read <id\|current> [--last 20]` | The last messages of a session as plain text |
| `l8 session send <id\|current> "…"` | Message a session. Queued if it is mid-turn |
| `l8 session stop <id\|current>` | Close a session's process. A later message resumes it |

Statuses: `starting`, `running`, `waiting_approval` (a tool call needs the user), `ready_for_input`, `stopped`, `failed`.

## Artifacts

This task's artifacts folder is `.layer8/task/`; read and write it directly with Read, Write and Edit. A sub-task reads its parent's files at `.layer8/task/parent/` but cannot write there: writes are denied. Use `l8 artifact` to reach another task's folder, for example to report back to the parent.

| Command | Effect |
|---|---|
| `l8 artifact list [--task <id\|current\|parent>]` | Files of a task's artifacts folder: path, type, modified |
| `l8 artifact read [--task …] <path>` | Print an artifact |
| `l8 artifact write [--task …] <path> < file` | Create or replace an artifact from stdin |

`--task` defaults to `current`; `parent` is this task's parent task. The app's Artifacts panel lists every file, newest first, labelled by its frontmatter `type`, and refreshes as you write.

To show a file inline in a Markdown artifact or in your reply, use a `task-artifact` fence with its path in the artifacts folder. HTML renders in a sandboxed iframe, images render as images:

````markdown
```task-artifact
mockup-workspace.html
```
````

## Recipe: split the work into sub-tasks

Use this when the work has slices that can be built and reviewed on their own, for example one per package or per PR.

1. Write the shared context first, in this task's artifacts (`.layer8/task/`): the PRD, TDD or outline the slices come from. Commit any code the slices must build on, because a sub-task branches from this branch's committed head.
2. Create one sub-task per slice and start it. Give each a prompt that stands on its own and points at the shared artifacts under `parent/`:

   ```bash
   l8 task create --parent current --start --workflow oneshot \
     --title "Session runtime (Go)" \
     --prompt "Implement phase 2 of .layer8/task/parent/05-structure-outline.md: the claude and session packages. Run make check before you finish."
   l8 task create --parent current --start --workflow oneshot \
     --title "Artifacts panel" \
     --prompt "Implement phase 4 of .layer8/task/parent/05-structure-outline.md. Run make check before you finish."
   ```

   Each prints the new task and its running session. With `--json`, read `.id` and `.session.id`.
3. Record the plan in this task: write `.layer8/task/subtasks.md` listing each sub-task's slug, branch and what it owns.
4. Check on them later, not in a loop:

   ```bash
   l8 task list --parent current
   l8 session list --task session-runtime-go
   l8 session read <session-id> --last 5
   ```

5. A sub-task reports back by writing into its parent's folder: `l8 artifact write --task parent session-runtime-report.md < report.md`.
6. When a sub-task is merged or abandoned, close it with `l8 task done <slug>`.

The task's project instructions (for this project: "Use Golang as much as possible wherever it is possible and relevant.") reach every sub-task's `task.md` on their own; you don't need to repeat them in the prompt.

## Rules

- Don't poll another session in a tight loop; check back with `l8 session read` when you need its result.
- Write shared notes and results to `.layer8/task/` so every session of the task can read them.
- Never write under `.layer8/task/parent/`; use `l8 artifact write --task parent <path>` instead.
- Keep sub-task prompts self-contained: the sub-task's session does not see this conversation, only its own `task.md`, the parent's artifacts and the repository.
