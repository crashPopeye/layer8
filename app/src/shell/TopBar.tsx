import { isTauri } from "@tauri-apps/api/core";
import { Search, X } from "lucide-react";
import { Fragment, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { crumbTrail, type Crumb } from "@/lib/crumbs";
import { closeTab, crumbs, setFocus, tabOf, useFocus, useTabs, type Tab } from "@/lib/focus";
import { useClient, useNode } from "@/lib/react";
import { openUrl } from "@/lib/tauri";
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

/** One open task or session: click focuses where it was left, middle-click or × closes it. */
function TabItem({ tab, active }: { tab: Tab; active: boolean }) {
  const title = useNode(tab.key, 0).node?.title ?? crumbs(tab.key).at(-1) ?? tab.key;
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (active) ref.current?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [active]);
  return (
    <div
      ref={ref}
      role="tab"
      aria-selected={active}
      title={tab.addr}
      onMouseDown={(e) => e.button === 1 && e.preventDefault()}
      onAuxClick={(e) => e.button === 1 && closeTab(tab.key)}
      className={cn(
        "group flex h-7 max-w-[180px] shrink-0 items-center rounded-md border border-transparent text-[12px]",
        active ? "border-border bg-muted text-foreground" : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
      )}
    >
      <button className="min-w-0 truncate py-1 pr-1 pl-2.5" onClick={() => setFocus(tab.addr)}>
        {title}
      </button>
      <button
        aria-label={`Close ${title}`}
        className={cn(
          "mr-1 grid size-4 shrink-0 place-items-center rounded-sm hover:bg-foreground/10",
          !active && "opacity-0 group-hover:opacity-100 focus-visible:opacity-100",
        )}
        onClick={() => closeTab(tab.key)}
      >
        <X className="size-3" />
      </button>
    </div>
  );
}

/** The open tasks and sessions as tabs, scrolling sideways when they overflow; empty room drags the window. */
function Tabs() {
  const tabs = useTabs();
  const active = tabOf(useFocus());
  return (
    <div data-tauri-drag-region className="flex min-w-0 flex-1 items-center self-stretch">
      <div data-tauri-drag-region className="flex-1 self-stretch" />
      <div role="tablist" aria-label="Open tasks" className="flex min-w-0 items-center gap-1 overflow-x-auto [scrollbar-width:none]">
        {tabs.map((t) => (
          <TabItem key={t.key} tab={t} active={t.key === active} />
        ))}
      </div>
    </div>
  );
}

/** The window's title bar: breadcrumb of the focused node, open tabs, workflow control and ⌘K. */
export function TopBar() {
  const focus = useFocus();
  const parts = useCrumbs(focus);

  return (
    <header
      data-tauri-drag-region
      className={cn("flex h-[42px] shrink-0 items-center gap-2 border-b pr-3", isTauri() ? "pl-[84px]" : "pl-3")}
    >
      <nav className="flex shrink-0 items-center gap-1.5 text-muted-foreground">
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
      <Tabs />
      <WorkflowControl />
      <Button variant="outline" size="sm" className="h-7 text-muted-foreground" title="Command palette" onClick={togglePalette}>
        <Search />
        <kbd className="font-mono text-[11px]">⌘K</kbd>
      </Button>
    </header>
  );
}
