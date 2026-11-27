---
name: prd
description: Write a product requirements document for the task from the ticket and the research - the problem, who has it, what success looks like, and the solution at the level of behaviour, not code. Writes NN-prd-<slug>.md.
argument-hint: "[product notes]"
---

# PRD

## 1. Orient

Read `layer8://here` (MCP server `layer8`) and every artifact in its `artifacts` folder (also `$LAYER8_ARTIFACTS`): ticket, research, earlier drafts. Add what the user typed.

## 2. Decide what you don't know

List the product decisions the material leaves open (who the user is, what's in scope, edge-case behaviour). Ask the user the ones only they can answer, at most 5 questions, each with your recommended answer. If the session runs unattended (a workflow on autopilot), take your recommendations and record them under **Decisions**.

## 3. Write `NN-prd-<slug>.md`

| Section | Content |
|---|---|
| Problem | who hits it, how often, what it costs them; evidence from the ticket or research |
| Success | 2-4 measurable outcomes |
| Solution | behaviour the user sees, as short bullets with a *why* each; a flow diagram when there is a sequence |
| Worked example | one concrete scenario with real-looking data, end to end |
| Out of scope | what we explicitly won't do |
| Decisions | each open question with the answer taken and who took it |

No code, no file names, no schema: that is the technical design's job.

## 4. Finish

Summarise the solution in 3 lines for the user. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
