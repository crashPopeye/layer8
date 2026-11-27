import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it } from "vitest";
import { setFocus } from "@/lib/focus";
import { mockClient } from "@/lib/mock";
import { ClientProvider } from "@/lib/react";
import { UsageMonitor } from "./UsageMonitor";

afterEach(cleanup);

it("shows each harness's windows and details them in a popover", async () => {
  act(() => setFocus("app"));
  render(
    <ClientProvider client={mockClient()}>
      <UsageMonitor />
    </ClientProvider>,
  );
  fireEvent.click(await screen.findByText("codex"));
  expect(await screen.findByText("weekly")).toBeTruthy();
  expect(screen.getByText("76%")).toBeTruthy();
  expect(screen.getByText("eng-482")).toBeTruthy();
});
