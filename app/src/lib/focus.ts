import { useSyncExternalStore } from "react";
import type { Addr } from "./types";

let focus: Addr = "app";
const subscribers = new Set<() => void>();

/** Sets the node every panel follows. */
export function setFocus(addr: Addr) {
  if (addr === focus) return;
  focus = addr;
  subscribers.forEach((s) => s());
}

/** Returns the focused node address. */
export function getFocus(): Addr {
  return focus;
}

/** Returns the focused node address and re-renders when it changes. */
export function useFocus(): Addr {
  return useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    getFocus,
  );
}

/** Splits an address into breadcrumb segments, e.g. `task:eng-482/pr2` → ["eng-482", "pr2"]. */
export function crumbs(addr: Addr): string[] {
  const i = addr.indexOf(":");
  return i < 0 ? [] : addr.slice(i + 1).split("/").filter(Boolean);
}
