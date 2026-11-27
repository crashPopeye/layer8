import type { Addr, Depth, Node } from "./types";

/** One step of the breadcrumb. */
export interface Crumb {
  addr: Addr;
  title: string;
}

/** Walks from addr up through its parents to the project, returning the steps top-down with their titles. */
export async function crumbTrail(get: (addr: Addr, depth: Depth) => Promise<Node>, addr: Addr): Promise<Crumb[]> {
  const trail: Crumb[] = [];
  for (let a: Addr | undefined = addr; a && a !== "app";) {
    const n = await get(a, 0);
    trail.unshift({ addr: a, title: n.title });
    a = (n.fields?.parent ?? n.fields?.task) as Addr | undefined;
  }
  return trail;
}
