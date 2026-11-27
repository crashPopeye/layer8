---
name: research
description: Document how the code relevant to a task works today, with file:line references, without proposing changes. Answers the research questions artifact when there is one. Writes NN-research-<slug>.md.
argument-hint: "[focus, if not in the artifacts]"
---

# Research

You are a cartographer, not an architect: describe what exists, where, and how it connects. No recommendations, no critique, no plan.

## 1. Orient

Read `layer8://here` (MCP server `layer8`): `artifacts` (also `$LAYER8_ARTIFACTS`), `worktree`, `branch`. Read the artifacts folder. If a `*-research-questions-*.md` exists, its questions are your scope. Otherwise derive the scope from the ticket and what the user typed.

## 2. Investigate

- Work from the worktree. Find the entry points, follow the calls, read the tests to learn the intended behaviour.
- For broad sweeps, run parallel sub-agents, each with one area and a clear question; read the key files yourself afterwards.
- Check git history (`git log -S`, `git blame`) when the "why" of a structure matters.
- Every claim gets a `path/to/file.go:123` reference. If you could not confirm something, say so.

## 3. Write `NN-research-<slug>.md`

1. **Summary**: 5 lines a newcomer could act on.
2. **Answers**: one section per question (or per area), with references.
3. **How it fits together**: a Mermaid diagram of the main flow, and a table of the key types or tables with their fields.
4. **Conventions worth copying**: patterns the codebase already uses for similar features (error handling, tests, naming).
5. **Open questions**: what the code could not tell you.

## 4. Finish

Give the user the file name and the summary. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
