import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { reduceAll } from "@/panels/session/model";
import { mockClient } from "./mock";
import type { SessionEvent } from "./types";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

const seed = [
  { target: "app", action: "add-project", args: { path: "/src/api" } },
  { target: "project:api", action: "create-task", args: { title: "T", slug: "t" } },
];

it("plays a turn that streams, nests a subagent, edits and waits for approval, then finishes once allowed", async () => {
  const client = mockClient(seed);
  const live: SessionEvent[] = [];
  client.subscribe((e) => e.type === "session" && live.push(...e.data.events));

  const [r] = await client.doOps([{ target: "task:t", action: "start-session", args: { prompt: "go", model: "opus" } }]);
  expect(r).toMatchObject({ ok: true, addr: expect.stringMatching(/^session:/) });
  expect((await client.getNode("task:t")).children).toContainEqual({ addr: r.addr, kind: "session", title: "go", status: "running" });
  await vi.advanceTimersByTimeAsync(10_000);

  const waiting = await client.getNode(r.addr, 0);
  expect(waiting.fields).toMatchObject({ status: "waiting", model: "opus", approvals: [{ id: 1, tool: "Bash" }] });
  expect(waiting.actions.map((a) => a.name)).toEqual(["say", "interrupt", "allow", "deny", "close", "set", "fork"]);
  expect(live.some((e) => e.partial)).toBe(true);
  const t = reduceAll(live);
  expect(t.items.map((i) => (i.kind === "tool" ? `${i.name}:${i.children.length}` : i.kind))).toEqual([
    "user",
    "thinking",
    "text",
    "Task:2",
    "Edit:0",
    "Bash:0",
    "approval",
  ]);

  const id8 = r.addr.slice("session:".length);
  const { events } = await client.getJSON<{ events: SessionEvent[] }>(`/api/sessions/${id8}/events?after=0`);
  expect(events.every((e) => e.id && !e.partial)).toBe(true);
  expect(reduceAll(events).items.map((i) => i.kind)).toEqual(t.items.map((i) => i.kind));

  await client.doOps([{ target: r.addr, action: "allow", args: { approval: 1 } }]);
  await vi.advanceTimersByTimeAsync(10_000);
  const done = reduceAll(live);
  expect(done.items.at(-1)).toMatchObject({ kind: "result", data: { subtype: "success", costUsd: 0.42 } });
  expect(done.items.find((i) => i.kind === "tool" && i.name === "Bash")).toMatchObject({ result: { isError: false } });
  expect((await client.getNode(r.addr, 0)).fields).toMatchObject({ status: "idle", approvals: [] });
});

it("queues a message sent mid-turn and plays it after the current one", async () => {
  const client = mockClient(seed);
  const [r] = await client.doOps([{ target: "task:t", action: "start-session", args: { prompt: "one" } }]);
  await client.doOps([{ target: r.addr, action: "say", args: { text: "two" } }]);
  await vi.advanceTimersByTimeAsync(10_000);
  await client.doOps([{ target: r.addr, action: "interrupt" }]);
  const { events } = await client.getJSON<{ events: SessionEvent[] }>(`/api/sessions/${r.addr.slice(8)}/events`);
  const t = reduceAll(events);
  expect(t.users).toBe(2);
  expect(t.results).toBe(1);
  expect((await client.getNode(r.addr, 0)).fields?.status).toBe("running");
});
