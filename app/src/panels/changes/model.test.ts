import { expect, it } from "vitest";
import { anchorOf, rowAnchors, splitRows, type Line } from "./model";

const ctx: Line = { kind: "ctx", old: 4, new: 5, text: "a" };
const del: Line = { kind: "del", old: 5, text: "b" };
const add: Line = { kind: "add", new: 6, text: "c" };

it("anchors deleted lines in the base and the rest in the current file", () => {
  expect(anchorOf(del)).toEqual({ side: "old", line: 5 });
  expect(anchorOf(add)).toEqual({ side: "new", line: 6 });
  expect(anchorOf(ctx)).toEqual({ side: "new", line: 5 });
});

it("gives a context row one anchor and a changed row one per side", () => {
  const [same, changed] = splitRows([ctx, del, add]);
  expect(rowAnchors(same)).toEqual([{ side: "new", line: 5 }]);
  expect(rowAnchors(changed)).toEqual([
    { side: "old", line: 5 },
    { side: "new", line: 6 },
  ]);
});
