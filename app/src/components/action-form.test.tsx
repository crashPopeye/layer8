import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { getFocus, setFocus } from "@/lib/focus";
import { demoSeed, mockClient } from "@/lib/mock";
import { ClientProvider } from "@/lib/react";
import { ActionDialog, ActionForm, openAction } from "./action-form";

afterEach(() => setFocus("app"));

const open = {
  name: "open",
  help: "Open the worktree",
  args: {
    type: "object",
    required: ["target"],
    properties: { target: { enum: ["terminal", "editor"] }, note: { type: "string" } },
  },
};

it("renders a field per schema property and submits typed args", async () => {
  const submit = vi.fn(async () => null);
  render(<ActionForm action={open} onSubmit={submit} />);

  fireEvent.click(screen.getByRole("button", { name: "open" }));
  expect(await screen.findByText("target is required")).toBeTruthy();
  expect(submit).not.toHaveBeenCalled();

  fireEvent.change(screen.getByRole("combobox"), { target: { value: "editor" } });
  fireEvent.click(screen.getByRole("button", { name: "open" }));
  await waitFor(() => expect(submit).toHaveBeenCalledWith({ target: "editor" }));
});

it("shows the daemon's refusal under the form", async () => {
  render(<ActionForm action={{ name: "done", help: "" }} onSubmit={async () => "not available right now"} />);
  fireEvent.click(screen.getByRole("button", { name: "done" }));
  expect(await screen.findByText("not available right now")).toBeTruthy();
});

it("runs an action from the dialog and focuses the node it creates", async () => {
  const client = mockClient(demoSeed);
  render(
    <ClientProvider client={client}>
      <ActionDialog />
    </ClientProvider>,
  );
  act(() => openAction(["project:web-app", "project:api-gateway"], "create-task"));

  fireEvent.change(await screen.findByRole("combobox", { name: "target" }), { target: { value: "project:api-gateway" } });
  fireEvent.change(await screen.findByPlaceholderText(/Branch to start from/), { target: { value: "develop" } });
  fireEvent.change(screen.getAllByRole("textbox")[0], { target: { value: "Spike: S3 multipart" } });
  fireEvent.click(screen.getByRole("button", { name: "create-task" }));

  await waitFor(() => expect(getFocus()).toBe("task:spike-s3-multipart"));
  expect((await client.getNode("task:spike-s3-multipart")).fields).toMatchObject({
    base: "develop",
    project: "project:api-gateway",
  });
  await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
});
