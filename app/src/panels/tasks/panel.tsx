import { Archive, ChevronDown, ChevronRight, FolderGit2, Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { openAction } from "@/components/action-form";
import { Button } from "@/components/ui/button";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { setFocus, useFocus } from "@/lib/focus";
import { useClient } from "@/lib/react";
import { pickFolder } from "@/lib/tauri";
import type { Action, Addr } from "@/lib/types";
import { cn } from "@/lib/utils";
import { loadTree, rows, type TreeItem } from "./tree";

const dots: Record<string, string> = {
  active: "bg-primary",
  running: "bg-primary shadow-[0_0_0_3px] shadow-primary/20",
  waiting: "bg-wait",
  done: "bg-ok",
  archived: "border border-muted-foreground bg-transparent",
};

/** Reads the project → task → sub-task tree and reads it again when a project or task changes. */
function useTree() {
  const client = useClient();
  const [state, setState] = useState<{ tree: TreeItem[]; error: Error | null }>({ tree: [], error: null });
  useEffect(() => {
    let latest = 0;
    const load = () => {
      const n = ++latest;
      loadTree(client.getNode).then(
        (tree) => n === latest && setState({ tree, error: null }),
        (error: Error) => n === latest && setState((s) => ({ ...s, error })),
      );
    };
    load();
    const off = client.subscribe((e) => {
      if (e.type === "node" && e.data.addrs.some((a) => a === "app" || a.startsWith("project:") || a.startsWith("task:"))) load();
    });
    return () => {
      latest = -1;
      off();
    };
  }, [client]);
  return state;
}

/** The actions a node offers right now, read when its menu opens. */
function NodeActions({ addr }: { addr: Addr }) {
  const client = useClient();
  const [actions, setActions] = useState<Action[] | null>(null);
  useEffect(() => {
    client.getNode(addr, 0).then(
      (n) => setActions(n.actions),
      () => setActions([]),
    );
  }, [client, addr]);
  return (
    <>
      <ContextMenuItem onSelect={() => setFocus(addr)}>Focus</ContextMenuItem>
      <ContextMenuSeparator />
      {actions === null && <ContextMenuItem disabled>Loading…</ContextMenuItem>}
      {actions?.length === 0 && <ContextMenuItem disabled>No actions</ContextMenuItem>}
      {actions?.map((a) => (
        <ContextMenuItem key={a.name} title={a.help} onSelect={() => openAction(addr, a.name)}>
          {a.name}
        </ContextMenuItem>
      ))}
    </>
  );
}

function TreeRow({ item, depth, open, onToggle }: { item: TreeItem; depth: number; open: boolean; onToggle: () => void }) {
  const focus = useFocus();
  const project = item.kind === "project";
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <div
          role="treeitem"
          aria-selected={focus === item.addr}
          onClick={() => setFocus(item.addr)}
          style={{ paddingLeft: 4 + Math.max(0, depth - 1) * 14 }}
          className={cn(
            "group flex cursor-default items-center gap-1.5 rounded-md py-1 pr-1.5 hover:bg-muted",
            project && "mt-2 text-[11px] text-muted-foreground",
            focus === item.addr && "bg-muted text-foreground",
            item.status === "archived" && "opacity-60",
          )}
        >
          <button
            aria-label={open ? "Collapse" : "Expand"}
            className={cn("grid size-4 place-items-center text-muted-foreground", !item.children.length && "invisible")}
            onClick={(e) => {
              e.stopPropagation();
              onToggle();
            }}
          >
            <Chevron className="size-3" />
          </button>
          {project ? (
            <FolderGit2 className="size-3.5" />
          ) : (
            <span className={cn("size-[7px] shrink-0 rounded-full bg-muted-foreground", dots[item.status ?? ""])} />
          )}
          <span className="truncate">{item.title}</span>
          <span className="ml-auto font-mono text-[11px] text-muted-foreground">
            {project ? (
              <Button
                variant="ghost"
                size="icon-xs"
                title="New task"
                className="opacity-0 group-hover:opacity-100"
                onClick={(e) => {
                  e.stopPropagation();
                  openAction(item.addr, "create-task");
                }}
              >
                <Plus />
              </Button>
            ) : !open && item.children.length ? (
              item.children.length
            ) : item.status !== "active" ? (
              item.status
            ) : null}
          </span>
        </div>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <NodeActions addr={item.addr} />
      </ContextMenuContent>
    </ContextMenu>
  );
}

/** Projects, tasks and sub-tasks with their status; click to focus, right-click for every action the node offers. */
export function TasksPanel() {
  const client = useClient();
  const { tree, error } = useTree();
  const [collapsed, setCollapsed] = useState<Set<Addr>>(new Set());
  const [archived, setArchived] = useState(false);
  const [message, setMessage] = useState<string | null>(null);

  const toggle = (addr: Addr) =>
    setCollapsed((old) => {
      const next = new Set(old);
      if (!next.delete(addr)) next.add(addr);
      return next;
    });

  async function addProject() {
    const path = await pickFolder();
    if (!path) return;
    const [result] = await client.doOps([{ target: "app", action: "add-project", args: { path } }]);
    setMessage(result.ok ? null : (result.message ?? "add-project failed"));
    if (result.ok) setFocus(result.addr);
  }

  return (
    <div className="flex h-full flex-col overflow-auto p-2" role="tree">
      <div className="flex items-center gap-1 px-1.5 text-[11px] text-muted-foreground">
        <span className="flex-1">Projects</span>
        <Button
          variant="ghost"
          size="icon-xs"
          title={archived ? "Hide archived tasks" : "Show archived tasks"}
          className={cn(archived && "text-foreground")}
          onClick={() => setArchived(!archived)}
        >
          <Archive />
        </Button>
        <Button variant="ghost" size="icon-xs" title="Add project" onClick={addProject}>
          <Plus />
        </Button>
      </div>
      {error && <p className="px-2 text-bad">{error.message}</p>}
      {message && <p className="px-2 text-bad">{message}</p>}
      {rows(tree, collapsed, archived).map(({ item, depth }) => (
        <TreeRow key={item.addr} item={item} depth={depth} open={!collapsed.has(item.addr)} onToggle={() => toggle(item.addr)} />
      ))}
      {tree.length === 0 && !error && <p className="px-2 text-muted-foreground">No projects yet. Add a git repository with +.</p>}
    </div>
  );
}
