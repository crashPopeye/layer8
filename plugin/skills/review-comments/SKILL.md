---
name: review-comments
description: Address the comments the user left in layer8 on artifacts and diff lines for the current task - revise the artifact or code each one points at, then mark it resolved.
argument-hint: "[artifact to focus on]"
---

# Address layer8 comments

The user comments on artifact selections and diff lines inside layer8 and sends them to this session. Each comment points at a file, a quoted passage or line, and says what to change.

## 1. Find the open comments

Read `layer8://here` (MCP server `layer8`) and look for comments: a `comments` field, or children of kind `comment`, on `here` and on the artifact nodes it lists. Read each comment node for its target, quote and text, and for the actions it offers.

If layer8 exposes no comments yet, use the comments pasted in the conversation; if there are none either, tell the user that there is nothing to address and stop.

## 2. Address each one

- Re-read the target passage in context before editing.
- Change the artifact or code as asked. When a comment is a question, answer it in your reply instead of editing.
- When comments conflict, follow the later one and say so.
- Keep the rest of the document consistent with each change (summaries, tables, diagrams).

## 3. Resolve

On each addressed comment, apply the resolve action its node lists, with a one-line reply saying what changed. Leave comments you disagree with open, with your reasoning.

## 4. Finish

List for the user what changed per comment. Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
