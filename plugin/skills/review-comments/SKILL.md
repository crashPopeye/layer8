---
name: review-comments
description: Address the comments the user left in layer8 on artifacts and diff lines for the current task - revise the artifact or code each one points at, then mark it resolved.
argument-hint: "[artifact to focus on]"
---

# Address layer8 comments

The user comments on artifact passages and diff lines inside layer8 and sends them to this session. Each comment points at a file, a quoted passage or line range, and says what to change.

## 1. Find the comments to address

Read `layer8://here` (MCP server `layer8`). Its `fields.comments` lists every comment on this task's diff and on its tree's artifacts:

```json
{"addr": "comment:3f9a1c2e", "target": "artifact", "artifact": "artifact:eng-482/05-plan.md", "path": "05-plan.md",
 "line": 12, "lineEnd": 14, "quote": "token bucket per tenant", "body": "Why not a sliding window?",
 "status": "sent", "author": "user"}
{"addr": "comment:b71e0d44", "target": "diff", "task": "task:eng-482/pr2", "path": "internal/http/ratelimit.go",
 "line": 42, "lineEnd": 42, "side": "new", "body": "Return 429, not 503", "status": "open"}
```

Address the comments whose `status` is `open` or `sent`; `resolved` ones are done. When the user named an artifact, keep to the comments on it.

- `target: artifact`: the file is `path` in the artifacts folder (`fields.artifacts` on `here`). Lines count from 1 in the source file.
- `target: diff`: the file is `path` in the worktree. `side: new` numbers lines in the current file, `side: old` in the base version (a deleted line).
- `quote` is the selected text; trust it over the line numbers when the file changed since.

Each `addr` is a node too: read `layer8://comment/<id8>` for its current state and the actions it offers. An artifact node also lists its comments as children of kind `comment`.

If `here` has no comments, use the comments pasted in the conversation; if there are none either, tell the user that there is nothing to address and stop.

## 2. Address each one

- Re-read the target passage in context before editing.
- Change the artifact or code as asked. When a comment is a question, answer it in the reply instead of editing.
- When comments conflict, follow the later one and say so.
- Keep the rest of the document consistent with each change (summaries, tables, diagrams).

## 3. Resolve

Resolve each addressed comment with a one-line reply saying what changed or answering the question:

```json
{"ops":[{"target":"comment:3f9a1c2e","action":"resolve","args":{"reply":"Switched to a sliding window and updated the diagram"}}]}
```

Leave comments you disagree with unresolved, and say why in your answer to the user.

## 4. Finish

List for the user what changed per comment. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
