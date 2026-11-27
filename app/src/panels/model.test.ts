import { describe, expect, it } from "vitest";
import { diffPath, splitRows, type Line } from "./changes/model";
import { prOf, prView, type PR } from "./pr/model";

const ctx = (n: number, text: string): Line => ({ kind: "ctx", old: n, new: n, text });
const del = (old: number, text: string): Line => ({ kind: "del", old, text });
const add = (n: number, text: string): Line => ({ kind: "add", new: n, text });

describe("splitRows", () => {
  it("puts context on both sides and pairs deletions with the additions after them", () => {
    const a = ctx(1, "a");
    const [d2, d3, a2, z] = [del(2, "two"), del(3, "three"), add(2, "TWO"), ctx(4, "z")];
    expect(splitRows([a, d2, d3, a2, z])).toEqual([
      { left: a, right: a },
      { left: d2, right: a2 },
      { left: d3, right: undefined },
      { left: z, right: z },
    ]);
  });

  it("starts a new pair when a deletion follows additions", () => {
    const [a1, d1, a2] = [add(1, "x"), del(1, "y"), add(2, "z")];
    expect(splitRows([a1, d1, a2])).toEqual([
      { left: undefined, right: a1 },
      { left: d1, right: a2 },
    ]);
  });
});

it("diffPath escapes the task address", () => {
  expect(diffPath("task:eng-482/pr2", "uncommitted")).toBe("/api/diff?task=task%3Aeng-482%2Fpr2&mode=uncommitted");
});

const pr: PR = {
  number: 91,
  url: "https://github.com/o/api/pull/91",
  state: "open",
  draft: false,
  base: "main",
  mergeable: "conflicting",
  reviewDecision: "changes_requested",
  checksState: "failure",
  checks: [
    { name: "deploy", state: "success", url: "" },
    { name: "lint", state: "pending", url: "" },
    { name: "test", state: "failure", url: "", logTail: "error: boom" },
  ],
  unresolvedThreads: 1,
  threads: [{ path: "limit.go", line: 12, author: "rev", body: "use a const", url: "", outdated: false, replies: 0 }],
};

describe("prView", () => {
  it("labels an open PR with failing checks first and both fix buttons enabled", () => {
    const v = prView(pr);
    expect(v.state).toEqual({ text: "Open", tone: "ok" });
    expect(v.review).toEqual({ text: "Changes requested", tone: "bad" });
    expect(v.merge).toEqual({ text: "Conflicts", tone: "bad" });
    expect(v.checks.map((c) => c.name)).toEqual(["test", "lint", "deploy"]);
    expect(v.summary).toBe("1 failing · 1 running · 1 passed");
    expect([v.canFixCI, v.canAddress]).toEqual([true, true]);
  });

  it("hides mergeability once merged and has nothing to fix when green", () => {
    const v = prView({ ...pr, state: "merged", checksState: "success", checks: [], reviewDecision: "", unresolvedThreads: 0, threads: [] });
    expect(v.state.text).toBe("Merged");
    expect([v.review, v.merge, v.summary, v.canFixCI, v.canAddress]).toEqual([null, null, "No checks", false, false]);
  });

  it("calls an open draft a draft", () => {
    expect(prView({ ...pr, draft: true }).state).toEqual({ text: "Draft", tone: "muted" });
  });
});

it("prOf reads the task's pr field and treats null as none", () => {
  const node = { addr: "task:x", kind: "task", title: "x", children: [], actions: [] };
  expect(prOf({ ...node, fields: { pr } })).toBe(pr);
  expect(prOf({ ...node, fields: { pr: null } })).toBeNull();
  expect(prOf(null)).toBeNull();
});
