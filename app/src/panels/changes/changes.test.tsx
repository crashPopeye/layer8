import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Comment } from "@/comments/model";
import { setFocus } from "@/lib/focus";
import { ClientProvider } from "@/lib/react";
import type { Client, Node, Op } from "@/lib/types";
import { ChangesPanel } from ".";
import type { Diff } from "./model";

afterEach(cleanup);

const diff: Diff = {
  mode: "all",
  base: "main",
  added: 1,
  deleted: 1,
  files: [
    {
      path: "main.go",
      status: "modified",
      added: 1,
      deleted: 1,
      hunks: [
        {
          header: "@@ -1,2 +1,2 @@",
          oldStart: 1,
          newStart: 1,
          lines: [
            { kind: "ctx", old: 1, new: 1, text: "package main" },
            { kind: "del", old: 2, text: "func old() {}" },
            { kind: "add", new: 2, text: "func serve() {}" },
          ],
        },
      ],
    },
  ],
};

const comment: Comment = {
  addr: "comment:aaaa0001",
  target: "diff",
  task: "task:feat",
  path: "main.go",
  line: 2,
  lineEnd: 2,
  side: "new",
  body: "Name it Serve",
  status: "open",
  author: "user",
  createdAt: "2026-11-27T10:00:00Z",
};

function fakeClient(ops: Op[]): Client {
  const task: Node = {
    addr: "task:feat",
    kind: "task",
    title: "feat",
    fields: { status: "active", comments: [comment] },
    children: [{ addr: "session:9c41e0aa", kind: "session", title: "Implement", status: "idle" }],
    actions: [{ name: "add-comment", help: "" }, { name: "send-comments", help: "" }],
  };
  return {
    mode: "mock",
    label: "fake",
    getNode: async (addr) => (addr === "task:feat" ? task : { ...task, addr, kind: "comment", fields: {}, children: [] }),
    getJSON: async <T,>() => diff as T,
    doOps: vi.fn(async (list: Op[]) => (ops.push(...list), list.map((o) => ({ ok: true, addr: o.target })))),
    raw: async () => new Blob(),
    subscribe: () => () => {},
  };
}

it("shows diff comments under their line and adds one from a line number", async () => {
  const ops: Op[] = [];
  setFocus("task:feat");
  render(
    <ClientProvider client={fakeClient(ops)}>
      <ChangesPanel />
    </ClientProvider>,
  );
  expect(await screen.findByText("Name it Serve")).toBeTruthy();
  expect(screen.getAllByText("2").find((td) => td.className.includes("commented"))).toBeTruthy();

  fireEvent.click(await screen.findByRole("button", { name: /Send 1 comment to Implement/ }));
  expect(ops.at(-1)).toEqual({ target: "task:feat", action: "send-comments", args: { session: "session:9c41e0aa" } });

  const deleted = screen.getAllByTitle("Comment on this line").find((td) => td.textContent === "2" && td.parentElement?.textContent?.includes("old"))!;
  fireEvent.click(deleted);
  fireEvent.change(screen.getByPlaceholderText("Comment for the agent"), { target: { value: "Why remove it?" } });
  fireEvent.keyDown(screen.getByPlaceholderText("Comment for the agent"), { key: "Enter", metaKey: true });
  expect(ops.at(-1)).toEqual({
    target: "task:feat",
    action: "add-comment",
    args: { path: "main.go", side: "old", line: 2, lineEnd: 2, quote: "func old() {}", body: "Why remove it?" },
  });
});
