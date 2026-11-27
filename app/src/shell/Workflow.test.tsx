import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { setFocus } from "@/lib/focus";
import { ClientProvider } from "@/lib/react";
import type { Client, Node, Op } from "@/lib/types";
import { WorkflowControl } from "./Workflow";

const app: Node = {
  addr: "app",
  kind: "app",
  title: "layer8",
  children: [
    { addr: "project:api", kind: "project", title: "api" },
    { addr: "workflow:feature", kind: "workflow", title: "feature" },
    { addr: "workflow:broken", kind: "workflow", title: "broken", status: "invalid" },
    { addr: "workflow:quick-fix", kind: "workflow", title: "quick-fix" },
  ],
  actions: [],
};

function fakeClient(task: Node) {
  const ops: Op[] = [];
  const client: Client = {
    mode: "mock",
    label: "test",
    getNode: async (addr) => (addr === "app" ? app : task),
    getJSON: async () => {
      throw new Error("unused");
    },
    raw: async () => {
      throw new Error("unused");
    },
    doOps: async (batch) => {
      ops.push(...batch);
      return batch.map((op) => ({ ok: true, addr: op.target }));
    },
    subscribe: () => () => {},
  };
  return { client, ops };
}

function show(task: Node) {
  const { client, ops } = fakeClient(task);
  act(() => setFocus(task.addr));
  render(
    <ClientProvider client={client}>
      <WorkflowControl />
    </ClientProvider>,
  );
  return ops;
}

afterEach(() => {
  cleanup();
  act(() => setFocus("app"));
});

it("lists the valid workflows and runs the chosen one on the focused task, with autopilot", async () => {
  const ops = show({
    addr: "task:feat",
    kind: "task",
    title: "feat",
    fields: { status: "active" },
    children: [],
    actions: [{ name: "run-workflow", help: "" }],
  });
  const select = await screen.findByLabelText("Run workflow");
  await waitFor(() =>
    expect([...select.querySelectorAll("option")].map((o) => o.textContent)).toEqual(["Workflow…", "feature", "quick-fix"]),
  );

  fireEvent.click(screen.getByTitle(/Autopilot/));
  fireEvent.change(select, { target: { value: "quick-fix" } });

  await waitFor(() =>
    expect(ops).toEqual([{ target: "task:feat", action: "run-workflow", args: { workflow: "quick-fix", autopilot: true } }]),
  );
});

it("offers the waiting step and starts it", async () => {
  const ops = show({
    addr: "task:feat",
    kind: "task",
    title: "feat",
    fields: { workflow: { name: "feature", task: "task:feat", step: "research", status: "waiting", autopilot: false, next: "plan" } },
    children: [],
    actions: [{ name: "next-step", help: "" }],
  });
  fireEvent.click(await screen.findByText("Next: plan ▸"));
  await waitFor(() => expect(ops).toEqual([{ target: "task:feat", action: "next-step" }]));
  expect(screen.queryByLabelText("Run workflow")).toBeNull();
  expect(screen.getByText("feature · research")).toBeTruthy();
});

it("shows nothing outside a task", () => {
  const { client } = fakeClient(app);
  const { container } = render(
    <ClientProvider client={client}>
      <WorkflowControl />
    </ClientProvider>,
  );
  expect(container.innerHTML).toBe("");
});
