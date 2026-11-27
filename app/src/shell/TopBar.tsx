import { isTauri } from "@tauri-apps/api/core";
import { Moon, Search, Sun } from "lucide-react";
import { Fragment, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { openPanel } from "@/dock/Dock";
import { panels } from "@/dock/registry";
import { crumbTrail, type Crumb } from "@/lib/crumbs";
import { crumbs, setFocus, useFocus } from "@/lib/focus";
import { useClient } from "@/lib/react";
import { setTheme, storedTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

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

/** The window's title bar: breadcrumb of the focused node, panel launchers, theme switch and ⌘K. */
export function TopBar() {
  const focus = useFocus();
  const [theme, setThemeState] = useState(storedTheme);
  const toggleTheme = () => {
    const next = theme === "dark" ? "light" : "dark";
    setTheme(next);
    setThemeState(next);
  };
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
      <div data-tauri-drag-region className="flex-1 self-stretch" />
      {panels().map(({ id, title, icon: Icon }) => (
        <Button key={id} variant="ghost" size="icon-sm" title={title} onClick={() => openPanel(id)}>
          <Icon />
        </Button>
      ))}
      <Button variant="ghost" size="icon-sm" title="Switch theme" onClick={toggleTheme}>
        {theme === "dark" ? <Sun /> : <Moon />}
      </Button>
      <Button variant="outline" size="sm" className="h-7 text-muted-foreground" title="Command palette">
        <Search />
        <kbd className="font-mono text-[11px]">⌘K</kbd>
      </Button>
    </header>
  );
}
