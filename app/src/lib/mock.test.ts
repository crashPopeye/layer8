import { expect, it } from "vitest";
import { mockClient } from "./mock";
import type { L8Event } from "./types";

it("adds a project, lists it under app and announces the change", async () => {
  const client = mockClient();
  const seen: L8Event[] = [];
  client.subscribe((e) => seen.push(e));

  const [r] = await client.doOps([{ target: "app", action: "add-project", args: { path: "/Users/alex/src/api-gateway/" } }]);

  expect(r).toEqual({ ok: true, addr: "project:api-gateway", message: "added project api-gateway" });
  expect((await client.getNode("app")).children).toEqual([
    { addr: "project:api-gateway", kind: "project", title: "api-gateway" },
  ]);
  expect((await client.getNode("project:api-gateway")).fields).toEqual({ path: "/Users/alex/src/api-gateway", defaultBranch: "main" });
  expect(seen).toEqual([{ type: "node", data: { addrs: ["app", "project:api-gateway"] } }]);
});

it("rejects duplicates, unknown actions and unknown nodes", async () => {
  const client = mockClient();
  const add = { target: "app", action: "add-project", args: { path: "/src/api" } };
  const [, dup, unknown] = await client.doOps([add, add, { target: "app", action: "explode" }]);
  expect(dup).toEqual({ ok: false, addr: "app", message: 'project "api" already exists' });
  expect(unknown).toEqual({ ok: false, addr: "app", message: 'app has no action "explode"' });
  await expect(client.getNode("project:nope")).rejects.toThrow("not found: project:nope");
});
