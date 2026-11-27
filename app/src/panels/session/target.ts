import { useSyncExternalStore } from "react";
import { openPanel } from "@/dock/Dock";
import { getFocus, setFocus, taskOf, useFocus } from "@/lib/focus";
import type { Addr, Summary } from "@/lib/types";
import { walk } from "@/panels/tasks/tree";
import { useTree } from "@/panels/tasks/use-tree";

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

/** The focused task, its sessions and the one the session panel shows: the focused session, else the newest unless drafting a new one. */
export function useShownSession(): { task: Addr | null; session: Addr | null; sessions: Summary[] } {
  const focus = useFocus();
  const task = taskOf(focus);
  const draft = useDraft();
  const { tree } = useTree();
  const sessions = (task && walk(tree).find((i) => i.addr === task)?.sessions) || [];
  const session = !task ? null : focus.startsWith("session:") ? focus : draft === task ? null : (latest(sessions)?.addr ?? null);
  return { task, session, sessions };
}
