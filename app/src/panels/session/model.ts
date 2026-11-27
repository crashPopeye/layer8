import type { SessionEvent } from "@/lib/types";

export type { SessionEvent };

export interface Image {
  mediaType: string;
  data: string;
}

export type Item =
  | { kind: "user"; key: string; id?: string; delivered?: boolean; text: string; files?: string[]; images?: Image[]; by?: string }
  | { kind: "text" | "thinking"; key: string; text: string; streaming?: boolean }
  | ToolItem
  | ApprovalItem
  | { kind: "result"; key: string; data: Record<string, any> }
  | { kind: "error"; key: string; message: string };

export interface ToolItem {
  kind: "tool";
  key: string;
  name: string;
  input: any;
  result?: { text: string; isError: boolean };
  children: Item[];
}

export interface ApprovalItem {
  kind: "approval";
  key: string;
  id: number;
  tool: string;
  input: any;
  description?: string;
  decision: string;
  always?: boolean;
}

/** A session's transcript folded from its events: top-level items, subagent work nested under the tool that started it. */
export interface Transcript {
  items: Item[];
  /** For each tool use id, the chain of tool ids that contains it. */
  paths: Record<string, string[]>;
  lastId: number;
  partials: number;
}

export const empty: Transcript = { items: [], paths: {}, lastId: 0, partials: 0 };

function update(items: Item[], path: string[], fn: (list: Item[]) => Item[]): Item[] {
  if (!path.length) return fn(items);
  return items.map((it) =>
    it.kind === "tool" && it.key === path[0] ? { ...it, children: update(it.children, path.slice(1), fn) } : it,
  );
}

function listOf(t: Transcript, parent: unknown): string[] {
  return typeof parent === "string" && parent in t.paths ? [...t.paths[parent], parent] : [];
}

/** Folds one event into the transcript; stored events at or below lastId are skipped as already seen. */
export function reduce(t: Transcript, e: SessionEvent): Transcript {
  if (e.id !== undefined) {
    if (e.id <= t.lastId) return t;
    t = { ...t, lastId: e.id };
  }
  const d = e.data ?? {};
  const path = listOf(t, d.parentToolUseId);
  const key = e.id !== undefined ? `e${e.id}` : `p${t.partials}`;
  const edit = (fn: (list: Item[]) => Item[], at = path) => ({ ...t, items: update(t.items, at, fn) });

  switch (e.type) {
    case "text":
    case "thinking": {
      const kind = e.type;
      if (e.partial)
        return {
          ...edit((list) => {
            const last = list.at(-1);
            if (last?.kind === kind && last.streaming) return [...list.slice(0, -1), { ...last, text: last.text + (d.text ?? "") }];
            return [...list, { kind, key, text: d.text ?? "", streaming: true }];
          }),
          partials: t.partials + 1,
        };
      return edit((list) => {
        const i = list.findIndex((it) => it.kind === kind && it.streaming);
        if (i < 0) return [...list, { kind, key, text: d.text ?? "" }];
        return list.map((it, j) => (j === i ? { kind, key: it.key, text: d.text ?? "" } : it));
      });
    }
    case "tool_use": {
      if (e.partial) return t;
      const id = String(d.id ?? key);
      const next = edit((list) => [...list, { kind: "tool", key: id, name: d.name ?? "?", input: d.input ?? {}, children: [] }]);
      return { ...next, paths: { ...t.paths, [id]: path } };
    }
    case "tool_result": {
      const id = String(d.toolUseId);
      if (!(id in t.paths)) return t;
      const result = { text: resultText(d.content), isError: !!d.isError };
      return edit((list) => list.map((it) => (it.kind === "tool" && it.key === id ? { ...it, result } : it)), t.paths[id]);
    }
    case "approval": {
      if (d.decision === "pending")
        return edit(
          (list) => [
            ...list,
            { kind: "approval", key, id: d.approval, tool: d.tool, input: d.input, description: d.description, decision: "pending" },
          ],
          [],
        );
      return edit(
        (list) =>
          list.map((it) => (it.kind === "approval" && it.id === d.approval ? { ...it, decision: d.decision, always: d.always } : it)),
        [],
      );
    }
    case "user":
      return edit((list) => [...list, { kind: "user", key, id: d.id, text: d.text ?? "", files: d.files, images: d.images, by: d.by }], []);
    case "delivered":
      return edit((list) => list.map((it) => (it.kind === "user" && it.id === d.id ? { ...it, delivered: true } : it)), []);
    case "result":
      return edit((list) => [...settle(list), { kind: "result", key, data: d }], []);
    case "error":
      return edit((list) => [...settle(list), { kind: "error", key, message: String(d.message ?? "error") }], []);
  }
  return t;
}

function settle(list: Item[]): Item[] {
  return list.map((it) => ((it.kind === "text" || it.kind === "thinking") && it.streaming ? { ...it, streaming: false } : it));
}

/** Folds a list of events. */
export const reduceAll = (events: SessionEvent[], t: Transcript = empty) => events.reduce(reduce, t);

/** Whether a user message waits for the agent to take it in while the session is busy. */
export function isQueued(item: Item, status: string): boolean {
  return item.kind === "user" && !!item.id && !item.delivered && ["queued", "starting", "running", "waiting"].includes(status);
}

/** Flattens a tool result's content, a string or a list of content blocks, to text. */
export function resultText(content: unknown): string {
  if (typeof content === "string") return content;
  if (!Array.isArray(content)) return content == null ? "" : JSON.stringify(content, null, 2);
  return content.map((b) => (b?.type === "text" ? b.text : b?.type === "image" ? "[image]" : JSON.stringify(b))).join("\n");
}

/** Turns `mcp__server__tool` into `server · tool`. */
export function toolLabel(name: string): string {
  const m = /^mcp__(.+?)__(.+)$/.exec(name);
  return m ? `${m[1]} · ${m[2]}` : name;
}

/** The one-line gist of a tool call's input. */
export function toolSummary(name: string, input: any): string {
  if (!input || typeof input !== "object") return "";
  if (name === "mcp__layer8__do" && Array.isArray(input.ops))
    return input.ops.map((o: any) => `${o.target} → ${o.action}${o.args ? " " + JSON.stringify(o.args) : ""}`).join(", ");
  if (name === "TodoWrite" && Array.isArray(input.todos)) return `${input.todos.length} todos`;
  const first =
    input.file_path ?? input.notebook_path ?? input.command ?? input.pattern ?? input.url ?? input.query ?? input.description ?? input.skill;
  const s = first ?? Object.values(input).find((v) => typeof v === "string") ?? JSON.stringify(input);
  const line = String(s).split("\n")[0];
  return name === "Grep" && input.path ? `${line} in ${input.path}` : line;
}

export interface DiffLine {
  kind: "add" | "del" | "ctx";
  text: string;
}

const context = 2;

function lineDiff(before: string, after: string): DiffLine[] {
  const a = before ? before.split("\n") : [];
  const b = after ? after.split("\n") : [];
  let head = 0;
  while (head < a.length && head < b.length && a[head] === b[head]) head++;
  let tail = 0;
  while (tail < a.length - head && tail < b.length - head && a[a.length - 1 - tail] === b[b.length - 1 - tail]) tail++;
  const ctx = (lines: string[]) => lines.map((text) => ({ kind: "ctx" as const, text }));
  return [
    ...ctx(a.slice(Math.max(0, head - context), head)),
    ...a.slice(head, a.length - tail).map((text) => ({ kind: "del" as const, text })),
    ...b.slice(head, b.length - tail).map((text) => ({ kind: "add" as const, text })),
    ...ctx(a.slice(a.length - tail, a.length - tail + context)),
  ];
}

/** The mini-diff of an Edit, MultiEdit or Write call, or null for other tools. */
export function editDiff(name: string, input: any): { lines: DiffLine[]; added: number; deleted: number } | null {
  let lines: DiffLine[];
  if (name === "Edit") lines = lineDiff(input?.old_string ?? "", input?.new_string ?? "");
  else if (name === "MultiEdit" && Array.isArray(input?.edits))
    lines = input.edits.flatMap((e: any) => lineDiff(e.old_string ?? "", e.new_string ?? ""));
  else if (name === "Write") lines = lineDiff("", input?.content ?? "");
  else return null;
  return {
    lines,
    added: lines.filter((l) => l.kind === "add").length,
    deleted: lines.filter((l) => l.kind === "del").length,
  };
}
