import { useClient } from "@/lib/react";
import { sessionCounts } from "@/panels/tasks/tree";
import { useTree } from "@/panels/tasks/use-tree";
import { cn } from "@/lib/utils";
import { UsageMonitor } from "./UsageMonitor";

const shortcuts = [
  ["⌘K", "everything"],
  ["⌘N", "new task"],
  ["⌘⇧N", "sub-task"],
  ["⌘T", "new session"],
  ["⌘1–9", "panels"],
  ["A/D", "allow · deny"],
];

/** The window's status bar: shortcuts, session counts and the daemon connection. */
export function Footer() {
  const client = useClient();
  const { running, waiting, queued } = sessionCounts(useTree().tree);
  return (
    <footer className="flex h-[26px] shrink-0 items-center gap-3.5 border-t px-3 text-[11px] text-muted-foreground">
      {shortcuts.map(([key, what]) => (
        <span key={key}>
          <kbd className="rounded border border-b-2 px-1 font-mono">{key}</kbd> {what}
        </span>
      ))}
      <span className="flex-1" />
      <UsageMonitor />
      <span className={cn(waiting > 0 && "text-wait")}>
        {running} running · {waiting} waiting · {queued} queued
      </span>
      <span className="flex items-center gap-1.5">
        <span className={cn("size-1.5 rounded-full", client.mode === "daemon" ? "bg-ok" : "bg-wait")} />
        {client.mode === "daemon" ? `layer8d ${client.label}` : "mock mode: no daemon"}
      </span>
    </footer>
  );
}
