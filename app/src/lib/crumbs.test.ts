import { expect, it } from "vitest";
import { crumbTrail } from "./crumbs";
import { demoSeed, mockClient } from "./mock";

it("walks a sub-task, an artifact and a project up to their project with titles", async () => {
  const { getNode } = mockClient(demoSeed);
  expect(await crumbTrail(getNode, "task:eng-482/pr2-middleware")).toEqual([
    { addr: "project:api-gateway", title: "api-gateway" },
    { addr: "task:eng-482", title: "ENG-482 Rate-limit uploads" },
    { addr: "task:eng-482/pr2-middleware", title: "pr2-middleware" },
  ]);
  expect((await crumbTrail(getNode, "artifact:eng-482/05-plan.md")).map((c) => c.title)).toEqual([
    "api-gateway",
    "ENG-482 Rate-limit uploads",
    "05-plan.md",
  ]);
  expect(await crumbTrail(getNode, "app")).toEqual([]);
});
