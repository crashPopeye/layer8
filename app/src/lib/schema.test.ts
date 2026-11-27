import { expect, it } from "vitest";
import { argsFrom, fieldsOf } from "./schema";

const createTask = {
  type: "object",
  required: ["title"],
  properties: {
    slug: { type: "string", description: "Short id" },
    title: { type: "string" },
    target: { enum: ["terminal", "editor"] },
    draft: { type: "boolean" },
    count: { type: "integer" },
    labels: { type: "array", items: { type: "string" } },
  },
};

it("lists fields with required ones first and a kind per schema type", () => {
  expect(fieldsOf(createTask)).toEqual([
    { name: "title", kind: "text", required: true, description: undefined, options: undefined },
    { name: "slug", kind: "text", required: false, description: "Short id", options: undefined },
    { name: "target", kind: "enum", required: false, description: undefined, options: ["terminal", "editor"] },
    { name: "draft", kind: "boolean", required: false, description: undefined, options: undefined },
    { name: "count", kind: "number", required: false, description: undefined, options: undefined },
    { name: "labels", kind: "json", required: false, description: undefined, options: undefined },
  ]);
  expect(fieldsOf(undefined)).toEqual([]);
});

it("builds args, dropping empty optional fields and typing the rest", () => {
  const fields = fieldsOf(createTask);
  expect(
    argsFrom(fields, { title: "Rate-limit uploads", slug: " ", target: "editor", draft: true, count: "3", labels: '["a"]' }),
  ).toEqual({
    args: { title: "Rate-limit uploads", target: "editor", draft: true, count: 3, labels: ["a"] },
  });
});

it("reports the first problem", () => {
  const fields = fieldsOf(createTask);
  expect(argsFrom(fields, {})).toEqual({ error: "title is required" });
  expect(argsFrom(fields, { title: "x", count: "many" })).toEqual({ error: "count must be a number" });
  expect(argsFrom(fields, { title: "x", labels: "[" })).toEqual({ error: "labels must be JSON" });
});
