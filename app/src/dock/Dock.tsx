import {
  DockviewReact,
  themeAbyssSpaced,
  type DockviewApi,
  type DockviewGroupPanel,
  type IDockviewHeaderActionsProps,
  type IDockviewPanelProps,
  type SerializedDockview,
} from "dockview-react";
import { ExternalLink, Puzzle } from "lucide-react";
import { ErrorBoundary } from "@/components/error-card";
import { useClient } from "@/lib/react";
import type { Client } from "@/lib/types";
import { toast } from "@/shell/Toasts";
import { arrangement } from "./presets";
import { panel, panels, placement, usePanels, type Slot } from "./registry";

let dock: DockviewApi | null = null;
let pendingSave: (() => Promise<unknown>) | null = null;

function groupsBySlot(api: DockviewApi): Partial<Record<Slot, DockviewGroupPanel>> {
  const groups: Partial<Record<Slot, DockviewGroupPanel>> = {};
  for (const p of api.panels) {
    const slot = (p.params?.slot as Slot | undefined) ?? panel(p.id)?.defaultPosition;
    if (slot && !groups[slot] && p.group.api.location.type === "grid") groups[slot] = p.group;
  }
  return groups;
}

/** Shows a registered panel: activates it when open, adds it at slot (its default position otherwise) when not. */
export function openPanel(id: string, slot?: Slot) {
  const def = panel(id);
  if (!dock || !def) return;
  const open = dock.getPanel(id);
  if (open) return open.api.setActive();
  const at = slot ?? def.defaultPosition;
  dock.addPanel({ id, component: "panel", title: def.title, params: { slot: at }, position: placement(at, groupsBySlot(dock)) });
}

const slotOrder: Slot[] = ["center", "left", "right", "right-bottom"];
const widths: Partial<Record<Slot, number>> = { left: 240, right: 380 };

function arrange(api: DockviewApi, name: string) {
  api.clear();
  const list = arrangement(name, panels());
  for (const slot of slotOrder) list.filter(([, s]) => s === slot).forEach(([id]) => openPanel(id, slot));
  for (const slot of slotOrder) api.getPanel(list.find(([, s]) => s === slot)?.[0] ?? "")?.api.setActive();
  const groups = groupsBySlot(api);
  for (const [slot, width] of Object.entries(widths)) groups[slot as Slot]?.api.setSize({ width });
}

/** Switches the dock to a preset: a saved layout when given, the built-in arrangement called name otherwise. */
export function applyPreset(name: string, layout?: SerializedDockview) {
  if (!dock) return;
  if (!layout) return arrange(dock, name);
  try {
    dock.fromJSON(layout);
  } catch (e) {
    toast("error", `Layout ${name} could not be restored: ${(e as Error).message}`);
    arrange(dock, "Default");
  }
}

/** Activates the n-th panel of the dock, counting from 1. */
export function focusPanel(n: number) {
  dock?.panels[n - 1]?.api.setActive();
}

/** Saves the current layout now instead of after the debounce. */
export async function flushLayout() {
  await pendingSave?.();
}

function persist(api: DockviewApi, client: Client) {
  let timer: ReturnType<typeof setTimeout> | undefined;
  const save = () => {
    clearTimeout(timer);
    pendingSave = null;
    return client.doOps([{ target: "ui:layout", action: "save-layout", args: { layout: api.toJSON() } }]);
  };
  api.onDidLayoutChange(() => {
    clearTimeout(timer);
    pendingSave = save;
    timer = setTimeout(save, 800);
  });
}

const revisions = new WeakMap<object, number>();
let revisionCount = 0;

function revision(def: object): number {
  if (!revisions.has(def)) revisions.set(def, ++revisionCount);
  return revisions.get(def)!;
}

function PanelHost({ api }: IDockviewPanelProps) {
  const client = useClient();
  const def = usePanels().find((p) => p.id === api.id);
  if (!def)
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 p-4 text-center text-muted-foreground">
        <Puzzle className="size-5" />
        <p>
          No panel <code className="font-mono">{api.id}</code> is registered. Its plugin may still be loading, or have failed: see the
          Plugins panel.
        </p>
      </div>
    );
  const report = (e: Error) =>
    client.doOps([{ target: "ui:plugins", action: "report", args: { plugin: def.plugin, error: `panel ${def.id}: ${e.message}`, panels: [def.id] } }]);
  return (
    <ErrorBoundary key={revision(def)} title={`${def.title} failed`} onError={def.plugin ? report : undefined}>
      <def.component />
    </ErrorBoundary>
  );
}

function PopoutAction({ containerApi, group, api }: IDockviewHeaderActionsProps) {
  if (api.location.type === "popout") return null;
  return (
    <button
      className="flex h-full items-center px-2 text-muted-foreground hover:text-foreground"
      title="Open in its own window"
      onClick={() => containerApi.addPopoutGroup(group, { popoutUrl: "/popout.html" })}
    >
      <ExternalLink className="size-3.5" />
    </button>
  );
}

const components = { panel: PanelHost };

/** The window's dock: the saved layout, or the Default preset, kept in ui:layout as it changes. */
export function Dock() {
  const client = useClient();
  return (
    <DockviewReact
      theme={{ ...themeAbyssSpaced, name: "layer8", gap: 6 }}
      components={components}
      rightHeaderActionsComponent={PopoutAction}
      onReady={async ({ api }) => {
        dock = api;
        const saved = await client.getNode("ui:layout", 0).then(
          (n) => n.fields?.layout as SerializedDockview | null,
          () => null,
        );
        if (saved) applyPreset("saved", saved);
        else arrange(api, "Default");
        persist(api, client);
      }}
    />
  );
}
