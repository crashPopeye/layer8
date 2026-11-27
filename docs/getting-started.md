---
title: Getting started
---
# Getting started

This walkthrough takes one small change from an empty window to an open pull request: add a project, create a task, run the `quick-fix` workflow step by step, review the diff, and open the PR. It uses a repository called `billing-api` and a task called `fix-invoice-rounding`; use any git repository of yours. Steps 1 to 5 need no GitHub login.

You need layer8 [installed](install.md) and `claude` (or `codex`) working in a new Terminal window.

## 1. Add a project

In the **Tasks** panel, click **Add project** (the `+` at the top) and pick the folder of a git repository, for example `~/code/billing-api`.

**You should see** `billing-api` in the Tasks panel. The repository is untouched: layer8 only records its path.

From the terminal: `layer8 do app add-project path=$HOME/code/billing-api`.

## 2. Create a task

Hover `billing-api` and click **New task** (or press ⌘N). Name it `Fix invoice rounding`.

**You should see** the task `fix-invoice-rounding` under the project. Behind it layer8 created:

| What | Where |
|---|---|
| a branch | `fix-invoice-rounding`, from the project's default branch |
| a worktree | `~/.layer8/worktrees/billing-api/fix-invoice-rounding/` |
| an artifacts folder | `~/.layer8/tasks/fix-invoice-rounding/` |

Your own checkout of `billing-api` stays on whatever branch it was on.

## 3. Run a workflow

Select the task. In the task header, leave **Autopilot** (the ▶ toggle) off, open the **Workflow…** menu and pick `quick-fix`. When asked what to do, the agent reads the task title; to give it more, start a session yourself with ⌘T and describe the change, or write a `01-ticket.md` into the artifacts first.

**You should see** a Claude session open in the **Session** panel with the `implement` skill. It reads the code in the worktree, makes the change, runs the tests and commits. The header shows `quick-fix · implement`.

If the session stops on a tool call, it's waiting for you: click **Allow** or **Deny** in the Session panel. See [approvals](concepts.md#approvals).

## 4. Review the diff and comment

Open the **Changes** panel. It shows the task's diff against its base branch.

**You should see** the files the agent changed, for example 2 files, +18 −4.

Click a line to comment on it, for example *"round half-even, not half-up"*. Then click **Send 1 comment to implement**.

**You should see** the comment arrive in the session as a message. The agent revises, commits again, and marks the comment resolved with a one-line reply. You can comment on artifacts in the **Artifacts** panel the same way.

## 5. Go to the next step

When the `implement` step finishes, the header shows **Next: describe-pr ▸**. Don't click it yet if you haven't logged in to GitHub: the next step opens the pull request.

You can also commit and push by hand from the Changes panel (**Commit**, **Push**). Push uses your own git credentials, not layer8's.

## 6. Open the pull request

Log in to GitHub once:

```sh
gh auth login
```

Then click **Next: describe-pr ▸**.

**You should see** a new session write `pr-description.md` into the artifacts, push the branch and open the pull request, for example PR #42. The **PR** panel shows its state, checks and review threads.

If the checks fail or reviewers leave comments, run the `pr-care` workflow on the same task: it fixes CI, then answers the review comments.

## Where next

| To | Read |
|---|---|
| understand tasks, sub-tasks, sessions and artifacts | [Concepts](concepts.md) |
| run bigger work (research → plan → stacked PRs) | [Workflows & skills](workflows.md) |
| rearrange the window, theme it, add panels | [Panels & customisation](panels.md) |
| script all of this | [CLI](cli.md) |

[← Install](install.md) · [Concepts →](concepts.md)
