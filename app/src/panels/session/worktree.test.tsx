import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { Node } from "@/lib/types";
import { BranchChip } from "./worktree";

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

afterEach(cleanup);

const task: Node = {
  addr: "task:eng-482",
  kind: "task",
  title: "ENG-482",
  fields: { branch: "eng-482", base: "main", ahead: 2, behind: 1, git: { changed: 3 } },
  children: [],
  actions: [
    { name: "open", help: "" },
    { name: "create-subtask", help: "" },
  ],
};

const openMenu = () => fireEvent.keyDown(screen.getByTitle(/^eng-482 on main/), { key: "Enter" });

it("shows the branch with ahead, behind and dirty counts, and opens the worktree", () => {
  const onOpen = vi.fn();
  render(<BranchChip task={task} fresh={null} onFresh={() => {}} onOpen={onOpen} />);
  const chip = screen.getByTitle("eng-482 on main · 2 ahead, 1 behind · 3 uncommitted");
  expect(chip.textContent).toBe("eng-482↑2↓1●3");

  openMenu();
  expect(screen.queryByText("New session runs in")).toBeNull();
  fireEvent.click(screen.getByRole("menuitem", { name: "Open in terminal" }));
  expect(onOpen).toHaveBeenCalledWith("terminal");
});

it("lets a new session run in a new sub-task worktree", () => {
  const onFresh = vi.fn();
  const { rerender } = render(<BranchChip task={task} fresh={false} onFresh={onFresh} onOpen={() => {}} />);
  openMenu();
  fireEvent.click(screen.getByRole("menuitemradio", { name: "New sub-task worktree" }));
  expect(onFresh).toHaveBeenCalledWith(true);

  rerender(<BranchChip task={task} fresh onFresh={onFresh} onOpen={() => {}} />);
  expect(screen.getByTitle(/^eng-482 on main/).textContent).toBe("new sub-task");
});
