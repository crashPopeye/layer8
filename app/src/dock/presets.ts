import type { PanelDef, Slot } from "./registry";

/** The panels a layout shows, each with the slot it goes in. */
export type Arrangement = [id: string, slot: Slot][];

const builtins: Record<string, Arrangement> = {
  Default: [
    ["tasks", "left"],
    ["session", "center"],
    ["artifacts", "right"],
    ["pr", "right"],
    ["files", "right"],
    ["changes", "right-bottom"],
  ],
  Focus: [
    ["tasks", "left"],
    ["session", "center"],
  ],
  Review: [
    ["tasks", "left"],
    ["changes", "center"],
    ["artifacts", "right"],
    ["pr", "right"],
    ["session", "right-bottom"],
  ],
  "Wide diff": [
    ["changes", "center"],
    ["pr", "right"],
  ],
};

/** Returns the registered panels of a built-in preset, Default for an unknown name. */
export function arrangement(name: string, defs: PanelDef[]): Arrangement {
  const ids = new Set(defs.map((d) => d.id));
  return (builtins[name] ?? builtins.Default).filter(([id]) => ids.has(id));
}
