import { afterEach, expect, it, vi } from "vitest";
import { closeTab, crumbs, getFocus, getTabs, setFocus, taskOf } from "./focus";
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

afterEach(() => {
  getTabs().forEach((t) => closeTab(t.key));
  setFocus("app");
});

it("opens one tab per task, remembering where it was left", () => {
  setFocus("project:api");
  expect(getTabs()).toEqual([]);
  setFocus("task:eng-1");
  setFocus("session:9c41", "task:eng-1");
  setFocus("artifact:eng-2/plan.md");
  setFocus("session:orphan");
  expect(getTabs()).toEqual([
    { key: "task:eng-1", addr: "session:9c41" },
    { key: "task:eng-2", addr: "artifact:eng-2/plan.md" },
    { key: "session:orphan", addr: "session:orphan" },
  ]);
});

it("closing the focused tab focuses its neighbour, then the app", () => {
  setFocus("task:a");
  setFocus("task:b");
  setFocus("task:c");
  setFocus("task:b");
  closeTab("task:b");
  expect(getFocus()).toBe("task:c");
  closeTab("task:a");
  expect(getFocus()).toBe("task:c");
  closeTab("task:c");
  expect(getFocus()).toBe("app");
  expect(getTabs()).toEqual([]);
});
