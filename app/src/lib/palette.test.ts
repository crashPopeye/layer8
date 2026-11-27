import { FileText } from "lucide-react";
import { expect, it, vi } from "vitest";
import { buildPalette, type PaletteInput } from "./palette";
import type { Node } from "./types";

const node = (n: Partial<Node> & Pick<Node, "addr" | "kind" | "title">): Node => ({ children: [], actions: [], ...n });

const input: PaletteInput = {
  app: node({
    addr: "app",
    kind: "app",
    title: "layer8",
    actions: [{ name: "add-project", help: "Add a project" }],
    children: [
      { addr: "project:api", kind: "project", title: "api", children: [{ addr: "task:eng-1", kind: "task", title: "Rate limits" }] },
      { addr: "workflow:plan", kind: "workflow", title: "plan" },
      { addr: "workflow:broken", kind: "workflow", title: "broken", status: "invalid" },
      { addr: "ui:theme", kind: "ui", title: "theme" },
    ],
  }),
  chain: [
    node({
      addr: "task:eng-1",
      kind: "task",
      title: "Rate limits",
      actions: [
        { name: "start-session", help: "Start an agent session here" },
        { name: "run-workflow", help: "Run a workflow" },
      ],
      children: [
        { addr: "session:9c41e0aa", kind: "session", title: "Implement" },
        { addr: "artifact:eng-1/plan.md", kind: "artifact", title: "plan.md" },
      ],
    }),
    node({ addr: "project:api", kind: "project", title: "api", actions: [{ name: "create-task", help: "Create a task" }] }),
  ],
  layout: node({ addr: "ui:layout", kind: "ui", title: "layout", fields: { presets: ["Default", "Mine"] } }),
  theme: node({ addr: "ui:theme", kind: "ui", title: "theme", fields: { themes: { layer8: {}, ocean: {} } } }),
  panels: [{ id: "open-prs", title: "Open PRs", icon: FileText, component: () => null, defaultPosition: "right", plugin: "open-prs" }],
  commands: [{ id: "open-prs.show", title: "Show open PRs", plugin: "open-prs", run: vi.fn() }],
};

function build() {
  const deps = { focus: vi.fn(), openAction: vi.fn(), run: vi.fn(), openPanel: vi.fn(), savePreset: vi.fn() };
  const items = buildPalette(input, deps);
  const pick = (title: string, group?: string) => {
    const item = items.find((i) => i.title === title && (!group || i.group === group));
    if (!item) throw new Error(`no item ${group ?? ""} ${title}`);
    item.run();
  };
  return { items, deps, pick };
}

it("groups nodes, actions, workflows, layouts, themes, panels and commands", () => {
  const { items } = build();
  const by = (group: string) => items.filter((i) => i.group === group).map((i) => i.title);
  expect(by("Go to")).toEqual(["api", "Rate limits", "Implement", "plan.md"]);
  expect(by("Actions")).toEqual(["start-session", "run-workflow", "create-task", "add-project"]);
  expect(by("Workflows")).toEqual(["Run plan"]);
  expect(by("Layout")).toEqual(["Layout: Default", "Layout: Mine", "Save layout as preset…", "Delete a layout preset…"]);
  expect(by("Theme")).toEqual(["Theme: layer8", "Theme: ocean", "Mode: dark", "Mode: light", "Mode: system"]);
  expect(by("Panels")).toEqual(["Open Open PRs"]);
  expect(by("Commands")).toEqual(["Show open PRs"]);
});

it("runs each item through the right dependency", () => {
  const { deps, pick } = build();
  pick("Implement");
  expect(deps.focus).toHaveBeenCalledWith("session:9c41e0aa", "task:eng-1");
  pick("api");
  expect(deps.focus).toHaveBeenLastCalledWith("project:api", undefined);
  pick("create-task");
  expect(deps.openAction).toHaveBeenCalledWith("project:api", "create-task");
  pick("Run plan");
  expect(deps.run).toHaveBeenLastCalledWith({ target: "task:eng-1", action: "run-workflow", args: { workflow: "plan" } });
  pick("Layout: Mine");
  expect(deps.run).toHaveBeenLastCalledWith({ target: "ui:layout", action: "preset", args: { name: "Mine" } });
  pick("Mode: system");
  expect(deps.run).toHaveBeenLastCalledWith({ target: "ui:theme", action: "set", args: { mode: "system" } });
  pick("Open Open PRs");
  expect(deps.openPanel).toHaveBeenCalledWith("open-prs");
  pick("Save layout as preset…");
  expect(deps.savePreset).toHaveBeenCalled();
  pick("Show open PRs");
  expect(input.commands[0].run).toHaveBeenCalled();
});

it("offers no workflows, layouts or themes when their nodes are missing", () => {
  const items = buildPalette(
    { app: null, chain: [], layout: null, theme: null, panels: [], commands: [] },
    { focus: vi.fn(), openAction: vi.fn(), run: vi.fn(), openPanel: vi.fn(), savePreset: vi.fn() },
  );
  expect(items).toEqual([]);
});
