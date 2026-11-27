import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { expect, it } from "vitest";
import { mockClient } from "./mock";
import { ClientProvider, useNode } from "./react";

it("useNode refetches when a node event names its address", async () => {
  const client = mockClient();
  const wrapper = ({ children }: { children: ReactNode }) => <ClientProvider client={client}>{children}</ClientProvider>;
  const { result } = renderHook(() => useNode("app"), { wrapper });
  await waitFor(() => expect(result.current.node?.children).toEqual([]));

  await act(() => client.doOps([{ target: "app", action: "add-project", args: { path: "/src/api" } }]));

  await waitFor(() => expect(result.current.node?.children.map((c) => c.addr)).toEqual(["project:api"]));
});

it("useNode reports a missing node as an error", async () => {
  const client = mockClient();
  const wrapper = ({ children }: { children: ReactNode }) => <ClientProvider client={client}>{children}</ClientProvider>;
  const { result } = renderHook(() => useNode("project:nope"), { wrapper });
  await waitFor(() => expect(result.current.error?.message).toBe("no node at project:nope"));
});
