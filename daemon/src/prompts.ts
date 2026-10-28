import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { SessionKind, Task } from "./types.js";

// prompts/ sits next to src/ and dist/.
const dir = join(dirname(fileURLToPath(import.meta.url)), "..", "prompts");

export function render(template: string, vars: Record<string, string>): string {
  return template.replace(/\{\{(\w+)\}\}/g, (_, k: string) => vars[k] ?? "").trim() + "\n";
}

function vars(task: Task, extra = ""): Record<string, string> {
  return {
    worktree: task.worktreePath,
    artifacts: task.artifactsPath,
    branch_line: task.branch ? ` on branch \`${task.branch}\` (based on \`${task.baseRef}\`)` : "",
    ticket_line: task.linear
      ? `- Linear ticket: ${task.linear.identifier} (${task.linear.url}), copied to \`${task.artifactsPath}/ticket.md\`.`
      : "",
    extra,
  };
}

const load = (name: string) => readFileSync(join(dir, `${name}.md`), "utf8");

/** Appended to Claude Code's own system prompt for every session of the task. */
export function taskContext(task: Task): string {
  return render(load("context"), vars(task));
}

/** First message of a workflow session. Chat sessions start from the user's text only. */
export function workflowPrompt(kind: SessionKind, task: Task, extra = ""): string {
  if (kind === "chat") return extra;
  return render(load(kind), vars(task, extra));
}
