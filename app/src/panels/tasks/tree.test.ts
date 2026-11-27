import { expect, it, vi } from "vitest";
import { demoSeed, mockClient } from "@/lib/mock";
import { loadTree, rows } from "./tree";

it("reads projects, tasks and sub-tasks at every depth, leaving artifacts out", async () => {
  const client = mockClient([
    ...demoSeed,
    { target: "task:eng-482/pr2-middleware", action: "create-subtask", args: { name: "fixups" } },
    { target: "task:eng-482/pr2-middleware/fixups", action: "create-subtask", args: { name: "deeper" } },
  ]);
  const get = vi.fn(client.getNode);
  const tree = await loadTree(get);

  const titles = (items: typeof tree): unknown[] =>
    items.map((i) => (i.children.length ? [i.title, titles(i.children)] : i.title));
  expect(titles(tree)).toEqual([
    [
      "api-gateway",
      [
        "ENG-470 Retry webhooks",
        ["ENG-482 Rate-limit uploads", ["pr1-store", ["pr2-middleware", [["fixups", ["deeper"]]]], "pr3-docs"]],
      ],
    ],
    ["web-app", ["ENG-501 Upload progress UI"]],
  ]);
  expect(tree[0].children[1].children[0]).toMatchObject({ addr: "task:eng-482/pr1-store", kind: "task", status: "done" });
  expect(get.mock.calls.map(([addr]) => addr).sort()).toEqual([
    "app",
    "task:eng-470",
    "task:eng-482",
    "task:eng-482/pr2-middleware/fixups",
    "task:eng-501",
  ]);
});

it("flattens to rows, honouring collapsed nodes and hiding archived tasks unless asked", async () => {
  const client = mockClient([...demoSeed, { target: "task:eng-470", action: "archive" }]);
  const tree = await loadTree(client.getNode);
  const show = (r: ReturnType<typeof rows>) => r.map(({ item, depth }) => `${"  ".repeat(depth)}${item.title}`);

  expect(show(rows(tree, new Set(["task:eng-482"]), false))).toEqual([
    "api-gateway",
    "  ENG-482 Rate-limit uploads",
    "web-app",
    "  ENG-501 Upload progress UI",
  ]);
  expect(show(rows(tree, new Set(["project:web-app"]), true))).toEqual([
    "api-gateway",
    "  ENG-470 Retry webhooks",
    "  ENG-482 Rate-limit uploads",
    "    pr1-store",
    "    pr2-middleware",
    "    pr3-docs",
    "web-app",
  ]);
});
