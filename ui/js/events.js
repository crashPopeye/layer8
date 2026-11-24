import { useEffect, useRef } from "/vendor/preact-htm.js";

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
