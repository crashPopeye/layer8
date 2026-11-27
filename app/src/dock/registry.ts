import type { FunctionComponent } from "react";
import type { LucideIcon } from "lucide-react";

/** Where a panel lands in the default layout: one of the three columns, or under the right one. */
export type Slot = "left" | "center" | "right" | "right-bottom";

export interface PanelDef {
  id: string;
  title: string;
  icon: LucideIcon;
  component: FunctionComponent;
  defaultPosition: Slot;
}

const registry = new Map<string, PanelDef>();

/** Adds a panel to the dock, replacing any panel with the same id. */
export function registerPanel(def: PanelDef) {
  registry.set(def.id, def);
}

/** Returns the registered panel with this id. */
export function panel(id: string): PanelDef | undefined {
  return registry.get(id);
}

/** Returns every registered panel, in registration order. */
export function panels(): PanelDef[] {
  return [...registry.values()];
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
