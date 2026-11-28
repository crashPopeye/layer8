import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { demoSeed, mockClient } from "@/lib/mock";
import { ClientProvider } from "@/lib/react";
import { Viewer, type TextSelection } from "./viewer";

afterEach(cleanup);

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
  const frame = (await screen.findByTitle("mockup-limits.html", undefined, { timeout: 5000 })) as HTMLIFrameElement;
  expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
  expect(frame.getAttribute("src")).toBeNull();
  expect(frame.getAttribute("srcdoc")).toContain("<h3>Upload limits</h3>");
});

it("shows worktree files as code", async () => {
  wrap(<Viewer src="/api/file?task=task%3Aeng-482&path=go.mod" name="go.mod" />);
  expect(await screen.findByText(/module example.com\/api-gateway/)).toBeTruthy();
});

it("reports text selected in rendered markdown with the source lines it quotes, and marks commented blocks", async () => {
  Range.prototype.getBoundingClientRect = () => new DOMRect(10, 20, 30, 5);
  const onSelectText = vi.fn<(s: TextSelection | null) => void>();
  wrap(<Viewer src={artifact("05-plan.md")} name="05-plan.md" marks={[{ line: 3, lineEnd: 3 }]} onSelectText={onSelectText} />);
  const para = await screen.findByText(/Wrap upload routes/);
  expect(para.className).toContain("commented");
  expect(screen.getByRole("heading").className).not.toContain("commented");

  const range = document.createRange();
  range.setStart(para.firstChild!, "Wrap upload routes with a ".length);
  range.setEnd(para.firstChild!, "Wrap upload routes with a token bucket".length);
  getSelection()!.removeAllRanges();
  getSelection()!.addRange(range);
  fireEvent.mouseUp(para);
  expect(onSelectText).toHaveBeenLastCalledWith({ quote: "token bucket", lines: { line: 3, lineEnd: 3 }, rect: { left: 10, top: 20, bottom: 25 } });

  getSelection()!.removeAllRanges();
  fireEvent.mouseUp(para);
  expect(onSelectText).toHaveBeenLastCalledWith(null);
});

it("adds the selection reporter to html only when comments are wanted", async () => {
  wrap(<Viewer src={artifact("mockup-limits.html")} name="mockup-limits.html" onSelectText={() => {}} />);
  const frame = (await screen.findByTitle("mockup-limits.html", undefined, { timeout: 5000 })) as HTMLIFrameElement;
  expect(frame.getAttribute("srcdoc")).toContain("layer8Selection");
});
