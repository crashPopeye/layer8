import { fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { expect, it } from "vitest";
import { demoSeed, mockClient } from "@/lib/mock";
import { ClientProvider } from "@/lib/react";
import { Viewer } from "./viewer";

const client = mockClient(demoSeed);
const wrap = (ui: ReactNode) => render(<ClientProvider client={client}>{ui}</ClientProvider>);
const artifact = (file: string) => `/api/artifact?addr=${encodeURIComponent(`artifact:eng-482/${file}`)}`;

it("renders markdown with gfm tables, and its source on toggle", async () => {
  wrap(<Viewer src={artifact("05-plan.md")} name="05-plan.md" />);
  expect(await screen.findByRole("heading", { name: "Plan: rate-limit uploads" })).toBeTruthy();
  expect(screen.getByRole("table")).toBeTruthy();

  fireEvent.click(screen.getByTitle("Show source"));
  expect(screen.queryByRole("heading")).toBeNull();
  expect(screen.getByText(/# Plan: rate-limit uploads/)).toBeTruthy();
});

it("renders html in a script-only sandbox from srcdoc", async () => {
  wrap(<Viewer src={artifact("mockup-limits.html")} name="mockup-limits.html" />);
  const frame = (await screen.findByTitle("mockup-limits.html")) as HTMLIFrameElement;
  expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
  expect(frame.getAttribute("src")).toBeNull();
  expect(frame.getAttribute("srcdoc")).toContain("<h3>Upload limits</h3>");
});

it("shows worktree files as code", async () => {
  wrap(<Viewer src="/api/file?task=task%3Aeng-482&path=go.mod" name="go.mod" />);
  expect(await screen.findByText(/module example.com\/api-gateway/)).toBeTruthy();
});
