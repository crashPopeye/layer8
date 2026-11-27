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
import { setFocus, taskOf, useFocus } from "@/lib/focus";
import { useClient, useNode } from "@/lib/react";
import { newSession, pickSession } from "@/panels/session/target";
import { pickFolder } from "@/lib/tauri";
import type { Action, Addr } from "@/lib/types";
import { cn } from "@/lib/utils";
import { liveStatus, rows, walk, type TreeItem } from "./tree";
import { useTree } from "./use-tree";

const dots: Record<string, string> = {
  active: "bg-primary",
  running: "bg-primary shadow-[0_0_0_3px] shadow-primary/20",
  waiting: "bg-wait",
  queued: "border border-wait bg-transparent",
  starting: "bg-primary",
  idle: "bg-muted-foreground",
  failed: "bg-bad",
  done: "bg-ok",
  archived: "border border-muted-foreground bg-transparent",
};

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
  const status = liveStatus(item);
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
            <span className={cn("size-[7px] shrink-0 rounded-full bg-muted-foreground", dots[status ?? ""])} />
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
            ) : status === "waiting" ? (
              <span className="text-wait">approve</span>
            ) : !open && item.children.length ? (
              item.children.length
            ) : status !== "active" ? (
              status
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
      <Sessions tree={tree} />
    </div>
  );
}

/** One session of the focused task: status, title, harness, model and cost. */
function SessionRow({ addr, task }: { addr: Addr; task: Addr }) {
  const { node } = useNode(addr, 0);
  const focus = useFocus();
  const f = node?.fields ?? {};
  const cost = Number(f.costUsd ?? 0);
  return (
    <button
      onClick={() => pickSession(addr, task)}
      className={cn("flex w-full items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-muted", focus === addr && "bg-muted")}
    >
      <span className={cn("size-[7px] shrink-0 rounded-full bg-muted-foreground", dots[String(f.status ?? "")])} />
      <span className="truncate">{node?.title ?? addr}</span>
      <span className="ml-auto shrink-0 font-mono text-[11px] text-muted-foreground">
        {[f.harness, f.model, cost > 0 && `$${cost.toFixed(2)}`].filter(Boolean).join(" · ")}
      </span>
    </button>
  );
}

/** The sessions of the focused task, with a button for a new one. */
function Sessions({ tree }: { tree: TreeItem[] }) {
  const task = taskOf(useFocus());
  const item = task ? walk(tree).find((i) => i.addr === task) : undefined;
  if (!task || !item) return null;
  return (
    <div className="mt-auto border-t pt-2">
      <div className="flex items-center gap-1 px-1.5 text-[11px] text-muted-foreground">
        <span className="flex-1 truncate">Sessions in {item.title}</span>
        <Button variant="ghost" size="icon-xs" title="New session (⌘T)" onClick={newSession}>
          <Plus />
        </Button>
      </div>
      {item.sessions.map((s) => (
        <SessionRow key={s.addr} addr={s.addr} task={task} />
      ))}
      {item.sessions.length === 0 && <p className="px-2 text-muted-foreground">No sessions yet.</p>}
    </div>
  );
}
