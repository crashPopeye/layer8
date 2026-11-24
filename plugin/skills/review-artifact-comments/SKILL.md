---
name: review-artifact-comments
description: Only use when the user explicitly invokes this skill by name.
---

# Review Artifact Comments

> **Autonomous tasks.** When the `<layer8_session>` block in your system prompt says `Autonomous: yes`, never stop to ask the user. Wherever this skill says to ask a question, wait for an answer or wait for approval, answer it yourself with your recommended option, record the choice and why in the artifact, and keep going until the skill's final answer.

You are helping the user address comments on an artifact file. Work through each comment systematically.

## Input Format

layer8 delivers comments as one message, sent from the Artifacts or Changes panel with *Send to agent*. It starts with `Please address these review comments:` and lists each comment as:

```
.layer8/task/03-prd-local-agent-desktop.md:42
> the quoted line(s) the comment is anchored to
the comment itself
```

A path under `.layer8/task/` is an artifact; any other path is a file of the worktree (a diff comment).

Generally:
1. If the comments are in the conversation, follow the procedure below for them.
2. If they are not, but an artifact is named or apparent from the conversation, tell the user to select the lines in the Artifacts panel, comment them and press *Send to agent*: layer8 has no command that fetches comments.

## Workflow

1. **Read each commented file** (artifacts are in `.layer8/task/`; list it with `ls -La`) _if you have not read it already_.
2. **Read the comments** to understand what feedback exists.
3. **Ask the user how to proceed**: if the user has not given you instructions (such as "apply all comments"), once you have read the files and the comments, ask the user how they would like you to proceed. You may offer contextually relevant, CONCISE recommendations - for example:

````
I can see 4 comments on this design discussion. Would you like me to...
1. update the design discussion based on the feedback of each comment
2. research and investigate the issues flagged in the comments and report my findings
3. something else?
````

4. Follow the user's instructions, then summarise per comment what you changed (`path:line` and one line each).

## Important Notes
- Unless the user gives you a course of action, you MUST ask the user how to proceed after reading the comments.
- Work through comments one at a time for clarity.
- Edit artifacts in `.layer8/task/` with Write/Edit; files under `.layer8/task/parent/` are read-only.
