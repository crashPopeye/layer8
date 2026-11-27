import { expect, it } from "vitest";
import { commandItems, referenceItems } from "./composer";
import { apply, rank, trigger } from "./menu";

it("finds the / or @ token at the caret", () => {
  expect(trigger("/re", 3)).toEqual({ char: "/", query: "re", start: 0 });
  expect(trigger("/review now", 11)).toBeNull();
  expect(trigger("say /re", 7)).toBeNull();
  expect(trigger("look at @src/m", 14)).toEqual({ char: "@", query: "src/m", start: 8 });
  expect(trigger("@", 1)).toEqual({ char: "@", query: "", start: 0 });
  expect(trigger("mail a@b", 8)).toBeNull();
  expect(trigger("look at @src/m done", 19)).toBeNull();
});

const caps = {
  commands: [
    { name: "research", description: "Document the codebase", plugin: "layer8" },
    { name: "review", description: "Review the diff" },
    { name: "rpi:review-artifact-comments", description: "Apply comments", plugin: "rpi" },
    { name: "compact", description: "Summarise" },
  ],
};

it("ranks commands by fuzzy match, exact substrings first", () => {
  const items = commandItems(caps);
  expect(items[0]).toEqual({ label: "/research", detail: "layer8 · Document the codebase", insert: "/research " });
  expect(rank(items, "rev").map((i) => i.label)).toEqual(["/review", "/rpi:review-artifact-comments"]);
  expect(rank(items, "rac").map((i) => i.label)).toEqual(["/rpi:review-artifact-comments", "/research"]);
  expect(rank(items, "").map((i) => i.label)).toHaveLength(4);
});

it("replaces the token with the picked reference and moves the caret after it", () => {
  const refs = referenceItems("/home/tasks/eng-482", [{ addr: "artifact:eng-482/05-plan.md", kind: "artifact", title: "05-plan.md" }], ["task:eng-482/pr1"], ["go.mod"]);
  expect(refs.map((r) => r.insert)).toEqual(["@/home/tasks/eng-482/05-plan.md ", "@layer8://task/eng-482/pr1 ", "@go.mod "]);
  const text = "read @pla please";
  expect(apply(text, 9, trigger(text, 9)!, refs[0])).toEqual({ text: "read @/home/tasks/eng-482/05-plan.md  please", caret: 37 });
});
