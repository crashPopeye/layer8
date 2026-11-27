import { describe, expect, it } from "vitest";
import { editDiff, isQueued, reduceAll, toolLabel, toolSummary, type SessionEvent } from "./model";

const ev = (type: SessionEvent["type"], data: Record<string, unknown>, id?: number): SessionEvent => ({ id, type, data });
const delta = (type: "text" | "thinking", text: string, parent?: string): SessionEvent => ({
  type,
  partial: true,
  data: { text, ...(parent && { parentToolUseId: parent }) },
});

describe("reduce", () => {
  it("streams deltas into one item and replaces it with the final event, keeping its key", () => {
    const streaming = reduceAll([ev("user", { text: "hi" }, 1), delta("thinking", "hm"), delta("text", "Hel"), delta("text", "lo")]);
    expect(streaming.items.map((i) => [i.kind, "text" in i && i.text, "streaming" in i && i.streaming])).toEqual([
      ["user", "hi", false],
      ["thinking", "hm", true],
      ["text", "Hello", true],
    ]);
    const done = reduceAll([ev("thinking", { text: "hmm." }, 2), ev("text", { text: "Hello!" }, 3)], streaming);
    expect(done.items.slice(1)).toEqual([
      { kind: "thinking", key: "p0", text: "hmm." },
      { kind: "text", key: "p1", text: "Hello!" },
    ]);
  });

  it("skips stored events it has already seen", () => {
    const t = reduceAll([ev("text", { text: "a" }, 5), ev("text", { text: "a" }, 5), ev("text", { text: "old" }, 4)]);
    expect(t.items).toHaveLength(1);
    expect(t.lastId).toBe(5);
  });

  it("pairs tool results with their call and nests subagent work under the tool that started it", () => {
    const t = reduceAll([
      ev("tool_use", { id: "t1", name: "Task", input: { description: "explore" } }, 1),
      ev("tool_use", { id: "t2", name: "Read", input: { file_path: "a.go" }, parentToolUseId: "t1" }, 2),
      delta("text", "found", "t1"),
      ev("tool_result", { toolUseId: "t2", content: [{ type: "text", text: "package a" }] }, 3),
      ev("text", { text: "found it", parentToolUseId: "t1" }, 4),
      ev("tool_result", { toolUseId: "t1", content: "done", isError: false }, 5),
    ]);
    expect(t.items).toEqual([
      {
        kind: "tool",
        key: "t1",
        name: "Task",
        input: { description: "explore" },
        result: { text: "done", isError: false },
        children: [
          { kind: "tool", key: "t2", name: "Read", input: { file_path: "a.go" }, result: { text: "package a", isError: false }, children: [] },
          { kind: "text", key: "p0", text: "found it" },
        ],
      },
    ]);
  });

  it("shows an approval as pending until its decision arrives", () => {
    const pending = reduceAll([ev("approval", { approval: 7, tool: "Bash", input: { command: "go test" }, decision: "pending" }, 1)]);
    expect(pending.items[0]).toMatchObject({ kind: "approval", id: 7, tool: "Bash", decision: "pending" });
    const allowed = reduceAll([ev("approval", { approval: 7, decision: "allow", always: true, by: "user" }, 2)], pending);
    expect(allowed.items).toHaveLength(1);
    expect(allowed.items[0]).toMatchObject({ id: 7, decision: "allow", always: true });
  });

  it("marks a user message queued until the agent takes it in", () => {
    const t = reduceAll([ev("user", { id: "m1", text: "one" }, 1), ev("delivered", { id: "m1" }, 2), ev("user", { id: "m2", text: "two" }, 3)]);
    expect(t.items.map((i) => isQueued(i, "running"))).toEqual([false, true]);
    expect(t.items.map((i) => isQueued(i, "idle"))).toEqual([false, false]);
    const answered = reduceAll([delta("text", "cut"), ev("delivered", { id: "m2" }, 4), ev("result", { costUsd: 0.1 }, 5)], t);
    expect(answered.items.map((i) => isQueued(i, "running"))).toEqual([false, false, false, false]);
    expect(answered.items[2]).toMatchObject({ kind: "text", streaming: false });
  });
});

describe("tool lines", () => {
  it("summarises inputs", () => {
    expect(toolSummary("Bash", { command: "go test ./...\necho" })).toBe("go test ./...");
    expect(toolSummary("Grep", { pattern: "Allow", path: "internal" })).toBe("Allow in internal");
    expect(toolSummary("mcp__layer8__do", { ops: [{ target: "task:a", action: "start-session" }] })).toBe("task:a → start-session");
    expect(toolLabel("mcp__layer8__do")).toBe("layer8 · do");
  });

  it("diffs edits around their common lines", () => {
    const d = editDiff("Edit", { old_string: "a\nb\nc\nd", new_string: "a\nb\nC\nd" })!;
    expect(d).toMatchObject({ added: 1, deleted: 1 });
    expect(d.lines.map((l) => l.kind + l.text)).toEqual(["ctxa", "ctxb", "delc", "addC", "ctxd"]);
    expect(editDiff("Write", { content: "x\ny" })).toMatchObject({ added: 2, deleted: 0 });
    expect(editDiff("Read", {})).toBeNull();
  });
});
