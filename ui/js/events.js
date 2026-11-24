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

/** useActiveSessions lists the sessions of every active task, refetched (at most every 200 ms) as sessions and tasks change. */
export function useActiveSessions() {
  const [sessions, setSessions] = useState([]);
  const timer = useRef(0);
  const load = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(() => get("/api/sessions").then(setSessions, () => {}), 200);
  }, []);
  useEffect(() => {
    load();
    return () => clearTimeout(timer.current);
  }, [load]);
  useEvents((e) => ["open", "session", "attention", "task"].includes(e.kind) && load());
  return sessions;
}
