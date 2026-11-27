import { icons, Puzzle, type LucideIcon } from "lucide-react";
import * as React from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { Input, NativeSelect, Textarea } from "@/components/ui/input";
import { openPanel } from "@/dock/Dock";
import { registerPanel, type Slot } from "@/dock/registry";
import { setFocus, useFocus } from "@/lib/focus";
import { registerCommand } from "@/lib/palette";
import { useNode } from "@/lib/react";
import { openUrl } from "@/lib/tauri";
import { token } from "@/lib/theme";
import type { Addr, Client, Depth, Node, Op, Result } from "@/lib/types";
import { cn } from "@/lib/utils";
import { toast } from "@/shell/Toasts";

function Badge({ className, ...props }: React.ComponentProps<"span">) {
  return <span className={cn("inline-flex items-center rounded-md border px-1.5 py-0.5 text-[11px] font-medium", className)} {...props} />;
}

function Card({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("rounded-lg border bg-card p-3 text-card-foreground", className)} {...props} />;
}

function ScrollArea({ className, ...props }: React.ComponentProps<"div">) {
  return <div className={cn("overflow-auto", className)} {...props} />;
}

const ui = { Button, Input, Textarea, NativeSelect, Badge, Card, ScrollArea, Dialog, DialogContent, DialogTitle, DialogDescription };

export interface PluginPanel {
  id: string;
  title: string;
  icon?: string | LucideIcon;
  component: React.FunctionComponent;
  defaultPosition?: Slot;
}

export interface PluginCommand {
  id: string;
  title: string;
  group?: string;
  run: () => void;
}

/** What a plugin's default export receives. Documented for agents in the `sdk` field of ui:plugins. */
export interface SDK {
  name: string;
  React: typeof React;
  ui: typeof ui;
  icons: typeof icons;
  get(addr: Addr, depth?: Depth): Promise<Node>;
  watch(addr: Addr, depth: Depth, onNode: (n: Node) => void): () => void;
  useNode(addr: Addr, depth?: Depth): Node | null;
  do(ops: Op[]): Promise<Result[]>;
  focus(addr: Addr, task?: Addr): void;
  useFocus(): Addr;
  registerPanel(p: PluginPanel): void;
  registerCommand(c: PluginCommand): void;
  openPanel(id: string): void;
  openUrl(url: string): void;
  notify(text: string, level?: string): void;
  theme: { mode(): string; token(name: string): string };
}

/** A plugin's SDK, with the ids it registered and the function that removes them and stops its watches. */
export interface Sandbox {
  sdk: SDK;
  panels: string[];
  commands: string[];
  dispose(): void;
}

/** Returns a fresh SDK for the plugin called name, acting on the daemon as `plugin:<name>`. */
export function createSandbox(name: string, client: Client): Sandbox {
  const cleanups: (() => void)[] = [];
  const box: Sandbox = {
    panels: [],
    commands: [],
    dispose: () => cleanups.splice(0).forEach((c) => c()),
    sdk: {
      name,
      React,
      ui,
      icons,
      get: (addr, depth = 1) => client.getNode(addr, depth),
      watch(addr, depth, onNode) {
        const load = () => client.getNode(addr, depth).then(onNode, () => {});
        load();
        const off = client.subscribe((e) => e.type === "node" && e.data.addrs.includes(addr) && load());
        cleanups.push(off);
        return off;
      },
      useNode: (addr, depth = 1) => useNode(addr, depth).node,
      do: (ops) => client.doOps(ops, `plugin:${name}`),
      focus: setFocus,
      useFocus,
      registerPanel(p) {
        const icon = typeof p.icon === "string" ? icons[p.icon as keyof typeof icons] : p.icon;
        cleanups.push(
          registerPanel({ ...p, icon: icon ?? Puzzle, defaultPosition: p.defaultPosition ?? "right", plugin: name }),
        );
        box.panels.push(p.id);
      },
      registerCommand(c) {
        cleanups.push(registerCommand({ ...c, plugin: name }));
        box.commands.push(c.id);
      },
      openPanel: (id) => openPanel(id),
      openUrl,
      notify: (text, level = "info") => toast(level, text),
      theme: { mode: () => document.documentElement.dataset.theme ?? "dark", token },
    },
  };
  return box;
}
