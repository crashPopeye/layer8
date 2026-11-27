import type { Addr, Depth, Node, Summary } from "@/lib/types";

/** A project, task or sub-task with its sub-tasks at every depth. */
export interface TreeItem {
  addr: Addr;
  kind: string;
  title: string;
  status?: string;
  children: TreeItem[];
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
  };
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
