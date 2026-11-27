import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { mockClient } from "@/lib/mock";
import { ClientProvider } from "@/lib/react";
import { Composer, subtaskName } from "./composer";

globalThis.ResizeObserver ??= class {
  observe() {}
  unobserve() {}
  disconnect() {}
};

afterEach(cleanup);

it("names a sub-task from the first words of the prompt", () => {
  expect(subtaskName("Fix the flaky upload test, then push")).toBe("fix-the-flaky-upload");
  expect(subtaskName("  !!  ")).toBe("session");
});

it("starts a new session in a new sub-task worktree", async () => {
  const client = mockClient([
    { target: "app", action: "add-project", args: { path: "/src/api" } },
    { target: "project:api", action: "create-task", args: { title: "Uploads", slug: "uploads" } },
  ]);
  const doOps = vi.spyOn(client, "doOps");
  render(
    <ClientProvider client={client}>
      <Composer task="task:uploads" session={null} sessions={[]} />
    </ClientProvider>,
  );

  fireEvent.keyDown(await screen.findByTitle(/^uploads on main/), { key: "Enter" });
  fireEvent.click(screen.getByRole("menuitemradio", { name: "New sub-task worktree" }));
  fireEvent.change(screen.getByLabelText("Message"), { target: { value: "Retry failed uploads" } });
  fireEvent.click(screen.getByTitle("Start session (⏎)"));

  await waitFor(() =>
    expect(doOps.mock.calls.map(([[op]]) => [op.target, op.action])).toEqual([
      ["task:uploads", "create-subtask"],
      ["task:uploads/retry-failed-uploads", "start-session"],
    ]),
  );
  expect(doOps.mock.calls[0][0][0].args).toEqual({ name: "retry-failed-uploads" });
  expect(doOps.mock.calls[1][0][0].args).toMatchObject({ prompt: "Retry failed uploads" });
});
