import { expect, it } from "vitest";
import { crumbs, getFocus } from "./focus";
import { applyUi } from "./ui";

it("splits addresses into breadcrumb segments", () => {
  expect(crumbs("app")).toEqual([]);
  expect(crumbs("project:api-gateway")).toEqual(["api-gateway"]);
  expect(crumbs("task:eng-482/pr2-middleware")).toEqual(["eng-482", "pr2-middleware"]);
});

it("moves focus on a ui focus command", () => {
  applyUi({ verb: "focus", addr: "task:eng-482" });
  expect(getFocus()).toBe("task:eng-482");
});
