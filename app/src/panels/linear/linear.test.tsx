import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { ActionDialog, openAction } from "@/components/action-form";
import { getFocus, setFocus } from "@/lib/focus";
import { ClientProvider } from "@/lib/react";
import type { Client, Node, Op, Result } from "@/lib/types";
import { LinearPanel } from ".";
import { issueTitle, linkOf, pillOf, rootOf, type Link } from "./model";

const link: Link = { id: "ENG-482", url: "https://linear.app/acme/issue/ENG-482", title: "Rate-limit uploads", state: { name: "In Progress", type: "started" } };

describe("model", () => {
  it("reads the link and renders the pill", () => {
    const task: Node = { addr: "task:eng-482", kind: "task", title: "t", fields: { linear: link }, children: [], actions: [] };
    expect(linkOf(task)).toEqual(link);
    expect(linkOf({ ...task, fields: { linear: null } })).toBeNull();
    expect(pillOf(link)).toEqual({ text: "ENG-482 · In Progress", tone: "primary" });
    expect(pillOf({ ...link, state: { name: "Done", type: "completed" } }).tone).toBe("ok");
    expect(pillOf({ ...link, state: { name: "Todo", type: "unstarted" } }).tone).toBe("muted");
  });

  it("finds the root task and strips the identifier from issue titles", () => {
    expect(rootOf("task:eng-482/pr2/a")).toBe("task:eng-482");
    expect(issueTitle({ addr: "linear:ENG-9", kind: "linear", title: "ENG-9 Upload limits doc" })).toBe("Upload limits doc");
  });
});

const node = (addr: string, fields: Record<string, unknown>, children: Node["children"] = [], actions: string[] = []): Node => ({
  addr,
  kind: addr.split(":")[0],
  title: addr,
  fields,
  children,
  actions: actions.map((name) => ({ name, help: "" })),
});

const myIssue = { addr: "linear:ENG-482", kind: "linear", title: "ENG-482 Rate-limit uploads", status: "In Progress" };
const hit = { addr: "linear:ENG-9", kind: "linear", title: "ENG-9 Upload limits doc", status: "Todo" };

function fakeClient(connected: boolean) {
  const ops: Op[] = [];
  const nodes: Record<string, Node> = {
    app: node("app", {}, [
      { addr: "project:api", kind: "project", title: "api" },
      { addr: "project:web", kind: "project", title: "web" },
    ]),
    linear: connected
      ? node("linear", { connected: true, viewer: { name: "Alex" } }, [myIssue], ["disconnect", "search"])
      : node("linear", { connected: false }, [], ["connect", "connect-oauth"]),
    "project:api": node("project:api", {}, [], ["create-task", "create-task-from-linear"]),
  };
  const client: Client = {
    mode: "mock",
    label: "test",
    getNode: async (addr) => nodes[addr] ?? Promise.reject(new Error("no " + addr)),
    getJSON: async () => Promise.reject(new Error("unused")),
    raw: async () => Promise.reject(new Error("unused")),
    doOps: async (batch) => {
      ops.push(...batch);
      return batch.map((op): Result => {
        if (op.action === "search") return { ok: true, addr: op.target, data: [hit] };
        if (op.action === "create-task-from-linear") return { ok: true, addr: "task:eng-9", message: "created" };
        return { ok: true, addr: op.target };
      });
    },
    subscribe: () => () => {},
  };
  return { client, ops };
}

afterEach(() => {
  cleanup();
  act(() => setFocus("app"));
});

describe("LinearPanel", () => {
  it("connects with an API key", async () => {
    const { client, ops } = fakeClient(false);
    render(
      <ClientProvider client={client}>
        <LinearPanel />
      </ClientProvider>,
    );
    fireEvent.change(await screen.findByLabelText("Linear API key"), { target: { value: "lin_api_x" } });
    fireEvent.click(screen.getByRole("button", { name: "Connect" }));
    expect(screen.getByRole("button", { name: "Sign in with Linear" })).toBeTruthy();
    await waitFor(() => expect(ops).toEqual([{ target: "linear", action: "connect", args: { apiKey: "lin_api_x" } }]));
  });

  it("lists my issues, searches, and creates a task in the chosen project", async () => {
    const { client, ops } = fakeClient(true);
    render(
      <ClientProvider client={client}>
        <LinearPanel />
      </ClientProvider>,
    );
    expect(await screen.findByText("Rate-limit uploads")).toBeTruthy();
    expect(screen.getByText("Linear · Alex")).toBeTruthy();

    const search = screen.getByLabelText("Search Linear issues");
    fireEvent.change(search, { target: { value: "upload" } });
    fireEvent.submit(search);
    fireEvent.click(await screen.findByText("Upload limits doc"));

    fireEvent.change(screen.getByLabelText("Project"), { target: { value: "project:web" } });
    fireEvent.click(screen.getByText("Upload limits doc"));
    await waitFor(() => expect(getFocus()).toBe("task:eng-9"));
    expect(ops).toEqual([
      { target: "linear", action: "search", args: { query: "upload" } },
      { target: "project:api", action: "create-task-from-linear", args: { issue: "ENG-9" } },
      { target: "project:web", action: "create-task-from-linear", args: { issue: "ENG-9" } },
    ]);
  });
});

it("the new-task dialog creates a task from a Linear issue", async () => {
  const { client, ops } = fakeClient(true);
  render(
    <ClientProvider client={client}>
      <ActionDialog />
    </ClientProvider>,
  );
  act(() => openAction("project:api", "create-task"));
  const details = (await screen.findByText("From Linear issue")).closest("details")!;
  details.open = true;
  fireEvent(details, new Event("toggle"));
  fireEvent.click(await screen.findByText("Rate-limit uploads"));
  await waitFor(() => expect(ops).toEqual([{ target: "project:api", action: "create-task-from-linear", args: { issue: "ENG-482" } }]));
  await waitFor(() => expect(getFocus()).toBe("task:eng-9"));
});
