import type { Addr, Depth, Node, Summary } from "@/lib/types";

/** A project, task or sub-task with its sub-tasks at every depth and its sessions. */
export interface TreeItem {
  addr: Addr;
  kind: string;
  title: string;
  status?: string;
  children: TreeItem[];
  sessions: Summary[];
}

type Get = (addr: Addr, depth: Depth) => Promise<Node>;

/** Reads every project with its tasks and sub-tasks, two levels per request. */
export async function loadTree(get: Get): Promise<TreeItem[]> {
  const app = await get("app", 2);
  return Promise.all(app.children.filter((c) => c.kind === "project").map((p) => expand(p, 1, get)));
}

async function expand(s: Summary, known: number, get: Get): Promise<TreeItem> {
  const children = known > 0 ? (s.children ?? []) : (await get(s.addr, 2)).children;
  const tasks = children.filter((c) => c.kind === "task");
  return {
    addr: s.addr,
    kind: s.kind,
    title: s.title,
    status: s.status,
    children: await Promise.all(tasks.map((c) => expand(c, known > 0 ? known - 1 : 1, get))),
    sessions: children.filter((c) => c.kind === "session"),
  };
}

/** Every item of the tree, depth first. */
export function walk(items: TreeItem[]): TreeItem[] {
  return items.flatMap((i) => [i, ...walk(i.children)]);
}

/** The status a task row shows: waiting or running when one of its sessions is, its own otherwise. */
export function liveStatus(item: TreeItem): string | undefined {
  const st = new Set(item.sessions.map((s) => s.status));
  if (st.has("waiting")) return "waiting";
  if (st.has("running") || st.has("starting")) return "running";
  return item.status;
}

/** Counts the sessions of the whole tree that are running, waiting for the user, or queued. */
export function sessionCounts(items: TreeItem[]): { running: number; waiting: number; queued: number } {
  const all = walk(items).flatMap((i) => i.sessions.map((s) => s.status));
  const count = (...st: string[]) => all.filter((s) => st.includes(s ?? "")).length;
  return { running: count("running", "starting"), waiting: count("waiting"), queued: count("queued") };
}

/** One visible line of the tree. */
export interface Row {
  item: TreeItem;
  depth: number;
}

/** Flattens the tree into visible rows, skipping collapsed sub-trees and, unless asked, archived tasks. */
export function rows(items: TreeItem[], collapsed: Set<Addr>, archived: boolean, depth = 0): Row[] {
  return items
    .filter((i) => archived || i.status !== "archived")
    .flatMap((item) => [
      { item, depth },
      ...(collapsed.has(item.addr) ? [] : rows(item.children, collapsed, archived, depth + 1)),
    ]);
}
