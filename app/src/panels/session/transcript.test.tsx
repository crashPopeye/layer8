import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { ItemView } from "./transcript";

afterEach(cleanup);

it("offers to send a queued message now", () => {
  const sendNow = vi.fn();
  const item = { kind: "user" as const, key: "e3", id: "m2", text: "Use a token bucket" };
  const { rerender } = render(<ItemView item={item} queued sendNow={sendNow} answer={vi.fn()} pending={new Set()} />);
  fireEvent.click(screen.getByRole("button", { name: "Send now" }));
  expect(sendNow).toHaveBeenCalledWith("m2");

  rerender(<ItemView item={{ ...item, delivered: true }} sendNow={sendNow} answer={vi.fn()} pending={new Set()} />);
  expect(screen.queryByRole("button", { name: "Send now" })).toBeNull();
});
