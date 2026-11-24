import { useEffect, useRef, useState, useCallback } from "/vendor/preact-htm.js";
import { get } from "./api.js";

const listeners = new Set();
let source = null;

function connect() {
  source = new EventSource("/api/events");
  source.onopen = () => listeners.forEach((fn) => fn({ kind: "open" }));
  source.onmessage = (e) => {
    const event = JSON.parse(e.data);
    listeners.forEach((fn) => fn(event));
  };
}

/** useEvents calls onEvent with every daemon event, plus {kind:"open"} on each (re)connect. */
export function useEvents(onEvent) {
  const handler = useRef(onEvent);
  handler.current = onEvent;
  useEffect(() => {
    const fn = (event) => handler.current(event);
    listeners.add(fn);
    if (!source) connect();
    return () => listeners.delete(fn);
  }, []);
}

/** useSessions lists a task's sessions and refetches when one of them changes. */
export function useSessions(taskId) {
  const [sessions, setSessions] = useState([]);
  const load = useCallback(() => get(`/api/tasks/${taskId}/sessions`).then(setSessions, () => {}), [taskId]);
  useEffect(() => {
    setSessions([]);
    load();
  }, [load]);
  useEvents((e) => (e.kind === "open" || (e.kind === "session" && e.taskId === taskId)) && load());
  return sessions;
}
