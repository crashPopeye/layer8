import { isTauri } from "@tauri-apps/api/core";
import { Moon, Search, Sun } from "lucide-react";
import { Fragment, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { openPanel } from "@/dock/Dock";
import { usePanels } from "@/dock/registry";
import { crumbTrail, type Crumb } from "@/lib/crumbs";
import { crumbs, setFocus, useFocus } from "@/lib/focus";
import { useClient, useNode } from "@/lib/react";
import { openUrl } from "@/lib/tauri";
import { resolveMode, usePrefersDark, type Mode } from "@/lib/theme";
import type { Addr } from "@/lib/types";
import { cn } from "@/lib/utils";
import { togglePalette } from "./Palette";
import { WorkflowControl } from "./Workflow";

/** Returns the breadcrumb of addr with node titles, falling back to address segments until they load. */
function useCrumbs(addr: string): Crumb[] {
  const client = useClient();
  const [trail, setTrail] = useState<{ addr: string; crumbs: Crumb[] } | null>(null);
  useEffect(() => {
    let live = true;
    const load = () => crumbTrail(client.getNode, addr).then((c) => live && setTrail({ addr, crumbs: c }), () => {});
    load();
    const off = client.subscribe((e) => e.type === "node" && e.data.addrs.includes(addr) && load());
    return () => {
      live = false;
      off();
    };
  }, [client, addr]);
  return trail?.addr === addr ? trail.crumbs : crumbs(addr).map((title) => ({ addr, title }));
}

/** The task's `link` field, set with the task's set-link action. */
interface Link {
  url: string;
  label: string;
}

/** The link of the task at addr, or of its root task, as a pill that opens it. */
function LinkPill({ addr }: { addr: Addr }) {
  const own = useNode(addr, 0).node?.fields?.link as Link | null | undefined;
  const root = useNode(addr.split("/")[0], 0).node?.fields?.link as Link | null | undefined;
  const link = own ?? root;
  if (!link) return null;
  return (
    <button
      className="max-w-48 truncate rounded-full border px-2 py-0.5 font-mono text-[11px] text-muted-foreground hover:bg-muted hover:text-foreground"
      title={`Open ${link.url}`}
      onClick={() => openUrl(link.url)}
    >
      {link.label}
    </button>
  );
}

/** The window's title bar: breadcrumb of the focused node, panel launchers, theme switch and ⌘K. */
export function TopBar() {
  const client = useClient();
  const focus = useFocus();
  const panels = usePanels().filter((p) => !p.plugin);
  const mode = useNode("ui:theme", 0).node?.fields?.mode as Mode | undefined;
  const dark = resolveMode(mode ?? "dark", usePrefersDark()) === "dark";
  const toggleTheme = () => client.doOps([{ target: "ui:theme", action: "set", args: { mode: dark ? "light" : "dark" } }]);
  const parts = useCrumbs(focus);

  return (
    <header
      data-tauri-drag-region
      className={cn("flex h-[42px] shrink-0 items-center gap-2 border-b pr-3", isTauri() ? "pl-[84px]" : "pl-3")}
    >
      <nav className="flex items-center gap-1.5 text-muted-foreground">
        <button className="hover:text-foreground" onClick={() => setFocus("app")}>
          layer8
        </button>
        {parts.map((p, i) => (
          <Fragment key={i}>
            <span>/</span>
            <button
              className={cn("hover:text-foreground", i === parts.length - 1 && "font-semibold text-foreground")}
              onClick={() => setFocus(p.addr)}
            >
              {p.title}
            </button>
          </Fragment>
        ))}
      </nav>
      {focus.startsWith("task:") && <LinkPill key={focus} addr={focus} />}
      <div data-tauri-drag-region className="flex-1 self-stretch" />
      <WorkflowControl />
      {panels.map(({ id, title, icon: Icon }) => (
        <Button key={id} variant="ghost" size="icon-sm" title={title} onClick={() => openPanel(id)}>
          <Icon />
        </Button>
      ))}
      <Button variant="ghost" size="icon-sm" title="Switch theme" onClick={toggleTheme}>
        {dark ? <Sun /> : <Moon />}
      </Button>
      <Button variant="outline" size="sm" className="h-7 text-muted-foreground" title="Command palette" onClick={togglePalette}>
        <Search />
        <kbd className="font-mono text-[11px]">⌘K</kbd>
      </Button>
    </header>
  );
}
