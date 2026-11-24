---
name: iterate-plan
description: Only use when the user explicitly invokes this skill by name.
---

# Iterate Plan

> **Autonomous tasks.** When the `<layer8_session>` block in your system prompt says `Autonomous: yes`, never stop to ask the user. Wherever this skill says to ask a question, wait for an answer or wait for approval, answer it yourself with your recommended option, record the choice and why in the artifact, and keep going until the skill's final answer.

You are iterating on an existing implementation plan based on user feedback.

## Steps

1. **Read all input files FULLY**:
   - Use Read tool WITHOUT limit/offset to read the plan document and any other provided paths
   - `ls -La .layer8/task/` to find all related documents in the task directory. Do not use glob or grep, or use `ls -l` or `ls` without `-L` as the directory may be a symlink.
   - Read everything in the task directory to build full context, including the plan and prior artifacts (`ticket.md` or `task.md`, research, design discussion, PRD/TDD, and structure outline if present)

2. **If a ticket file is provided, read it for feedback**:
   - These comments contain instructions/feedback from the user

3. **If the user gives any input**:
   - DO NOT just accept the correction blindly
   - Read the specific files/directories they mention
   - Verify code examples and file paths are accurate
   - Only proceed once you've verified the facts yourself


4. **Process the feedback**:
   - If user requested phase changes: Reorganize or modify phases as requested
   - If user requested code changes: Update the specific code examples
   - If user found errors: Fix inaccuracies in file paths, code, or descriptions
   - Keep the same YAML frontmatter and format

5. **Update document** (if changes needed):
   - Update the document at the same path
   - Ensure code examples are accurate and complete
   - Verify success criteria are actionable
   - Maintain the phase structure with automated/manual verification

6. **Read the final output template** (every layer8 task already runs in its own worktree on its own branch):

`Read({SKILLBASE}/references/plan_final_answer.md)`

7. Respond following the template exactly. Do not include a summary or other information.

## Plan Writing Guidelines

- Each phase should be independently testable
- Include specific code examples, not just descriptions
- Automated verification should be runnable commands
- Manual verification should be specific, actionable steps
- Pause for human confirmation between phases

## Document Precedence

When documents conflict, the most recent document wins:
**plan > structure outline > TDD > design discussion > research > ticket**

The plan is the final authority. Follow the structure outline and design decisions over
the original ticket when they differ.

<guidance>

## Markdown Formatting

When writing markdown files that contain code blocks showing other markdown (like README examples or SKILL.md templates), use 4 backticks (````) for the outer fence so inner 3-backtick code blocks don't prematurely close it:

````markdown
# Example README
## Installation
```bash
npm install example
```
````

## Validation Design

Not every phase requires manual validation, don't put steps for manual validation just to have them.
</guidance>
