---
name: research-questions
description: Turn a task into the factual questions a researcher must answer about the codebase before anyone designs a solution. Writes NN-research-questions-<slug>.md in the task's artifacts folder.
argument-hint: "[what the task is about, if not in the artifacts]"
---

# Research questions

Goal: a short list of questions whose answers describe **how the code works today**, in the areas the task touches. The questions must not leak a solution, so the research that answers them stays objective.

## 1. Orient

Read `layer8://here` (MCP server `layer8`). Note `artifacts` (also `$LAYER8_ARTIFACTS`) and `worktree`. Read every file already in the artifacts folder, starting with the ticket (`01-ticket.md` when there is one). Add what the user typed after the command.

## 2. Skim, don't research

Spend a few minutes locating the areas involved: entry points, packages, data models, config, tests. Just enough to ask precise questions, with real file and symbol names.

## 3. Write the questions

Write `NN-research-questions-<slug>.md` (see the `layer8` skill for numbering):

- A one-paragraph **context** restating the task in neutral terms.
- 5 to 15 **questions**, grouped by area. Each one asks how something works, where something lives, or what calls what. Good: "How does `UploadHandler` decide the max body size, and where is it configured?" Bad: "Should we add a rate limiter to `UploadHandler`?"
- **Out of scope**: areas you looked at and decided don't matter, with one line on why.

## 4. Finish

Tell the user the file name and the 3 questions you think matter most. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
