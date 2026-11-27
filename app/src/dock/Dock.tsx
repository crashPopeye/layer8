import { DockviewReact, themeAbyssSpaced, type DockviewApi, type DockviewGroupPanel } from "dockview-react";
import { useMemo } from "react";
import { panel, panels, placement, type Slot } from "./registry";

let dock: DockviewApi | null = null;

function groupsBySlot(api: DockviewApi): Partial<Record<Slot, DockviewGroupPanel>> {
  const groups: Partial<Record<Slot, DockviewGroupPanel>> = {};
  for (const p of api.panels) {
    const slot = panel(p.id)?.defaultPosition;
    if (slot && !groups[slot]) groups[slot] = p.group;
  }
  return groups;
}

/** Shows a registered panel: activates it when open, adds it at its default position otherwise. */
export function openPanel(id: string) {
  const def = panel(id);
  if (!dock || !def) return;
  const open = dock.getPanel(id);
  if (open) return open.api.setActive();
  dock.addPanel({ id, component: id, title: def.title, position: placement(def.defaultPosition, groupsBySlot(dock)) });
}

const slotOrder: Slot[] = ["center", "left", "right", "right-bottom"];
const widths: Partial<Record<Slot, number>> = { left: 240, right: 380 };

/** The window's dock: every registered panel in the default three-column layout. */
export function Dock() {
  const components = useMemo(() => Object.fromEntries(panels().map((d) => [d.id, d.component])), []);
  return (
    <DockviewReact
      theme={{ ...themeAbyssSpaced, name: "layer8", gap: 6 }}
      components={components}
      onReady={({ api }) => {
        dock = api;
        for (const slot of slotOrder) panels().filter((d) => d.defaultPosition === slot).forEach((d) => openPanel(d.id));
        for (const slot of slotOrder) api.getPanel(panels().find((d) => d.defaultPosition === slot)?.id ?? "")?.api.setActive();
        const groups = groupsBySlot(api);
        for (const [slot, width] of Object.entries(widths)) groups[slot as Slot]?.api.setSize({ width });
      }}
    />
  );
}
