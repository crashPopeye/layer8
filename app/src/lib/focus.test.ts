import { expect, it } from "vitest";
import { vi } from "vitest";
import { crumbs, getFocus, taskOf } from "./focus";
import { mockClient } from "./mock";
import { applyUi } from "./ui";

it("splits addresses into breadcrumb segments", () => {
  expect(crumbs("app")).toEqual([]);
  expect(crumbs("project:api-gateway")).toEqual(["api-gateway"]);
  expect(crumbs("task:eng-482/pr2-middleware")).toEqual(["eng-482", "pr2-middleware"]);
});

it("moves focus on a ui focus command", () => {
  const client = mockClient();
  applyUi({ verb: "focus", addr: "task:eng-482" }, client);
  expect(getFocus()).toBe("task:eng-482");
});

it("focuses a session together with its task", async () => {
  const client = mockClient([
    { target: "app", action: "add-project", args: { path: "/src/api" } },
    { target: "project:api", action: "create-task", args: { title: "T", slug: "t" } },
    { target: "task:t", action: "start-session", args: { title: "S" } },
  ]);
  const [session] = (await client.getNode("task:t")).children;
  applyUi({ verb: "focus", addr: session.addr }, client);
  await vi.waitFor(() => expect(getFocus()).toBe(session.addr));
  expect(taskOf(session.addr)).toBe("task:t");
});
