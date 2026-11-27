import { eventTypes, type Client, type L8Event, type Op, type Result } from "./types";

export interface DaemonInfo {
  url: string;
  token: string;
}

/** Returns a Client that talks to a running layer8d over HTTP and SSE. */
export function httpClient({ url, token }: DaemonInfo): Client {
  const base = url.replace(/\/$/, "");
  const headers = { Authorization: `Bearer ${token}`, "Content-Type": "application/json" };
  const listeners = new Set<(e: L8Event) => void>();
  let source: EventSource | null = null;

  async function call<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(base + path, { ...init, headers });
    const body = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(body.error ?? `${res.status} ${res.statusText}`);
    return body as T;
  }

  function open() {
    source = new EventSource(`${base}/api/events?token=${encodeURIComponent(token)}`);
    for (const type of eventTypes) {
      source.addEventListener(type, (m) => {
        const e = { type, data: JSON.parse((m as MessageEvent).data) } as L8Event;
        listeners.forEach((l) => l(e));
      });
    }
  }

  return {
    mode: "daemon",
    label: new URL(base).host,
    getNode: (addr, depth = 1) =>
      call(`/api/node?addr=${encodeURIComponent(addr)}&depth=${depth}`),
    doOps: async (ops: Op[]) =>
      (await call<{ results: Result[] }>("/api/do", { method: "POST", body: JSON.stringify({ ops }) })).results,
    subscribe(listener) {
      listeners.add(listener);
      if (!source) open();
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) {
          source?.close();
          source = null;
        }
      };
    },
  };
}
