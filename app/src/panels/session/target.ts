import { useSyncExternalStore } from "react";
import { openPanel } from "@/dock/Dock";
import { getFocus, setFocus, taskOf } from "@/lib/focus";
import type { Addr, Summary } from "@/lib/types";

let draft: Addr | null = null;
const subscribers = new Set<() => void>();
const notify = () => subscribers.forEach((s) => s());

/** Puts the session panel in new-session mode for the focused task. */
export function newSession() {
  const task = taskOf(getFocus());
  if (!task) return;
  draft = task;
  notify();
  setFocus(task);
  openPanel("session");
}

/** Shows a session, leaving new-session mode. */
export function pickSession(session: Addr, task: Addr) {
  draft = null;
  notify();
  setFocus(session, task);
  openPanel("session");
}

/** The task whose session panel is in new-session mode, if any. */
export function useDraft(): Addr | null {
  return useSyncExternalStore(
    (s) => {
      subscribers.add(s);
      return () => subscribers.delete(s);
    },
    () => draft,
  );
}

/** The session a task shows by default: its newest live one, else its newest. */
export function latest(sessions: Summary[]): Summary | undefined {
  return sessions.filter((s) => s.status !== "done").at(-1) ?? sessions.at(-1);
}
