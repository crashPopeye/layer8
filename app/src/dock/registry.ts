import { useSyncExternalStore, type FunctionComponent } from "react";
import type { LucideIcon } from "lucide-react";

/** Where a panel lands in the default layout: one of the three columns, or under the right one. */
export type Slot = "left" | "center" | "right" | "right-bottom";

export interface PanelDef {
  id: string;
  title: string;
  icon: LucideIcon;
  component: FunctionComponent;
  defaultPosition: Slot;
  /** The plugin that registered the panel; built-in panels have none. */
  plugin?: string;
}

let registry = new Map<string, PanelDef>();
const subscribers = new Set<() => void>();

function changed(next: Map<string, PanelDef>) {
  registry = next;
  subscribers.forEach((s) => s());
}

/** Adds a panel to the dock, replacing any panel with the same id, and returns the function that removes it. */
export function registerPanel(def: PanelDef): () => void {
  changed(new Map(registry).set(def.id, def));
  return () => {
    if (registry.get(def.id) !== def) return;
    const next = new Map(registry);
    next.delete(def.id);
    changed(next);
  };
}

/** Returns the registered panel with this id. */
export function panel(id: string): PanelDef | undefined {
  return registry.get(id);
}

/** Returns every registered panel, in registration order. */
export function panels(): PanelDef[] {
  return [...registry.values()];
}

/** Returns every registered panel and re-renders when one is added or removed. */
export function usePanels(): PanelDef[] {
  const map = useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    () => registry,
  );
  return [...map.values()];
}

type Direction = "left" | "right" | "above" | "below";

const neighbours: Record<Slot, [Slot, Direction][]> = {
  center: [["left", "right"], ["right", "left"], ["right-bottom", "left"]],
  left: [["center", "left"], ["right", "left"], ["right-bottom", "left"]],
  right: [["right-bottom", "above"], ["center", "right"], ["left", "right"]],
  "right-bottom": [["right", "below"], ["center", "right"], ["left", "right"]],
};

/** Says where to add a panel given the groups already on screen: as a tab of its slot, or split off the nearest neighbour. */
export function placement<G>(
  slot: Slot,
  groups: Partial<Record<Slot, G>>,
): { referenceGroup: G; direction?: Direction } | undefined {
  const own = groups[slot];
  if (own) return { referenceGroup: own };
  for (const [near, direction] of neighbours[slot]) {
    const g = groups[near];
    if (g) return { referenceGroup: g, direction };
  }
  return undefined;
}
