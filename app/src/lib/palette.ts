import type { PanelDef } from "@/dock/registry";
import { resolveMode, type Mode } from "./theme";
import type { Addr, Client, Node, Op, Summary } from "./types";

/** A ⌘K command: built in, or registered by a plugin. */
export interface Command {
  id: string;
  title: string;
  group?: string;
  plugin?: string;
  run: () => void;
}

const registry = new Map<string, Command>();

/** Adds a ⌘K command, replacing any with the same id, and returns the function that removes it. */
export function registerCommand(cmd: Command): () => void {
  registry.set(cmd.id, cmd);
  return () => {
    if (registry.get(cmd.id) === cmd) registry.delete(cmd.id);
  };
}

/** Returns every registered command. */
export function commands(): Command[] {
  return [...registry.values()];
}

/** One row of the palette. */
export interface Item {
  id: string;
  group: string;
  title: string;
  hint?: string;
  run: () => void;
}

/** What the palette is built from: the app, the focused node then its parents, the ui nodes, panels and commands. */
export interface PaletteInput {
  app: Node | null;
  chain: Node[];
  layout: Node | null;
  theme: Node | null;
  prefersDark: boolean;
  panels: PanelDef[];
  commands: Command[];
}

/** What palette items do when picked. */
export interface PaletteDeps {
  focus: (addr: Addr, task?: Addr) => void;
  openAction: (addr: Addr, action: string) => void;
  run: (op: Op) => void;
  openPanel: (id: string) => void;
  savePreset: () => void;
}

const navigable = new Set(["project", "task", "session", "artifact"]);

/** Builds the palette rows: nodes to focus, actions of the focused node and its parents, workflows, presets, themes, panels and commands. */
export function buildPalette(input: PaletteInput, deps: PaletteDeps): Item[] {
  const items: Item[] = [];
  const seen = new Set<string>();
  const add = (item: Item) => {
    if (seen.has(item.id)) return;
    seen.add(item.id);
    items.push(item);
  };

  const summaries: [Summary, Addr][] = [
    ...(input.app?.children ?? []).flatMap((p): [Summary, Addr][] => [[p, "app"], ...(p.children ?? []).map((t): [Summary, Addr] => [t, p.addr])]),
    ...input.chain.flatMap((n) => n.children.map((c): [Summary, Addr] => [c, n.addr])),
  ];
  for (const [s, parent] of summaries.filter(([s]) => navigable.has(s.kind))) {
    const task = s.kind === "session" ? parent : undefined;
    add({ id: `go:${s.addr}`, group: "Go to", title: s.title, hint: s.addr, run: () => deps.focus(s.addr, task) });
  }

  for (const n of [...input.chain, ...(input.app ? [input.app] : [])]) {
    for (const a of n.actions) {
      add({ id: `do:${n.addr}:${a.name}`, group: "Actions", title: a.name, hint: `${n.title} · ${a.help}`, run: () => deps.openAction(n.addr, a.name) });
    }
  }

  const task = input.chain.find((n) => n.actions.some((a) => a.name === "run-workflow"));
  if (task) {
    for (const w of input.app?.children.filter((c) => c.kind === "workflow" && c.status !== "invalid") ?? []) {
      const op = { target: task.addr, action: "run-workflow", args: { workflow: w.title } };
      add({ id: `workflow:${w.title}`, group: "Workflows", title: `Run ${w.title}`, hint: task.title, run: () => deps.run(op) });
    }
  }

  for (const name of (input.layout?.fields?.presets as string[] | undefined) ?? []) {
    const op = { target: "ui:layout", action: "preset", args: { name } };
    add({ id: `preset:${name}`, group: "Layout", title: `Layout: ${name}`, run: () => deps.run(op) });
  }
  if (input.layout) {
    add({ id: "preset:save", group: "Layout", title: "Save layout as preset…", run: deps.savePreset });
    add({ id: "preset:delete", group: "Layout", title: "Delete a layout preset…", run: () => deps.openAction("ui:layout", "delete-preset") });
  }

  for (const theme of Object.keys((input.theme?.fields?.themes as object | undefined) ?? {})) {
    const op = { target: "ui:theme", action: "set", args: { theme } };
    add({ id: `theme:${theme}`, group: "Theme", title: `Theme: ${theme}`, run: () => deps.run(op) });
  }
  if (input.theme) {
    const dark = resolveMode((input.theme.fields?.mode as Mode | undefined) ?? "dark", input.prefersDark) === "dark";
    const op = { target: "ui:theme", action: "set", args: { mode: dark ? "light" : "dark" } };
    add({ id: "mode:toggle", group: "Theme", title: "Toggle light / dark", hint: dark ? "light" : "dark", run: () => deps.run(op) });
    for (const mode of ["dark", "light", "system"]) {
      const op = { target: "ui:theme", action: "set", args: { mode } };
      add({ id: `mode:${mode}`, group: "Theme", title: `Mode: ${mode}`, run: () => deps.run(op) });
    }
  }

  for (const p of input.panels) {
    add({ id: `panel:${p.id}`, group: "Panels", title: `Open ${p.title}`, hint: p.plugin, run: () => deps.openPanel(p.id) });
  }

  for (const c of input.commands) {
    add({ id: `cmd:${c.id}`, group: c.group ?? "Commands", title: c.title, hint: c.plugin, run: c.run });
  }
  return items;
}

/** Reads the nodes the palette is built from: the focused node and its parents with their actions, the app with projects and tasks, and the ui nodes. */
export async function paletteNodes(client: Client, focus: Addr): Promise<Pick<PaletteInput, "app" | "chain" | "layout" | "theme">> {
  const get = (addr: Addr, depth: 0 | 1 | 2) => client.getNode(addr, depth).catch(() => null);
  const chain: Node[] = [];
  for (let a: Addr | undefined = focus; a && a !== "app"; ) {
    const n = await get(a, 1);
    if (!n) break;
    chain.push(n);
    a = (n.fields?.parent ?? n.fields?.task) as Addr | undefined;
  }
  const [app, layout, theme] = await Promise.all([get("app", 2), get("ui:layout", 0), get("ui:theme", 0)]);
  return { app, chain, layout, theme };
}
