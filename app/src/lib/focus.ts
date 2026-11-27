import { useSyncExternalStore } from "react";
import type { Addr } from "./types";

/** An open tab: a task, or a session whose task is unknown, and the address last focused in it. */
export interface Tab {
  key: Addr;
  addr: Addr;
}

let focus: Addr = "app";
let tabs: Tab[] = [];
const subscribers = new Set<() => void>();
const sessionTasks = new Map<Addr, Addr>();

function subscribe(s: () => void) {
  subscribers.add(s);
  return () => subscribers.delete(s);
}

function notify() {
  subscribers.forEach((s) => s());
}

/** Sets the node every panel follows, opening or updating its tab; a session comes with the task it runs in. */
export function setFocus(addr: Addr, task?: Addr) {
  if (task) sessionTasks.set(addr, task);
  const before = tabs;
  const key = tabOf(addr);
  if (key && !tabs.some((t) => t.key === key && t.addr === addr)) {
    tabs = tabs.some((t) => t.key === key) ? tabs.map((t) => (t.key === key ? { key, addr } : t)) : [...tabs, { key, addr }];
  }
  if (addr === focus && tabs === before) return;
  focus = addr;
  notify();
}

/** Returns the focused node address. */
export function getFocus(): Addr {
  return focus;
}

/** Returns the focused node address and re-renders when it changes. */
export function useFocus(): Addr {
  return useSyncExternalStore(subscribe, getFocus);
}

/** Returns the open tabs, oldest first. */
export function getTabs(): Tab[] {
  return tabs;
}

/** Returns the open tabs and re-renders when one opens, closes or moves to another address. */
export function useTabs(): Tab[] {
  return useSyncExternalStore(subscribe, getTabs);
}

/** Closes a tab; closing the focused one focuses its neighbour, or the app when it was the last. */
export function closeTab(key: Addr) {
  const at = tabs.findIndex((t) => t.key === key);
  if (at < 0) return;
  tabs = tabs.filter((t) => t.key !== key);
  if (tabOf(focus) === key) return setFocus(tabs[Math.min(at, tabs.length - 1)]?.addr ?? "app");
  notify();
}

/** Returns the tab key of addr: its task, the address itself for a session of unknown task, or null for nodes without tabs. */
export function tabOf(addr: Addr): Addr | null {
  return /^(task|session|artifact):/.test(addr) ? (taskOf(addr) ?? addr) : null;
}

/** Splits an address into breadcrumb segments, e.g. `task:eng-482/pr2` → ["eng-482", "pr2"]. */
export function crumbs(addr: Addr): string[] {
  const i = addr.indexOf(":");
  return i < 0 ? [] : addr.slice(i + 1).split("/").filter(Boolean);
}

/** Returns the task a node belongs to: the task itself, the root task of an artifact, or a focused session's task. */
export function taskOf(addr: Addr): Addr | null {
  if (addr.startsWith("task:")) return addr;
  if (addr.startsWith("session:")) return sessionTasks.get(addr) ?? null;
  if (addr.startsWith("artifact:")) return `task:${addr.slice("artifact:".length).split("/")[0]}`;
  return null;
}
