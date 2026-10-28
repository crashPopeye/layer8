import "./helpers.js";
import { test } from "node:test";
import assert from "node:assert/strict";
import type { AddressInfo } from "node:net";
import { loadConfig } from "../src/config.js";
import { Store } from "../src/db.js";
import { createApp } from "../src/server.js";
import { SessionManager } from "../src/sessions.js";
import { TaskService } from "../src/tasks.js";

test("API requires the token, a local Host and an allowed Origin", async (t) => {
  const store = new Store(":memory:");
  const cfg = loadConfig();
  const server = createApp({
    store,
    sessions: new SessionManager(store, () => 1),
    tasks: new TaskService(store, () => cfg),
    config: () => cfg,
    saveConfig: () => {},
    token: "secret",
  });
  await new Promise<void>((r) => server.listen(0, "127.0.0.1", r));
  t.after(() => server.close());
  const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;

  assert.equal((await fetch(`${base}/api/state`)).status, 401);
  const ok = await fetch(`${base}/api/state`, { headers: { Authorization: "Bearer secret" } });
  assert.equal(ok.status, 200);
  assert.deepEqual(Object.keys(await ok.json()), ["workspaces", "tasks", "sessions", "approvals"]);

  const evil = await fetch(`${base}/api/state`, {
    headers: { Authorization: "Bearer secret", Origin: "https://evil.example" },
  });
  assert.equal(evil.status, 403);

  const cfgRes = await fetch(`${base}/api/config`, { headers: { Authorization: "Bearer secret" } });
  const body = await cfgRes.json();
  assert.ok(body.linearApiKey === null || body.linearApiKey === "set", "key is never sent back");
});
