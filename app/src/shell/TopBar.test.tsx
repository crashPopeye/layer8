import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { closeTab, getFocus, getTabs, setFocus } from "@/lib/focus";
import { ClientProvider } from "@/lib/react";
import type { Client, Node } from "@/lib/types";
import { TopBar } from "./TopBar";

const titles: Record<string, string> = { "task:a": "Rate limits", "task:b": "Billing" };

const client: Client = {
  mode: "mock",
  label: "test",
  getNode: async (addr) => ({ addr, kind: addr.split(":")[0], title: titles[addr] ?? addr, children: [], actions: [] }) as Node,
  getJSON: async () => {
    throw new Error("unused");
  },
  raw: async () => {
    throw new Error("unused");
  },
  doOps: async (batch) => batch.map((op) => ({ ok: true, addr: op.target })),
  subscribe: () => () => {},
};

afterEach(() => {
  cleanup();
  act(() => {
    getTabs().forEach((t) => closeTab(t.key));
    setFocus("app");
  });
});

function show() {
  act(() => {
    setFocus("task:a");
    setFocus("session:s1", "task:a");
    setFocus("task:b");
  });
  render(
    <ClientProvider client={client}>
      <TopBar />
    </ClientProvider>,
  );
}

it("shows the open tasks as tabs, the focused one selected", async () => {
  show();
  const tabs = await screen.findAllByRole("tab");
  expect(tabs.map((t) => t.textContent)).toEqual(["Rate limits", "Billing"]);
  expect(tabs.map((t) => t.getAttribute("aria-selected"))).toEqual(["false", "true"]);
  expect(screen.queryByTitle("Switch theme")).toBeNull();
});

it("focuses a tab where it was left", async () => {
  show();
  fireEvent.click(await screen.findByText("Rate limits"));
  expect(getFocus()).toBe("session:s1");
});

it("closes a tab with × or a middle click", async () => {
  show();
  fireEvent.click(await screen.findByLabelText("Close Billing"));
  expect(getFocus()).toBe("session:s1");
  const [tab] = screen.getAllByRole("tab");
  fireEvent(tab, new MouseEvent("auxclick", { bubbles: true, button: 1 }));
  expect(getTabs()).toEqual([]);
  expect(getFocus()).toBe("app");
});
