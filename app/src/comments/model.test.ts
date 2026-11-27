import { describe, expect, it } from "vitest";
import { covers, lineLabel, liveSession, locate, type Comment } from "./model";

const plan = `# Plan: rate-limit uploads

We use a **token bucket** per [tenant](https://example.com/t),
refilled every second.

1. Add the middleware
2. Wire the \`limits\` config

| Limit | Value |
|---|---|
| burst | 20 |

We use a token bucket again here.
`;

describe("locate", () => {
  it("maps code selections line for line", () => {
    const src = "package main\n\nfunc main() {\n\thttp.Serve(\":8080\")\n}\n";
    expect(locate(src, "func main() {\n    http.Serve")).toEqual({ line: 3, lineEnd: 4 });
  });

  it("maps text selected in rendered markdown back past emphasis, links and line breaks", () => {
    expect(locate(plan, "token bucket per tenant, refilled every")).toEqual({ line: 3, lineEnd: 4 });
    expect(locate(plan, "Plan: rate-limit")).toEqual({ line: 1, lineEnd: 1 });
  });

  it("maps list items and table cells without their markers", () => {
    expect(locate(plan, "Add the middleware\nWire the limits config")).toEqual({ line: 6, lineEnd: 7 });
    expect(locate(plan, "burst\t20")).toEqual({ line: 11, lineEnd: 11 });
  });

  it("maps text selected in rendered html past its tags", () => {
    const html = "<section>\n  <h3>Upload limits</h3>\n  <p>Burst of <b>20</b> files</p>\n</section>";
    expect(locate(html, "Upload limits\nBurst of 20 files")).toEqual({ line: 2, lineEnd: 3 });
  });

  it("picks the match closest to the hinted line", () => {
    expect(locate(plan, "token bucket")).toEqual({ line: 3, lineEnd: 3 });
    expect(locate(plan, "token bucket", 12)).toEqual({ line: 13, lineEnd: 13 });
  });

  it("returns null for text the source does not hold", () => {
    expect(locate(plan, "leaky bucket")).toBeNull();
    expect(locate(plan, "  \n ")).toBeNull();
  });
});

const comment = (c: Partial<Comment>) => ({ line: 0, ...c }) as Comment;

it("covers a line range, and a single line without lineEnd", () => {
  expect(covers(comment({ line: 3, lineEnd: 5 }), 5)).toBe(true);
  expect(covers(comment({ line: 3, lineEnd: 5 }), 6)).toBe(false);
  expect(covers(comment({ line: 3 }), 3)).toBe(true);
  expect(covers(comment({}), 0)).toBe(false);
  expect(lineLabel({ line: 3, lineEnd: 5 })).toBe("L3-5");
  expect(lineLabel({ line: 3, lineEnd: 3 })).toBe("L3");
});

it("sends to the last session that is neither done nor failed", () => {
  const s = (addr: string, status: string) => ({ addr, kind: "session", title: addr, status });
  expect(liveSession([s("session:a", "idle"), s("session:b", "running"), s("session:c", "done")])?.addr).toBe("session:b");
  expect(liveSession([s("session:c", "failed"), { addr: "task:x/y", kind: "task", title: "y" }])).toBeUndefined();
});
