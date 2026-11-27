import { afterEach, describe, expect, it, vi } from "vitest";
import { httpClient } from "./client";
import type { L8Event } from "./types";

class FakeEventSource {
  static last: FakeEventSource;
  handlers = new Map<string, (m: MessageEvent) => void>();
  closed = false;
  constructor(public url: string) {
    FakeEventSource.last = this;
  }
  addEventListener(type: string, h: (m: MessageEvent) => void) {
    this.handlers.set(type, h);
  }
  close() {
    this.closed = true;
  }
  send(type: string, data: unknown) {
    this.handlers.get(type)?.({ data: JSON.stringify(data) } as MessageEvent);
  }
}

function fakeFetch(status: number, body: unknown) {
  const fn = vi.fn(async (_url: string, _init?: RequestInit) => new Response(JSON.stringify(body), { status }));
  vi.stubGlobal("fetch", fn);
  return fn;
}

const info = { url: "http://127.0.0.1:7979/", token: "s3cret" };

afterEach(() => vi.unstubAllGlobals());

describe("httpClient", () => {
  it("reads a node with the token and depth", async () => {
    const node = { addr: "project:api", kind: "project", title: "api", fields: {}, children: [], actions: [] };
    const fetch = fakeFetch(200, node);
    expect(await httpClient(info).getNode("project:api", 2)).toEqual(node);
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("http://127.0.0.1:7979/api/node?addr=project%3Aapi&depth=2");
    expect((init!.headers as Record<string, string>).Authorization).toBe("Bearer s3cret");
  });

  it("posts ops and returns their results", async () => {
    const fetch = fakeFetch(200, { results: [{ ok: true, addr: "project:api" }] });
    const ops = [{ target: "app", action: "add-project", args: { path: "/src/api" } }];
    expect(await httpClient(info).doOps(ops)).toEqual([{ ok: true, addr: "project:api" }]);
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe("http://127.0.0.1:7979/api/do");
    expect(init!.method).toBe("POST");
    expect(JSON.parse(init!.body as string)).toEqual({ ops });
  });

  it("throws the daemon's error message", async () => {
    fakeFetch(404, { error: "no node at task:nope" });
    await expect(httpClient(info).getNode("task:nope")).rejects.toThrow("no node at task:nope");
  });

  it("delivers typed SSE events and closes the stream after the last listener", () => {
    vi.stubGlobal("EventSource", FakeEventSource);
    const client = httpClient(info);
    const seen: L8Event[] = [];
    const off = client.subscribe((e) => seen.push(e));
    const es = FakeEventSource.last;
    expect(es.url).toBe("http://127.0.0.1:7979/api/events?token=s3cret");

    es.send("node", { addrs: ["app"] });
    es.send("ui", { verb: "open-panel", panel: "changes" });
    expect(seen).toEqual([
      { type: "node", data: { addrs: ["app"] } },
      { type: "ui", data: { verb: "open-panel", panel: "changes" } },
    ]);

    off();
    expect(es.closed).toBe(true);
  });
});
