import type { Addr, Node, Summary } from "@/lib/types";
import type { Label } from "../pr/model";

/** A task's `linear` field, as `internal/linear` stores it. */
export interface Link {
  id: string;
  url: string;
  title: string;
  state: { name: string; type: string };
}

/** Returns the task's Linear link, or null when it has none. */
export function linkOf(node: Node | null): Link | null {
  return (node?.fields?.linear as Link | null | undefined) ?? null;
}

/** The top-bar pill of a link: "ENG-482 · In Progress", coloured by the state's type. */
export function pillOf(link: Link): Label {
  const tone = ({ started: "primary", completed: "ok" } as const)[link.state.type as "started" | "completed"] ?? "muted";
  return { text: `${link.id} · ${link.state.name}`, tone };
}

/** Returns the address of the root task of a task address. */
export function rootOf(addr: Addr): Addr {
  return addr.split("/")[0];
}

/** Returns the issue identifier of a `linear:<ID>` summary. */
export function issueId(s: Summary): string {
  return s.addr.slice("linear:".length);
}

/** Returns the title of an issue summary without its leading identifier. */
export function issueTitle(s: Summary): string {
  const id = issueId(s);
  return s.title.startsWith(id + " ") ? s.title.slice(id.length + 1) : s.title;
}
